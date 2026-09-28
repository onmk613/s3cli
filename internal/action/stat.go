// stat.go 实现元信息展示 (StatObjects):
// 对象输出 Name/Date/Size/ETag/Type/Metadata; 桶输出属性 (Versioning/Location/
// Anonymous/ILM) 与用量统计; 支持 --json 与 -r/--recursive.

package action

import (
	"errors"
	"fmt"
	"s3cli/internal/action/render"
	"sort"
	"strings"
	"time"

	"s3cli/internal/api"
	myprint "s3cli/internal/fmtutil"
	"s3cli/internal/i18n"
)

// StatOptions stat 命令参数.
type StatOptions struct {
	Recursive bool   // -r: 递归展示前缀下所有对象
	VersionID string // --version-id/--vid: 指定对象版本
	JSON      bool   // --json: JSON lines 输出
}

// StatObjects 展示对象或桶的元信息.
func (c *Action) StatObjects(opt StatOptions, bucket, prefix string) error {
	if opt.VersionID != "" {
		if prefix == "" {
			return errors.New(i18n.T("--version-id requires an object key", "--version-id 需要指定对象 key"))
		}
		return c.statObject(bucket, prefix, opt.VersionID, opt)
	}
	if opt.Recursive {
		// -r: 递归展示对象, 桶本身不输出
		return c.statRecursive(bucket, prefix, opt)
	}
	if prefix == "" {
		return c.statBucket(bucket, opt)
	}

	ok, err := c.IsS3File(bucket, prefix)
	if err != nil {
		return fmt.Errorf("check s3 path: %w", err)
	}
	if !ok {
		return fmt.Errorf(i18n.T("%s: not a file (use -r/--recursive to stat all objects under it)", "%s：不是文件（使用 -r/--recursive 查看其下所有对象）"), c.S3Path(bucket, prefix))
	}
	return c.statObject(bucket, prefix, "", opt)
}

// statRecursive 逐个输出前缀下对象的 stat (-r).
func (c *Action) statRecursive(bucket, prefix string, opt StatOptions) error {
	var count int
	err := c.forEachObject(c.Ctx, bucket, prefix, func(obj api.ObjectInfo) error {
		if strings.HasSuffix(obj.Key, "/") && obj.Size == 0 {
			return nil // 目录标记对象
		}
		count++
		if err := c.statObject(bucket, obj.Key, "", opt); err != nil {
			return err
		}
		if !opt.JSON {
			myprint.Println("")
		}
		return nil
	})
	if err != nil {
		return err
	}
	if count == 0 {
		myprint.PrintfBoldYellow(i18n.T("%s: no objects found\n", "%s：未找到对象\n"), c.S3Path(bucket, prefix))
	}
	return nil
}

// statObject 输出单个对象的元信息.
func (c *Action) statObject(bucket, key, versionID string, opt StatOptions) error {
	head, err := c.S3.HeadObject(c.Ctx, bucket, key, versionID)
	if err != nil {
		return fmt.Errorf("stat %s: %w", c.S3Path(bucket, key), err)
	}

	meta := statMetadata(head)
	if opt.JSON {
		return render.JSONLine(map[string]any{
			"status":       "success",
			"name":         pathBase(key),
			"lastModified": head.LastModified,
			"size":         head.ContentLength,
			"etag":         head.ETag,
			"type":         "file",
			"metadata":     meta,
		})
	}

	myprint.PrintfBoldBlue("%-10s: %s\n", i18n.T("Name", "名称"), pathBase(key))
	myprint.Printf("%-10s: %s\n", i18n.T("Date", "日期"), head.LastModified.Local().Format(statTimeLayout))
	myprint.Printf("%-10s: %s\n", i18n.T("Size", "大小"), myprint.FormatBytes(head.ContentLength))
	myprint.Printf("%-10s: %s\n", i18n.T("ETag", "ETag"), head.ETag)
	myprint.Printf("%-10s: %s\n", i18n.T("Type", "类型"), i18n.T("file", "文件"))
	if len(meta) > 0 {
		myprint.Printf("%-10s:\n", i18n.T("Metadata", "元数据"))
		keys := make([]string, 0, len(meta))
		for k := range meta {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			myprint.Printf("  %s: %s\n", k, meta[k])
		}
	}
	myprint.Println("")
	return nil
}

