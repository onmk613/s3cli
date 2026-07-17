package action

import (
	"fmt"
	"strings"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/s3api"
)

func (c *S3Client) ListObjects(bucket, prefix string, listAll bool) error {
	if bucket == "" {
		buckets, err := c.S3.ListBuckets(c.Ctx)
		if err != nil {
			return fmt.Errorf("list buckets: %s", FormatAPIError(err))
		}
		for _, bucket := range buckets {
			myprint.PrintfDim("[%s]   ", bucket.CreationDate.Format("2006-01-02 15:04"))
			myprint.PrintfGreen("%s\n", c.S3Path(bucket.Name, ""))
		}
		return nil
	}
	return c.listObjectsV2(bucket, prefix, listAll)
}

func (c *S3Client) listObjectsV2(bucket, prefix string, listAll bool) error {
	opts := &s3api.ListObjectsV2Options{
		Prefix: prefix,
	}
	if !listAll {
		opts.Delimiter = "/"
	}

	var hasOutput bool
	paginator := s3api.NewListObjectsV2Paginator(c.S3, bucket, opts)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(c.Ctx)
		if err != nil {
			return fmt.Errorf("list objects: %s", FormatAPIError(err))
		}
		for _, p := range page.CommonPrefixes {
			hasOutput = true
			myprint.PrintfBlue("%-22s %12s   DIR   %s\n", "", "-", c.S3Path(bucket, p))
		}
		for _, item := range page.Contents {
			hasOutput = true
			// 目录标记对象 (以 "/" 结尾且 0 字节) 显示为 DIR
			if strings.HasSuffix(item.Key, "/") && item.Size == 0 {
				myprint.PrintfBlue("%-22s %12s   DIR   %s\n", "", "-", c.S3Path(bucket, item.Key))
				continue
			}
			myprint.PrintfDim("[%s]  ", item.LastModified.Format("2006-01-02 15:04:05"))
			myprint.Printf("%12d   ", item.Size)
			myprint.PrintfGreen("FILE  %s\n", c.S3Path(bucket, item.Key))
		}
	}

	// 递归模式 (ls -a) 下如果完全没有输出，回退到非递归列举以显示一级目录。
	// 某些 S3 实现 (如 SeaweedFS) 在无 delimiter 时不返回目录标记对象，
	// 导致只有空目录的前缀递归列举结果为空。
	if listAll && !hasOutput {
		return c.listObjectsV2(bucket, prefix, false)
	}
	return nil
}
