// object-del.go 实现对象删除 DeleteObjects, 支持:
// --recursive/-r (需 --force), --versions, --version-id/--vid, --incomplete/-I,
// --dry-run, --older-than/--newer-than, --stdin, --non-current.
// 批量删除按 S3 上限分批, 并清理删除后变空的目录标记对象.

package action

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/s3iface"
)

// isBenignNotFound 判断错误是否为"对象不存在"（删除目录标记时的良性 404）。
// 用类型断言而非字符串嗅探, 避免后端改变错误包装方式后判断失效。
func isBenignNotFound(err error) bool {
	var apiErr *s3iface.ErrorResponse
	return errors.As(err, &apiErr) &&
		(apiErr.StatusCode == 404 || strings.Contains(apiErr.Code, "NoSuch"))
}

// DelOptions rm 命令参数.
type DelOptions struct {
	Recursive  bool     // -r: 递归删除
	Force      bool     // 递归删除必须显式 --force
	VersionID  string   // --version-id/--vid: 删除指定版本
	Versions   bool     // --versions: 删除对象及其全部版本
	Incomplete bool     // -I/--incomplete: 中止进行中的分片上传
	DryRun     bool     // --dry-run: 只列出将删除的对象, 不实际删除
	OlderThan  string   // 时长 (如 7d10h31s) 或绝对时间: 只删除更旧的对象
	NewerThan  string   // 时长或绝对时间: 只删除更新的对象
	Stdin      bool     // --stdin: 从 stdin 逐行读取 key
	NonCurrent bool     // --non-current: 删除非当前版本
	Include    []string // --include: 仅删除匹配任一 glob 的对象 (配合 -r)
	Exclude    []string // --exclude: 不删除匹配任一 glob 的对象
}

// DeleteObjects 删除对象 / 目录
func (c *Action) DeleteObjects(bucket, prefix string, opt DelOptions) error {
	// 指定 versionId 时只删除该对象的特定版本，忽略 recursive / 目录语义。
	if opt.VersionID != "" {
		if opt.Recursive {
			return fmt.Errorf("--version-id cannot be used with -r/--recursive")
		}
		if strings.HasSuffix(prefix, "/") {
			return fmt.Errorf("%s: --version-id requires a single object key, not a directory", c.S3Path(bucket, prefix))
		}
		if opt.DryRun {
			myprint.PrintfYellow("would delete %s (version %s)\n", c.S3Path(bucket, prefix), opt.VersionID)
			return nil
		}
		return c.deleteObjectVersion(bucket, prefix, opt.VersionID)
	}

	// --stdin: 从 stdin 逐行读取对象 key
	if opt.Stdin {
		return c.deleteFromStdin(bucket, opt)
	}

	// 末尾带 "/" 明确表示目录：不能拿带 "/" 的 key 去 HeadObject（部分服务
	// 会报 "Object name contains unsupported characters"），直接按目录前缀处理。
	if strings.HasSuffix(prefix, "/") {
		if !opt.Recursive {
			return fmt.Errorf("%s: is a directory. Use -r/--recursive to delete it", c.S3Path(bucket, prefix))
		}
		if !opt.Force {
			return fmt.Errorf("%s: recursive delete requires --force", c.S3Path(bucket, prefix))
		}
		if opt.Incomplete {
			return c.deletePrefixIncomplete(bucket, prefix, opt)
		}
		return c.deleteObjectsWithPrefix(bucket, prefix, opt)
	}

	ok, err := c.IsS3File(bucket, prefix)
	if err != nil {
		return fmt.Errorf("check s3 path: %s", FormatAPIError(err))
	}
	switch {
	case !ok && opt.Recursive:
		if !opt.Force {
			return fmt.Errorf("%s: recursive delete requires --force", c.S3Path(bucket, prefix))
		}
		if opt.Incomplete {
			return c.deletePrefixIncomplete(bucket, prefix, opt)
		}
		if err := c.deleteObjectsWithPrefix(bucket, prefix, opt); err != nil {
			return err
		}
	case !ok && !opt.Recursive:
		return fmt.Errorf("%s: not a single object. Use -r/--recursive to delete a directory", c.S3Path(bucket, prefix))
	default:
		if opt.Incomplete {
			return fmt.Errorf("--incomplete applies to a directory prefix; use -r")
		}
		if opt.Versions || opt.NonCurrent {
			return c.deleteVersionsOfObject(bucket, prefix, opt)
		}
		if opt.DryRun {
			myprint.PrintfYellow("would delete %s\n", c.S3Path(bucket, prefix))
			return nil
		}
		if err := c.deleteSingleObject(bucket, prefix); err != nil {
			return err
		}
	}
	return nil
}

