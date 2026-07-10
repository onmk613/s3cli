package action

import (
	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/s3api"
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
	var totalSize, diskSize, count int64
	err := c.forEachObject(c.Ctx, bucket, prefix, func(o s3api.ObjectInfo) error {
		sz := o.Size
		totalSize += sz                               // 真实文件大小累加（始终）
		diskSize += roundUpToBlock(sz, opt.BlockSize) // 按块向上取整的磁盘占用
		count++
		return nil
	})
	if err != nil {
		return err
	}

	myprint.PrintfBoldBlue("Path: %s, FileNum: %d, Size: %d, RealSize: %s", c.S3Path(bucket, prefix), count, totalSize, FormatBytes(totalSize))
	if opt.BlockSize > 0 {
		myprint.PrintfBoldBlue(", DiskSize: %s\n", FormatBytes(diskSize))
		return nil
	}
	myprint.Printf("\n")
	return nil
}
