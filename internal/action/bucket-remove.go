// bucket-remove.go 实现桶删除 (RemoveBuckets); --force 时先清空全部对象与版本再删桶.

package action

import (
	"fmt"

	"s3cli/internal/api"
	myprint "s3cli/internal/fmtutil"
	"s3cli/internal/i18n"
)

type RemoveBucketOptions struct {
	Force  bool
	DryRun bool // --dry-run: 只列出将删除的桶与对象数, 不做任何删除
}

// RemoveBuckets 删除桶; force=true 时先清空桶内全部对象与版本 (不可恢复) 再删桶.
func (c *Action) RemoveBuckets(opt RemoveBucketOptions, bucket string) error {
	if opt.DryRun {
		var objects, versions int64
		if err := c.forEachVersion(c.Ctx, bucket, "", func(VersionEntry) error {
			versions++
			return nil
		}); err != nil {
			return err
		}
		if err := c.forEachObject(c.Ctx, bucket, "", func(api.ObjectInfo) error {
			objects++
			return nil
		}); err != nil {
			return err
		}
		myprint.PrintfBoldBlue(i18n.T("would delete bucket %s (%d object(s), %d version(s)) (dry-run)\n",
			"将删除存储桶 %s（%d 个对象，%d 个版本，dry-run）\n"), c.S3Path(bucket, ""), objects, versions)
		return nil
	}

	if opt.Force {
		myprint.PrintfBoldYellow(i18n.T("WARNING: --force will permanently delete all objects/versions in %s\n", "警告：--force 将永久删除 %s 中的所有对象/版本\n"), c.S3Path(bucket, ""))
		if err := c.deleteAllObjects(bucket); err != nil {
			return fmt.Errorf("force-delete objects in %s: %w", bucket, err)
		}
	}

	if err := c.S3.DeleteBucket(c.Ctx, bucket); err != nil {
		return fmt.Errorf("delete bucket %s: %w", bucket, err)
	}

	myprint.PrintfBoldGreen(i18n.T("Bucket %s deleted for %s\n", "已为 %s 删除存储桶 %s\n"), c.Alias, bucket)
	return nil
}

// deleteAllObjects 清空桶内全部对象与版本 (bucket remove --force)。
// 按页累积后交给统一的批量删除, 分页与错误口径都来自 forEachVersionPage。
func (c *Action) deleteAllObjects(bucket string) error {
	var pending []api.ObjectIdentifier
	err := c.forEachVersionPage(c.Ctx, bucket, "", func(page *api.ListObjectVersionsOutput) error {
		for _, v := range page.Versions {
			pending = append(pending, api.ObjectIdentifier{Key: v.Key, VersionID: v.VersionID})
		}
		for _, m := range page.DeleteMarkers {
			pending = append(pending, api.ObjectIdentifier{Key: m.Key, VersionID: m.VersionID})
		}
		// 逐页落盘, 避免整桶版本一次性堆在内存里。
		if err := c.deleteObjectsInBatches(c.Ctx, bucket, pending); err != nil {
			return err
		}
		pending = pending[:0]
		return nil
	})
	return err
}