// deleteVersionsOfObject 删除单个对象的全部版本 (--versions) 或非当前版本 (--non-current).
func (c *Action) deleteVersionsOfObject(bucket, key string, opt DelOptions) error {
	paginator := c.S3.NewListObjectVersionsPaginator(bucket,
		&s3iface.ListObjectVersionsOptions{Prefix: key})

	var toDelete []s3iface.ObjectIdentifier
	var total int
	flush := func() error {
		if len(toDelete) == 0 {
			return nil
		}
		if opt.DryRun {
			for _, o := range toDelete {
				myprint.PrintfYellow("would delete %s (version %s)\n", c.S3Path(bucket, o.Key), o.VersionID)
			}
			total += len(toDelete)
			toDelete = toDelete[:0]
			return nil
		}
		if err := c.deleteBatch(bucket, toDelete); err != nil {
			return err
		}
		total += len(toDelete)
		toDelete = toDelete[:0]
		return nil
	}

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(c.Ctx)
		if err != nil {
			return fmt.Errorf("list versions: %s", FormatAPIError(err))
		}
		for _, v := range page.Versions {
			if v.Key != key {
				continue
			}
			if opt.NonCurrent && v.IsLatest {
				continue
			}
			toDelete = append(toDelete, s3iface.ObjectIdentifier{Key: v.Key, VersionID: v.VersionID})
		}
		for _, m := range page.DeleteMarkers {
			if m.Key != key {
				continue
			}
			if opt.NonCurrent && m.IsLatest {
				continue
			}
			toDelete = append(toDelete, s3iface.ObjectIdentifier{Key: m.Key, VersionID: m.VersionID})
		}
		if len(toDelete) >= 1000 {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	if err := flush(); err != nil {
		return err
	}

	myprint.PrintfBoldGreen("Delete %d version(s) of %s: success\n", total, c.S3Path(bucket, key))
	return nil
}

// deleteFromStdin 从 stdin 逐行读取 key 并逐个删除 (--stdin).
func (c *Action) deleteFromStdin(bucket string, opt DelOptions) error {
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		key := strings.TrimSpace(sc.Text())
		if key == "" {
			continue
		}
		key = strings.TrimPrefix(key, "s3://"+bucket+"/")
		if opt.DryRun {
			myprint.PrintfYellow("would delete %s\n", c.S3Path(bucket, key))
			continue
		}
		if err := c.deleteSingleObject(bucket, key); err != nil {
			myprint.PrintfRed("%v\n", err)
		}
	}
	return sc.Err()
}

// deletePrefixIncomplete 中止 prefix 下所有进行中的分片上传 (-I).
func (c *Action) deletePrefixIncomplete(bucket, prefix string, opt DelOptions) error {
	out, err := c.S3.ListMultipartUploads(c.Ctx, bucket, &s3iface.ListMultipartUploadsOptions{Prefix: prefix})
	if err != nil {
		return fmt.Errorf("list multipart uploads: %s", FormatAPIError(err))
	}
	var aborted int
	for _, u := range out.Uploads {
		if opt.DryRun {
			myprint.PrintfYellow("would abort %s  uploadId=%s\n", c.S3Path(bucket, u.Key), u.UploadID)
			continue
		}
		if err := c.S3.AbortMultipartUpload(c.Ctx, bucket, u.Key, u.UploadID); err != nil {
			myprint.PrintfRed("abort %s/%s: %s\n", bucket, u.Key, FormatAPIError(err))
			continue
		}
		aborted++
	}
	myprint.PrintfBoldGreen("aborted %d in-progress uploads under %s\n", aborted, c.S3Path(bucket, prefix))
	return nil
}

func (c *Action) deleteSingleObject(bucket, key string) error {
	_, err := c.S3.DeleteObject(c.Ctx, bucket, key, "")
	if err != nil {
		return fmt.Errorf("delete %s: %s", c.S3Path(bucket, key), FormatAPIError(err))
	}
	if err := c.deleteEmptyParentDirectories(bucket, parentDirectory(key)); err != nil {
		return err
	}

	myprint.PrintfBoldGreen("Delete %s: success\n", c.S3Path(bucket, key))
	return nil
}

func (c *Action) deleteObjectVersion(bucket, key, versionID string) error {
	_, err := c.S3.DeleteObject(c.Ctx, bucket, key, versionID)
	if err != nil {
		return fmt.Errorf("delete %s (version %s): %s", c.S3Path(bucket, key), versionID, FormatAPIError(err))
	}

	myprint.PrintfBoldGreen("Delete %s (version %s): success\n", c.S3Path(bucket, key), versionID)
	return nil
}

