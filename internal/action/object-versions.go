// object-versions.go 实现对象版本列举 ListObjectVersions, 展示各版本与 delete-marker.

package action

import (
	"s3cli/internal/action/render"
	myprint "s3cli/internal/fmtutil"
	"s3cli/internal/i18n"
)

// ListObjectVersions 列出对象版本 + delete-marker。
//
// 与 ls --versions 共用 forEachVersion 的分页逻辑与 VersionEntry 归一视图,
// 两者的差异只剩下着色与列宽 (删除标记用红色、大小显示为 "-")。
func (c *Action) ListObjectVersions(bucket, prefix string) error {
	return c.forEachVersion(c.Ctx, bucket, prefix, func(v VersionEntry) error {
		flag := render.VersionFlag(v.IsDeleteMarker, v.IsLatest)
		path := c.S3Path(bucket, v.Key)

		if v.IsDeleteMarker {
			myprint.PrintfRed("%s ", flag)
			myprint.PrintfDim("[%s]  ", v.LastModified.Format(render.TimeLayout))
			myprint.Printf("%12s   ", "-")
			myprint.PrintfRed("%s  ", path)
		} else {
			myprint.Printf("%s ", flag)
			myprint.PrintfDim("[%s]  ", v.LastModified.Format(render.TimeLayout))
			myprint.Printf("%12d   ", v.Size)
			myprint.PrintfGreen("%s  ", path)
		}
		myprint.PrintfCyan(i18n.T("ID=%s\n", "ID=%s\n"), v.VersionID)
		return nil
	})
}
