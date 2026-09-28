// info.go 实现元信息查看 Info: 桶或对象均可, 输出 JSON.
// 支持 --recursive/-r 遍历对象与 --version-id/--vid 指定版本.

package action

import (
	"errors"
	"fmt"
	"s3cli/internal/action/render"

	"s3cli/internal/api"
	myprint "s3cli/internal/fmtutil"
	"s3cli/internal/i18n"
)

// InfoOptions info/stat 命令参数.
type InfoOptions struct {
	Recursive bool   // -r: 统计/列出前缀下所有对象
	VersionID string // --version-id/--vid: 指定对象版本
}

// Info 打印桶或对象的元信息
func (c *Action) Info(opt InfoOptions, bucket, prefix string) error {
	if opt.VersionID != "" {
		if prefix == "" {
			return errors.New(i18n.T("--version-id requires an object key", "--version-id 需要指定对象 key"))
		}
		return c.infoObjectVersion(bucket, prefix, opt.VersionID)
	}
	if prefix == "" {
		return c.infoBucket(bucket)
	}

	ok, err := c.IsS3File(bucket, prefix)
	if err != nil {
		return fmt.Errorf("check s3 path: %w", err)
	}
	if !ok {
		if !opt.Recursive {
			return fmt.Errorf(i18n.T("%s: not a file (use -r/--recursive to show all objects under it)", "%s：不是文件（使用 -r/--recursive 查看其下所有对象）"), c.S3Path(bucket, prefix))
		}
		return c.infoObjectsRecursive(bucket, prefix)
	}

	return c.infoObject(bucket, prefix)
}

// infoObjectsRecursive 逐个输出前缀下对象的元信息 (-r).
func (c *Action) infoObjectsRecursive(bucket, prefix string) error {
	var count int
	err := c.forEachObject(c.Ctx, bucket, prefix, func(obj api.ObjectInfo) error {
		if err := c.infoObject(bucket, obj.Key); err != nil {
			return err
		}
		count++
		return nil
	})
	if err != nil {
		return err
	}
	myprint.PrintfBoldBlue(i18n.T("%d object(s) under %s\n", "共 %d 个对象（位于 %s 下）\n"), count, c.S3Path(bucket, prefix))
	return nil
}

func (c *Action) infoObjectVersion(bucket, key, versionID string) error {
	head, err := c.S3.HeadObject(c.Ctx, bucket, key, versionID)
	if err != nil {
		return fmt.Errorf("head object: %w", err)
	}
	myprint.PrintfBoldBlue(i18n.T("# %s info(object, version %s):\n", "# %s info(object，版本 %s)：\n"), c.S3Path(bucket, key), versionID)
	return printHeadInfo(c.S3Path(bucket, key), head, nil)
}

func (c *Action) infoObject(bucket, key string) error {
	head, err := c.S3.HeadObject(c.Ctx, bucket, key, "")
	if err != nil {
		return fmt.Errorf("head object: %w", err)
	}
	myprint.PrintfBoldBlue(i18n.T("# %s info(object):\n", "# %s info(object)：\n"), c.S3Path(bucket, key))

	// Tagging
	tags := map[string]string{}
	if t, err := c.S3.GetObjectTagging(c.Ctx, bucket, key, ""); err == nil {
		for _, kv := range t {
			tags[kv.Key] = kv.Value
		}
	} else {
		myprint.PrintfBoldYellow("Cannot read tags for %s: %s\n", c.S3Path(bucket, key), err)
	}

	return printHeadInfo(c.S3Path(bucket, key), head, tags)
}

// printHeadInfo 输出 HeadObject 结果的 JSON.
func printHeadInfo(path string, head *api.HeadObjectOutput, tags map[string]string) error {
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
	return render.PrintJSONDoc(m)
}

func (c *Action) infoBucket(bucket string) error {
	info := map[string]any{"Bucket": bucket}

	// Location 同时充当 bucket 存在性检查:
	// 吞掉它的错误会让 "info 不存在的 bucket" 输出一份全空 JSON 且退出码为 0。
	location, err := c.S3.GetBucketLocation(c.Ctx, bucket)
	if err != nil {
		return fmt.Errorf("get bucket location: %w", err)
	}
	info["Location"] = location

	// 以下子项允许不存在 (未配置时服务端返回 404 / NoSuch*), 此时按空值输出;
	// 但 403、网络错误等真实失败必须上抛 —— 否则"没权限看"会被渲染成
	// "Versioning": "" / "Policy": "", 与"确实未配置"无法区分。
	// 注意 NoSuchBucketPolicy 这类码本身就属于 IsNotFound, 即"未配置"。

	// Versioning
	var versioning string
	if v, verr := c.S3.GetBucketVersioning(c.Ctx, bucket); verr != nil {
		if !api.IsNotFound(verr) {
			return fmt.Errorf("get bucket versioning %s: %w", c.S3Path(bucket, ""), verr)
		}
	} else {
		versioning = string(v)
	}
	info["Versioning"] = versioning

	// Policy
	var policy string
	if p, perr := c.S3.GetBucketPolicy(c.Ctx, bucket); perr != nil {
		if !api.IsNotFound(perr) {
			return fmt.Errorf("get bucket policy %s: %w", c.S3Path(bucket, ""), perr)
		}
	} else {
		policy = string(p)
	}
	info["Policy"] = policy

	// CORS
	var corsRules any
	if cors, cerr := c.S3.GetBucketCors(c.Ctx, bucket); cerr != nil {
		if !api.IsNotFound(cerr) {
			return fmt.Errorf("get bucket cors %s: %w", c.S3Path(bucket, ""), cerr)
		}
	} else {
		corsRules = cors.CORSRules
	}
	info["CORS"] = corsRules

	// URL
	var url string
	if cred, err := c.GetS3Credentials(); err == nil {
		url = fmt.Sprintf("%s/%s/", cred.BaseEndpoint, bucket)
	}
	info["URL"] = url

	myprint.PrintfBoldBlue(i18n.T("# %s %s info(bucket):\n", "# %s %s info(bucket)：\n"), c.Alias, bucket)
	return render.PrintJSONDoc(info)
}