func (c *Action) deleteObjectsWithPrefix(bucket, prefix string, opt DelOptions) error {
	// 时间过滤解析 (--older-than/--newer-than)
	newer, older, err := parseDeleteTimeFilters(opt)
	if err != nil {
		return err
	}

	// 规范化目录前缀：若 prefix 非空且不以 "/" 结尾，且 prefix+"/" 是一个真实目录，
	// 则改用 prefix+"/" 作为删除前缀。否则 "s3cli/.git" 会把同级的 "s3cli/.gitignore"
	// 一并误删（前缀匹配把 ".gitignore" 也命中了）。
	//
	// 使用 delimiter="/" 检测 CommonPrefixes (子目录) 和 Contents (对象)，
	// 以兼容 SeaweedFS 等仅通过 filer 目录而非 S3 对象来表示空目录的后端。
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		dirPrefix := prefix + "/"
		listResp, err := c.S3.ListObjectsV2(c.Ctx, bucket, &s3iface.ListObjectsV2Options{
			Prefix:    dirPrefix,
			Delimiter: "/",
			MaxKeys:   1,
		})
		if err != nil {
			return fmt.Errorf("list objects: %s", FormatAPIError(err))
		}
		if len(listResp.Contents) > 0 || len(listResp.CommonPrefixes) > 0 {
			prefix = dirPrefix
		}
	}

	// --versions / --non-current: 走版本列举逐个删除
	if opt.Versions || opt.NonCurrent {
		return c.deleteVersionsUnderPrefix(bucket, prefix, opt, newer, older)
	}

	paginator := c.S3.NewListObjectsV2Paginator(bucket, &s3iface.ListObjectsV2Options{
		Prefix: prefix,
	})

	var toDelete []s3iface.ObjectIdentifier
	var total int
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(c.Ctx)
		if err != nil {
			return fmt.Errorf("list objects: %s", FormatAPIError(err))
		}
		for _, item := range page.Contents {
			if len(opt.Include) > 0 || len(opt.Exclude) > 0 {
				if !matchesMirrorFilters(item.Key, opt.Include, opt.Exclude) {
					continue
				}
			}
			if !matchDeleteTime(item.LastModified, newer, older) {
				continue
			}
			if opt.DryRun {
				myprint.PrintfYellow("would delete %s\n", c.S3Path(bucket, item.Key))
				total++
				continue
			}
			toDelete = append(toDelete, s3iface.ObjectIdentifier{Key: item.Key})
		}
		if len(toDelete) >= 1000 {
			if err := c.deleteBatch(bucket, toDelete); err != nil {
				return err
			}
			total += len(toDelete)
			toDelete = toDelete[:0]
		}
	}
	if len(toDelete) > 0 {
		if err := c.deleteBatch(bucket, toDelete); err != nil {
			return err
		}
		total += len(toDelete)
	}

	if opt.DryRun {
		myprint.PrintfBoldBlue("would delete %d object(s) from %s (dry-run)\n", total, c.S3Path(bucket, prefix))
		return nil
	}

	// 显式删除目录标记对象本身（如 prefix="a/b/" 时删除 "a/b/" 这个零字节对象）。
	// 同时也尝试删除 prefix+"/"（当 prefix 不以 "/" 结尾时），确保两种情况都能覆盖。
	if _, err := c.S3.DeleteObject(c.Ctx, bucket, prefix, ""); err != nil {
		if !isBenignNotFound(err) {
			return fmt.Errorf("delete directory marker %s: %s", c.S3Path(bucket, prefix), FormatAPIError(err))
		}
	}
	if !strings.HasSuffix(prefix, "/") {
		if _, err := c.S3.DeleteObject(c.Ctx, bucket, prefix+"/", ""); err != nil {
			if !isBenignNotFound(err) {
				return fmt.Errorf("delete directory marker %s/: %s", c.S3Path(bucket, prefix), FormatAPIError(err))
			}
		}
	}

	myprint.PrintfBoldGreen("Delete %d objects from %s: success\n", total, c.S3Path(bucket, prefix))
	if err := c.deleteEmptyParentDirectories(bucket, parentDirectory(prefix)); err != nil {
		return err
	}
	return nil
}

// parseDeleteTimeFilters 解析 rm 的 --older-than/--newer-than.
func parseDeleteTimeFilters(opt DelOptions) (newer, older time.Time, err error) {
	if opt.NewerThan != "" {
		newer, err = parseFilterTime(opt.NewerThan)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("--newer-than: %w", err)
		}
	}
	if opt.OlderThan != "" {
		older, err = parseFilterTime(opt.OlderThan)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("--older-than: %w", err)
		}
	}
	return newer, older, nil
}

