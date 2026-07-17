package action

import (
	"fmt"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/s3api"
)

// ListObjectVersions 列出对象版本 + delete-marker
func (c *S3Client) ListObjectVersions(bucket, prefix string) error {
	paginator := s3api.NewListObjectVersionsPaginator(c.S3, bucket,
		&s3api.ListObjectVersionsOptions{Prefix: prefix})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(c.Ctx)
		if err != nil {
			return fmt.Errorf("list versions: %s", FormatAPIError(err))
		}
		for _, v := range page.Versions {
			flag := "VER "
			if v.IsLatest {
				flag = "VER*"
			}
			myprint.Printf("%s ", flag)
			myprint.PrintfDim("[%s]  ", v.LastModified.Format("2006-01-02 15:04:05"))
			myprint.Printf("%12d   ", v.Size)
			myprint.PrintfGreen("%s  ", c.S3Path(bucket, v.Key))
			myprint.PrintfCyan("ID=%s\n", v.VersionID)
		}
		for _, m := range page.DeleteMarkers {
			flag := "DEL "
			if m.IsLatest {
				flag = "DEL*"
			}

			myprint.PrintfRed("%s ", flag)
			myprint.PrintfDim("[%s]  ", m.LastModified.Format("2006-01-02 15:04:05"))
			myprint.Printf("%12s   ", "-")
			myprint.PrintfRed("%s  ", c.S3Path(bucket, m.Key))
			myprint.PrintfCyan("ID=%s\n", m.VersionID)
		}
	}
	return nil
}
