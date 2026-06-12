package action

import (
	"fmt"

	myprint "s3cli/pkg/fmtutil"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// DuOptions du 命令参数。
type DuOptions struct {
	BlockSize int64
}

func roundUpToBlock(size, block int64) int64 {
	if block <= 0 || size <= 0 {
		return size
	}
	return ((size + block - 1) / block) * block
}

// Du 显示磁盘占用, 只支持bucket及以下级别
func (c *S3Client) DuObject(opt DuOptions, bucket, prefix string) error {
	paginator := s3.NewListObjectsV2Paginator(c.S3, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket), Prefix: aws.String(prefix),
	})
	var totalSize, diskSize, count int64
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(c.Ctx)
		if err != nil {
			return fmt.Errorf("list objects in %s: %s", c.S3Path(bucket, prefix), FormatAPIError(err))
		}
		for _, o := range page.Contents {
			sz := aws.ToInt64(o.Size)
			totalSize += sz                               // 真实文件大小累加（始终）
			diskSize += roundUpToBlock(sz, opt.BlockSize) // 按块向上取整的磁盘占用
			count++
		}
	}

	myprint.PrintfBoldBlue("Path: %s, FileNum: %d, Size: %d, RealSize: %s", c.S3Path(bucket, prefix), count, totalSize, FormatBytes(totalSize))
	if opt.BlockSize > 0 {
		myprint.PrintfBoldBlue(", DiskSize: %s\n", FormatBytes(diskSize))
		return nil
	}
	myprint.Printf("\n")
	return nil
}