// matchDeleteTime 判断对象修改时间是否满足时间过滤 (两个条件同时为空时恒真).
func matchDeleteTime(lastModified time.Time, newer, older time.Time) bool {
	if !newer.IsZero() && !lastModified.After(newer) {
		return false
	}
	if !older.IsZero() && !lastModified.Before(older) {
		return false
	}
	return true
}

// deleteVersionsUnderPrefix 删除前缀下所有对象的版本 (--versions 全删;
// --non-current 只删非当前版本).
func (c *Action) deleteVersionsUnderPrefix(bucket, prefix string, opt DelOptions, newer, older time.Time) error {
	paginator := c.S3.NewListObjectVersionsPaginator(bucket,
		&s3iface.ListObjectVersionsOptions{Prefix: prefix})

	var toDelete []s3iface.ObjectIdentifier
	var total int
	flush := func() error {
		if len(toDelete) == 0 {
			return nil
		}
		if opt.DryRun {
			for _, o := range toDelete {
				myprint.PrintfYellow("would delete %s (version %s)\n", c.S3Path(bucket, o.Key), o.VersionID)
			}
			total += len(toDelete)
			toDelete = toDelete[:0]
			return nil
		}
		if err := c.deleteBatch(bucket, toDelete); err != nil {
			return err
		}
		total += len(toDelete)
		toDelete = toDelete[:0]
		return nil
	}

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(c.Ctx)
		if err != nil {
			return fmt.Errorf("list versions: %s", FormatAPIError(err))
		}
		for _, v := range page.Versions {
			if len(opt.Include) > 0 || len(opt.Exclude) > 0 {
				if !matchesMirrorFilters(v.Key, opt.Include, opt.Exclude) {
					continue
				}
			}
			// --non-current: 跳过最新版本
			if opt.NonCurrent && v.IsLatest {
				continue
			}
			if !matchDeleteTime(v.LastModified, newer, older) {
				continue
			}
			toDelete = append(toDelete, s3iface.ObjectIdentifier{Key: v.Key, VersionID: v.VersionID})
		}
		for _, m := range page.DeleteMarkers {
			if opt.NonCurrent && m.IsLatest {
				continue
			}
			if !matchDeleteTime(m.LastModified, newer, older) {
				continue
			}
			toDelete = append(toDelete, s3iface.ObjectIdentifier{Key: m.Key, VersionID: m.VersionID})
		}
		if len(toDelete) >= 1000 {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	if err := flush(); err != nil {
		return err
	}

	myprint.PrintfBoldGreen("Delete %d version(s) from %s: success\n", total, c.S3Path(bucket, prefix))
	return nil
}

func parentDirectory(key string) string {
	key = strings.TrimSuffix(key, "/")
	parent := strings.LastIndex(key, "/")
	if parent < 0 {
		return ""
	}
	return key[:parent+1]
}

// deleteEmptyParentDirectories removes explicit directory marker objects left empty by a deletion.
func (c *Action) deleteEmptyParentDirectories(bucket, directory string) error {
	for directory != "" {
		listResp, err := c.S3.ListObjectsV2(c.Ctx, bucket, &s3iface.ListObjectsV2Options{
			Prefix:  directory,
			MaxKeys: 2,
		})
		if err != nil {
			return fmt.Errorf("list objects: %s", FormatAPIError(err))
		}

		isEmptyMarker := len(listResp.Contents) == 1 && listResp.Contents[0].Key == directory && !listResp.IsTruncated
		if !isEmptyMarker {
			return nil
		}
		if _, err := c.S3.DeleteObject(c.Ctx, bucket, directory, ""); err != nil {
			return fmt.Errorf("delete empty directory %s: %s", c.S3Path(bucket, directory), FormatAPIError(err))
		}
		directory = parentDirectory(directory)
	}
	return nil
}

func (c *Action) deleteBatch(bucket string, objects []s3iface.ObjectIdentifier) error {
	result, err := c.S3.DeleteObjects(c.Ctx, bucket, objects, true)
	if err != nil {
		return fmt.Errorf("delete batch of %d: %s", len(objects), FormatAPIError(err))
	}
	if len(result.Errors) > 0 {
		first := result.Errors[0]
		return fmt.Errorf("delete batch of %d: %d object(s) failed (first %q: %s: %s)", len(objects), len(result.Errors), first.Key, first.Code, first.Message)
	}
	return nil
}