// statMetadata 汇总 HEAD 响应中的元数据 (Metadata 段).
func statMetadata(head *api.HeadObjectOutput) map[string]string {
	meta := map[string]string{}
	if head.ContentType != "" {
		meta["Content-Type"] = head.ContentType
	}
	if head.ContentEncoding != "" {
		meta["Content-Encoding"] = head.ContentEncoding
	}
	if head.ContentDisposition != "" {
		meta["Content-Disposition"] = head.ContentDisposition
	}
	if head.ContentLanguage != "" {
		meta["Content-Language"] = head.ContentLanguage
	}
	if head.CacheControl != "" {
		meta["Cache-Control"] = head.CacheControl
	}
	if head.StorageClass != "" {
		meta["x-amz-storage-class"] = head.StorageClass
	}
	if head.ServerSideEncryption != "" {
		meta["x-amz-server-side-encryption"] = head.ServerSideEncryption
	}
	if head.SSEKMSKeyID != "" {
		meta["x-amz-server-side-encryption-aws-kms-key-id"] = head.SSEKMSKeyID
	}
	if head.ObjectLockMode != "" {
		meta["x-amz-object-lock-mode"] = head.ObjectLockMode
	}
	for k, v := range head.Metadata {
		meta["x-amz-meta-"+k] = v
	}
	return meta
}

