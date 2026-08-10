// info.go 实现元信息查看 Info (mc stat 对齐): 桶或对象均可, 输出 JSON.
// 支持 --recursive/-r 遍历对象与 --version-id/--vid 指定版本.

package action

import (
	"encoding/json"
	"fmt"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/s3iface"
)

// InfoOptions info/stat 命令参数 (mc stat 对齐).
type InfoOptions struct {
	Recursive bool   // -r: 统计/列出前缀下所有对象
	VersionID string // --version-id/--vid: 指定对象版本
}

// Info 打印桶或对象的元信息
func (c *Action) Info(opt InfoOptions, bucket, prefix string) error {
	if opt.VersionID != "" {
		if prefix == "" {
			return fmt.Errorf("--version-id requires an object key")
		}
		return c.infoObjectVersion(bucket, prefix, opt.VersionID)
	}
	if prefix == "" {
		return c.infoBucket(bucket)
	}

	ok, err := c.IsS3File(bucket, prefix)
	if err != nil {
		return fmt.Errorf("check s3 path: %s", FormatAPIError(err))
	}
	if !ok {
		if !opt.Recursive {
			return fmt.Errorf("%s: not a file (use -r/--recursive to show all objects under it)", c.S3Path(bucket, prefix))
		}
		return c.infoObjectsRecursive(bucket, prefix)
	}

	return c.infoObject(bucket, prefix)
}

// infoObjectsRecursive 逐个输出前缀下对象的元信息 (mc stat -r).
func (c *Action) infoObjectsRecursive(bucket, prefix string) error {
	var count int
	err := c.forEachObject(c.Ctx, bucket, prefix, func(obj s3iface.ObjectInfo) error {
		if err := c.infoObject(bucket, obj.Key); err != nil {
			return err
		}
		count++
		return nil
	})
	if err != nil {
		return err
	}
	myprint.PrintfBoldBlue("%d object(s) under %s\n", count, c.S3Path(bucket, prefix))
	return nil
}

func (c *Action) infoObjectVersion(bucket, key, versionID string) error {
	head, err := c.S3.HeadObject(c.Ctx, bucket, key, versionID)
	if err != nil {
		return fmt.Errorf("head object: %s", FormatAPIError(err))
	}
	myprint.PrintfBoldBlue("# %s info(object, version %s):\n", c.S3Path(bucket, key), versionID)
	return printHeadInfo(c.S3Path(bucket, key), head, nil)
}

func (c *Action) infoObject(bucket, key string) error {
	head, err := c.S3.HeadObject(c.Ctx, bucket, key, "")
	if err != nil {
		return fmt.Errorf("head object: %s", FormatAPIError(err))
	}
	myprint.PrintfBoldBlue("# %s info(object):\n", c.S3Path(bucket, key))

	// Tagging
	tags := map[string]string{}
	if t, err := c.S3.GetObjectTagging(c.Ctx, bucket, key, ""); err == nil {
		for _, kv := range t {
			tags[kv.Key] = kv.Value
		}
	} else {
		myprint.PrintfBoldYellow("Cannot read tags for %s: %s\n", c.S3Path(bucket, key), FormatAPIError(err))
	}

	return printHeadInfo(c.S3Path(bucket, key), head, tags)
}

// printHeadInfo 输出 HeadObject 结果的 JSON.
func printHeadInfo(path string, head *s3iface.HeadObjectOutput, tags map[string]string) error {
	if tags == nil {
		tags = map[string]string{}
	}
	m := map[string]any{
		"Key":                   path,
		"ContentLength":         head.ContentLength,
		"ContentType":           head.ContentType,
		"ContentEncoding":       head.ContentEncoding,
		"ContentDisposition":    head.ContentDisposition,
		"CacheControl":          head.CacheControl,
		"ETag":                  head.ETag,
		"LastModified":          head.LastModified,
		"StorageClass":          head.StorageClass,
		"VersionId":             head.VersionID,
		"ServerSideEncryption":  head.ServerSideEncryption,
		"SSEKMSKeyId":           head.SSEKMSKeyID,
		"Metadata":              head.Metadata,
		"PartsCount":            head.PartsCount,
		"ReplicationStatus":     head.ReplicationStatus,
		"ObjectLockMode":        head.ObjectLockMode,
		"ObjectLockRetainUntil": head.ObjectLockRetainUntilDate,
		"Tags":                  tags,
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal info: %w", err)
	}

	myprint.PrintlnGreen(string(b))
	return nil
}

func (c *Action) infoBucket(bucket string) error {
	info := map[string]any{"Bucket": bucket}

	// Location 同时充当 bucket 存在性检查:
	// 吞掉它的错误会让 "info 不存在的 bucket" 输出一份全空 JSON 且退出码为 0。
	location, err := c.S3.GetBucketLocation(c.Ctx, bucket)
	if err != nil {
		return fmt.Errorf("get bucket location: %s", FormatAPIError(err))
	}
	info["Location"] = location

	// 以下子项允许不存在 (未配置时服务端返回 404 类错误), 失败按空值输出。

	// Versioning
	var versioning string
	if v, err := c.S3.GetBucketVersioning(c.Ctx, bucket); err == nil {
		versioning = string(v)
	}
	info["Versioning"] = versioning

	// Policy
	var policy string
	if p, err := c.S3.GetBucketPolicy(c.Ctx, bucket); err == nil {
		policy = string(p)
	}
	info["Policy"] = policy

	// CORS
	var corsRules any
	if cors, err := c.S3.GetBucketCors(c.Ctx, bucket); err == nil {
		corsRules = cors.CORSRules
	}
	info["CORS"] = corsRules

	// URL
	var url string
	if cred, err := c.GetS3Credentials(); err == nil {
		url = fmt.Sprintf("%s/%s/", cred.BaseEndpoint, bucket)
	}
	info["URL"] = url

	b, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal info: %w", err)
	}

	myprint.PrintfBoldBlue("# %s %s info(bucket):\n", c.Alias, bucket)
	myprint.PrintlnGreen(string(b))
	return nil
}
