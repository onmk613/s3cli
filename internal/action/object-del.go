package action

import (
	"fmt"
	"strings"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/s3api"
)

// DelOptions Delete 命令参数
type DelOptions struct {
	Recursive bool
	VersionID string
}

// DeleteObjects 删除对象 / 目录
func (c *S3Client) DeleteObjects(bucket, prefix string, opt DelOptions) error {
	// 指定 versionId 时只删除该对象的特定版本，忽略 recursive / 目录语义。
	if opt.VersionID != "" {
		if opt.Recursive {
			return fmt.Errorf("--version-id cannot be used with -r/--recursive")
		}
		if strings.HasSuffix(prefix, "/") {
			return fmt.Errorf("%s: --version-id requires a single object key, not a directory", c.S3Path(bucket, prefix))
		}
		return c.deleteObjectVersion(bucket, prefix, opt.VersionID)
	}

	// 末尾带 "/" 明确表示目录：不能拿带 "/" 的 key 去 HeadObject（部分服务如 minio
	// 会报 "Object name contains unsupported characters"），直接按目录前缀处理。
	if strings.HasSuffix(prefix, "/") {
		if !opt.Recursive {
			return fmt.Errorf("%s: is a directory. Use -r/--recursive to delete it", c.S3Path(bucket, prefix))
		}
		return c.deleteObjectsWithPrefix(bucket, prefix)
	}

	ok, err := c.IsS3File(bucket, prefix)
	if err != nil {
		return fmt.Errorf("check s3 path: %s", FormatAPIError(err))
	}
	switch {
	case !ok && opt.Recursive:
		if err := c.deleteObjectsWithPrefix(bucket, prefix); err != nil {
			return err
		}
	case !ok && !opt.Recursive:
		return fmt.Errorf("%s: not a single object. Use -r/--recursive to delete a directory", c.S3Path(bucket, prefix))
	default:
		if err := c.deleteSingleObject(bucket, prefix); err != nil {
			return err
		}
	}
	return nil
}

func (c *S3Client) deleteSingleObject(bucket, key string) error {
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

func (c *S3Client) deleteObjectVersion(bucket, key, versionID string) error {
	_, err := c.S3.DeleteObject(c.Ctx, bucket, key, versionID)
	if err != nil {
		return fmt.Errorf("delete %s (version %s): %s", c.S3Path(bucket, key), versionID, FormatAPIError(err))
	}

	myprint.PrintfBoldGreen("Delete %s (version %s): success\n", c.S3Path(bucket, key), versionID)
	return nil
}

func (c *S3Client) deleteObjectsWithPrefix(bucket, prefix string) error {
	// 规范化目录前缀：若 prefix 非空且不以 "/" 结尾，且 prefix+"/" 是一个真实目录，
	// 则改用 prefix+"/" 作为删除前缀。否则 "s3cli/.git" 会把同级的 "s3cli/.gitignore"
	// 一并误删（前缀匹配把 ".gitignore" 也命中了）。
	//
	// 使用 delimiter="/" 检测 CommonPrefixes (子目录) 和 Contents (对象)，
	// 以兼容 SeaweedFS 等仅通过 filer 目录而非 S3 对象来表示空目录的后端。
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		dirPrefix := prefix + "/"
		listResp, err := c.S3.ListObjectsV2(c.Ctx, bucket, &s3api.ListObjectsV2Options{
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
	paginator := s3api.NewListObjectsV2Paginator(c.S3, bucket, &s3api.ListObjectsV2Options{
		Prefix: prefix,
	})

	var toDelete []s3api.ObjectIdentifier
	var total int
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(c.Ctx)
		if err != nil {
			return fmt.Errorf("list objects: %s", FormatAPIError(err))
		}
		for _, item := range page.Contents {
			toDelete = append(toDelete, s3api.ObjectIdentifier{Key: item.Key})
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

	// 显式删除目录标记对象本身（如 prefix="a/b/" 时删除 "a/b/" 这个零字节对象）。
	// 同时也尝试删除 prefix+"/"（当 prefix 不以 "/" 结尾时），确保两种情况都能覆盖。
	if _, err := c.S3.DeleteObject(c.Ctx, bucket, prefix, ""); err != nil {
		if !strings.Contains(err.Error(), "NoSuchKey") {
			return fmt.Errorf("delete directory marker %s: %s", c.S3Path(bucket, prefix), FormatAPIError(err))
		}
	}
	if !strings.HasSuffix(prefix, "/") {
		if _, err := c.S3.DeleteObject(c.Ctx, bucket, prefix+"/", ""); err != nil {
			if !strings.Contains(err.Error(), "NoSuchKey") {
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

func parentDirectory(key string) string {
	key = strings.TrimSuffix(key, "/")
	parent := strings.LastIndex(key, "/")
	if parent < 0 {
		return ""
	}
	return key[:parent+1]
}

// deleteEmptyParentDirectories removes explicit directory marker objects left empty by a deletion.
func (c *S3Client) deleteEmptyParentDirectories(bucket, directory string) error {
	for directory != "" {
		listResp, err := c.S3.ListObjectsV2(c.Ctx, bucket, &s3api.ListObjectsV2Options{
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

func (c *S3Client) deleteBatch(bucket string, objects []s3api.ObjectIdentifier) error {
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
