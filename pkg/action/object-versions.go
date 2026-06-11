package action

import (
	"fmt"

	myprint "s3cli/pkg/fmtutil"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// LsVersions 列出对象版本 + delete-marker
func (c *S3Client) ListOjbectVersions(bucket, prefix string) error {
	paginator := s3.NewListObjectVersionsPaginator(c.S3,
		&s3.ListObjectVersionsInput{Bucket: aws.String(bucket), Prefix: aws.String(prefix)})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(c.Ctx)
		if err != nil {
			return fmt.Errorf("list versions: %s", FormatAPIError(err))
		}
		for _, v := range page.Versions {
			flag := "VER "
			if aws.ToBool(v.IsLatest) {
				flag = "VER*"
			}
			myprint.PrintfGreen("%s %s %12d  %s  %s  version=%s\n",
				flag,
				v.LastModified.Format("2006-01-02 15:04:05"),
				aws.ToInt64(v.Size),
				aws.ToString(v.ETag),
				c.S3Path(bucket, aws.ToString(v.Key)),
				aws.ToString(v.VersionId))
		}
		for _, m := range page.DeleteMarkers {
			flag := "DEL "
			if aws.ToBool(m.IsLatest) {
				flag = "DEL*"
			}
			myprint.PrintfRed("%s %s              -                                  %s  version=%s\n",
				flag,
				m.LastModified.Format("2006-01-02 15:04:05"),
				c.S3Path(bucket, aws.ToString(m.Key)),
				aws.ToString(m.VersionId))
		}
	}
	return nil
}