// statBucket 输出桶的元信息与用量.
func (c *Action) statBucket(bucket string, opt StatOptions) error {
	location, err := c.S3.GetBucketLocation(c.Ctx, bucket)
	if err != nil {
		return fmt.Errorf("get bucket location: %w", err)
	}
	if location == "" {
		location = "us-east-1" // 未返回时使用默认值
	}

	// 创建时间 (来自 ListBuckets)。ListBuckets 往往需要额外的账号级权限,
	// 失败时降级为不可知而不是报错, 但要让用户知道是"查不到"而非"没有"。
	createdAtKnown := true
	var createdAt time.Time
	if buckets, err := c.S3.ListBuckets(c.Ctx); err == nil {
		for _, b := range buckets {
			if b.Name == bucket {
				createdAt = b.CreationDate
				break
			}
		}
	} else {
		createdAtKnown = false
	}

	// 属性: 只把"未配置"(404 / NoSuch*) 当作 Disabled/Un-versioned;
	// 403、网络错误等必须上抛 —— 否则用户会把"没权限看"误读成"桶上确实没有",
	// 例如以为没有策略而放心地把桶当私有。
	enabledState, err := bucketConfigState(func() error {
		_, e := c.S3.GetBucketPolicy(c.Ctx, bucket)
		return e
	})
	if err != nil {
		return fmt.Errorf("get bucket policy %s: %w", c.S3Path(bucket, ""), err)
	}
	anonymous := map[bool]string{true: "Enabled", false: "Disabled"}[enabledState]

	ilmState, err := bucketConfigState(func() error {
		_, e := c.S3.GetBucketLifecycle(c.Ctx, bucket)
		return e
	})
	if err != nil {
		return fmt.Errorf("get bucket lifecycle %s: %w", c.S3Path(bucket, ""), err)
	}
	ilm := map[bool]string{true: "Enabled", false: "Disabled"}[ilmState]

	versioning := "Un-versioned"
	if v, verr := c.S3.GetBucketVersioning(c.Ctx, bucket); verr != nil {
		if !api.IsNotFound(verr) {
			return fmt.Errorf("get bucket versioning %s: %w", c.S3Path(bucket, ""), verr)
		}
	} else {
		switch v {
		case api.VersioningEnabled:
			versioning = "Enabled"
		case api.VersioningSuspended:
			versioning = "Suspended"
		}
	}

	// 用量: 对象数/总大小 (递归列举), 版本数 (版本列举)
	var totalSize, objCount, verCount int64
	// 列举失败 (如无 ListBucket 权限) 必须上报, 否则用量会静默显示为 0。
	if err := c.forEachObject(c.Ctx, bucket, "", func(o api.ObjectInfo) error {
		totalSize += o.Size
		objCount++
		return nil
	}); err != nil {
		return err
	}
	// 版本列举失败必须上报: 此前 break 会让 versionsCount 静默截断成一个偏小的数字。
	if err := c.forEachVersion(c.Ctx, bucket, "", func(VersionEntry) error {
		verCount++
		return nil
	}); err != nil {
		return err
	}

	if opt.JSON {
		return render.JSONLine(map[string]any{
			"status":     "success",
			"name":       bucket,
			"createdAt":  createdAtJSON(createdAt, createdAtKnown),
			"type":       "folder",
			"versioning": versioning,
			"location":   location,
			"anonymous":  anonymous,
			"ilm":        ilm,
			"usage": map[string]any{
				"totalSize":     totalSize,
				"objectsCount":  objCount,
				"versionsCount": verCount,
			},
		})
	}

	myprint.PrintfBoldBlue("%-10s: %s\n", i18n.T("Name", "名称"), bucket)
	dateStr := createdAtText(createdAt, createdAtKnown)
	myprint.Printf("%-10s: %s\n", i18n.T("Date", "日期"), dateStr)
	myprint.Printf("%-10s: %s\n", i18n.T("Size", "大小"), "N/A")
	myprint.Printf("%-10s: %s\n", i18n.T("Type", "类型"), i18n.T("folder", "文件夹"))
	myprint.Println("")
	myprint.PrintfBoldBlue("%s", i18n.T("Properties:\n", "属性：\n"))
	myprint.Printf(i18n.T("  Versioning: %s\n", "  版本控制：%s\n"), versioning)
	myprint.Printf(i18n.T("  Location: %s\n", "  区域：%s\n"), location)
	myprint.Printf(i18n.T("  Anonymous: %s\n", "  匿名访问：%s\n"), anonymous)
	myprint.Printf(i18n.T("  ILM: %s\n", "  ILM：%s\n"), ilm)
	myprint.Println("")
	myprint.PrintfBoldBlue("%s", i18n.T("Usage:\n", "用量：\n"))
	myprint.Printf(i18n.T("      Total size: %s\n", "      总大小：%s\n"), myprint.FormatBytes(totalSize))
	myprint.Printf(i18n.T("   Objects count: %d\n", "   对象数：%d\n"), objCount)
	myprint.Printf(i18n.T("  Versions count: %d\n", "  版本数：%d\n"), verCount)
	return nil
}

// statTimeLayout 是 stat 输出的时间格式 (带时区缩写, 便于跨区域核对)。
const statTimeLayout = "2006-01-02 15:04:05 MST"

// createdAtText 渲染桶创建时间: 未知 (ListBuckets 无权限) 与"确实未返回"都显示 N/A。
func createdAtText(t time.Time, known bool) string {
	if !known || t.IsZero() {
		return "N/A"
	}
	return t.Local().Format(statTimeLayout)
}

// createdAtJSON 与 createdAtText 同语义, 供 --json 输出使用 (未知时为 null)。
func createdAtJSON(t time.Time, known bool) any {
	if !known || t.IsZero() {
		return nil
	}
	return t
}

// bucketConfigState 把一个"读取桶子资源配置"的调用归类为 已配置 / 未配置。
//
// 未配置 (NoSuch* / 404) 与真正的失败 (403、网络错误) 必须区分: 把后者当成
// "Disabled" 会让用户以为桶上确实没有策略或生命周期规则, 而实际只是没权限看。
func bucketConfigState(get func() error) (enabled bool, err error) {
	switch err := get(); {
	case err == nil:
		return true, nil
	case api.IsNotFound(err):
		return false, nil
	default:
		return false, err
	}
}

// pathBase 取路径最后一段.
func pathBase(key string) string {
	if i := strings.LastIndex(key, "/"); i >= 0 {
		return key[i+1:]
	}
	return key
}
