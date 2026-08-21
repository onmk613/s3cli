// bucket-remove.go 实现桶删除 (RemoveBuckets); --force 时先清空全部对象与版本再删桶.

package action

import (
	"fmt"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/i18n"
	"s3cli/pkg/s3iface"
)

type RemoveBucketOptions struct {
	Force bool
}

// RemoveBuckets 删除桶; force=true 时先清空桶内全部对象与版本 (不可恢复) 再删桶.
func (c *Action) RemoveBuckets(opt RemoveBucketOptions, bucket string) error {
	if opt.Force {
		myprint.PrintfBoldYellow(i18n.T("WARNING: --force will permanently delete all objects/versions in %s\n", "警告：--force 将永久删除 %s 中的所有对象/版本\n"), c.S3Path(bucket, ""))
		if err := c.deleteAllObjects(bucket); err != nil {
			return fmt.Errorf("force-delete objects in %s: %v", bucket, err)
		}
	}

	if err := c.S3.DeleteBucket(c.Ctx, bucket); err != nil {
		return fmt.Errorf("delete bucket %s: %s", bucket, FormatAPIError(err))
	}

	myprint.PrintfBoldGreen(i18n.T("Bucket %s deleted for %s\n", "已为 %s 删除存储桶 %s\n"), c.Alias, bucket)
	return nil
}

func (c *Action) deleteAllObjects(bucket string) error {
	paginator := c.S3.NewListObjectVersionsPaginator(bucket, &s3iface.ListObjectVersionsOptions{})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(c.Ctx)
		if err != nil {
			return fmt.Errorf("list versions: %s", FormatAPIError(err))
		}
		objects := make([]s3iface.ObjectIdentifier, 0, len(page.Versions)+len(page.DeleteMarkers))
		for _, v := range page.Versions {
			objects = append(objects, s3iface.ObjectIdentifier{Key: v.Key, VersionID: v.VersionID})
		}
		for _, m := range page.DeleteMarkers {
			objects = append(objects, s3iface.ObjectIdentifier{Key: m.Key, VersionID: m.VersionID})
		}
		if len(objects) == 0 {
			continue
		}
		result, err := c.S3.DeleteObjects(c.Ctx, bucket, objects, true)
		if err != nil {
			return fmt.Errorf("delete objects: %s", FormatAPIError(err))
		}
		// quiet 模式下部分对象仍可能失败 (权限/瞬态错误), 静默继续会导致
		// 随后 DeleteBucket 报 BucketNotEmpty 而用户不知道哪些对象没删掉。
		if len(result.Errors) > 0 {
			first := result.Errors[0]
			return fmt.Errorf("delete objects: %d object(s) failed (first %q: %s: %s)", len(result.Errors), first.Key, first.Code, first.Message)
		}
	}
	return nil
}
