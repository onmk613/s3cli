package action

import (
	"fmt"

	myprint "s3cli/pkg/fmtutil"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func (c *S3Client) ListObjects(bucket, prefix string, listAll bool) error {
	if bucket == "" {
		result, err := c.S3.ListBuckets(c.Ctx, nil)
		if err != nil {
			return fmt.Errorf("list buckets: %s", FormatAPIError(err))
		}
		for _, bucket := range result.Buckets {
			myprint.Printf("%s   ", bucket.CreationDate.Format("2006-01-02 15:04"))
			myprint.PrintfGreen("%s\n", c.S3Path(aws.ToString(bucket.Name), ""))
		}
		return nil
	}
	return c.listObjectsV2(bucket, prefix, listAll)
}

func (c *S3Client) listObjectsV2(bucket, prefix string, listAll bool) error {
	// 计算 alias 列宽：最小 8 字符，取实际 alias 长度
	aliasW := len(c.Alias)
	if aliasW < 8 {
		aliasW = 8
	}

	var dirs, files []string
	var paginator *s3.ListObjectsV2Paginator

	if listAll {
		paginator = s3.NewListObjectsV2Paginator(c.S3, &s3.ListObjectsV2Input{
			Bucket: aws.String(bucket), Prefix: aws.String(prefix),
		})
	} else {
		paginator = s3.NewListObjectsV2Paginator(c.S3, &s3.ListObjectsV2Input{
			Bucket: aws.String(bucket), Prefix: aws.String(prefix),
			Delimiter: aws.String("/"),
		})
	}

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(c.Ctx)
		if err != nil {
			return fmt.Errorf("list objects: %s", FormatAPIError(err))
		}
		for _, p := range page.CommonPrefixes {
			dirs = append(dirs, fmt.Sprintf("%-*s  %-19s %12s   DIR   %s\n",
				aliasW, c.Alias, "", "-", keyPath(bucket, aws.ToString(p.Prefix))))
		}
		for _, item := range page.Contents {
			files = append(files, fmt.Sprintf("%-*s  %s %12d   FILE  %s\n",
				aliasW, c.Alias,
				item.LastModified.Format("2006-01-02 15:04:05"),
				aws.ToInt64(item.Size),
				keyPath(bucket, aws.ToString(item.Key))))
		}
	}
	for _, v := range dirs {
		myprint.PrintfBlue("%s", v)
	}
	for _, v := range files {
		myprint.PrintfGreen("%s", v)
	}
	return nil
}

// keyPath 返回 "bucket/key" 形式的路径（不含 alias / s3:// 前缀）。
func keyPath(bucket, key string) string {
	if key == "" {
		return bucket
	}
	return bucket + "/" + key
}
