package action

import (
	"fmt"
	"strings"

	myprint "s3cli/pkg/fmtutil"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// DelOpt Delete 命令参数
type DelOptions struct {
	Recursive bool
	VersionID string
}

// Rm 删除对象 / 目录
func (c *S3Client) DeleteObjects(bucket, prefix string, opt DelOptions) error {
	// 指定 versionId 时只删除该对象的特定版本，忽略 recursive / 目录语义。
	if opt.VersionID != "" {
		if opt.Recursive {
			return fmt.Errorf("--version-id cannot be used with -r/--recursive")
		}
		if strings.HasSuffix(prefix, "/") {
			return fmt.Errorf("%s: --version-id requires a single object key, not a directory", c.S3Path(bucket, prefix))
		}
		return c.deleteObjectVersion(bucket, prefix, opt.VersionID)
	}

	// 末尾带 "/" 明确表示目录：不能拿带 "/" 的 key 去 HeadObject（部分服务如 minio
	// 会报 "Object name contains unsupported characters"），直接按目录前缀处理。
	if strings.HasSuffix(prefix, "/") {
		if !opt.Recursive {
			return fmt.Errorf("%s: is a directory. Use -r/--recursive to delete it.", c.S3Path(bucket, prefix))
		}
		return c.deleteObjectsWithPrefix(bucket, prefix)
	}

	ok, err := c.IsS3File(bucket, prefix)
	if err != nil {
		return fmt.Errorf("check s3 path: %s", FormatAPIError(err))
	}
	switch {
	case !ok && opt.Recursive:
		if err := c.deleteObjectsWithPrefix(bucket, prefix); err != nil {
			return err
		}
	case !ok && !opt.Recursive:
		return fmt.Errorf("%s: not a single object. Use -r/--recursive to delete a directory.", c.S3Path(bucket, prefix))
	default:
		if err := c.deleteSingleObject(bucket, prefix); err != nil {
			return err
		}
	}
	return nil
}

func (c *S3Client) deleteSingleObject(bucket, key string) error {
	_, err := c.S3.DeleteObject(c.Ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket), Key: aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("delete %s: %s", c.S3Path(bucket, key), FormatAPIError(err))
	}

	myprint.PrintfBoldGreen("Delete %s: success\n", c.S3Path(bucket, key))
	return nil
}

func (c *S3Client) deleteObjectVersion(bucket, key, versionID string) error {
	_, err := c.S3.DeleteObject(c.Ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket), Key: aws.String(key), VersionId: aws.String(versionID),
	})
	if err != nil {
		return fmt.Errorf("delete %s (version %s): %s", c.S3Path(bucket, key), versionID, FormatAPIError(err))
	}

	myprint.PrintfBoldGreen("Delete %s (version %s): success\n", c.S3Path(bucket, key), versionID)
	return nil
}

func (c *S3Client) deleteObjectsWithPrefix(bucket, prefix string) error {
	// 规范化目录前缀：若 prefix 非空且不以 "/" 结尾，且 prefix+"/" 是一个真实目录，
	// 则改用 prefix+"/" 作为删除前缀。否则 "s3cli/.git" 会把同级的 "s3cli/.gitignore"
	// 一并误删（前缀匹配把 ".gitignore" 也命中了）。
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		dirPrefix := prefix + "/"
		listResp, err := c.S3.ListObjectsV2(c.Ctx, &s3.ListObjectsV2Input{
			Bucket: aws.String(bucket), Prefix: aws.String(dirPrefix), MaxKeys: aws.Int32(1),
		})
		if err != nil {
			return fmt.Errorf("list objects: %s", FormatAPIError(err))
		}
		if len(listResp.Contents) > 0 {
			prefix = dirPrefix
		}
	}

	paginator := s3.NewListObjectsV2Paginator(c.S3, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket), Prefix: aws.String(prefix),
	})

	var toDelete []types.ObjectIdentifier
	var total int
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(c.Ctx)
		if err != nil {
			return fmt.Errorf("list objects: %s", FormatAPIError(err))
		}
		for _, item := range page.Contents {
			toDelete = append(toDelete, types.ObjectIdentifier{Key: item.Key})
		}
		if len(toDelete) >= 1000 {
			if err := c.deleteBatch(bucket, toDelete); err != nil {
				return err
			}
			total += len(toDelete)
			toDelete = toDelete[:0]
		}
	}
	if len(toDelete) > 0 {
		if err := c.deleteBatch(bucket, toDelete); err != nil {
			return err
		}
		total += len(toDelete)
	}

	myprint.PrintfBoldGreen("Delete %d objects from %s: success\n", total, c.S3Path(bucket, prefix))
	return nil
}

func (c *S3Client) deleteBatch(bucket string, objects []types.ObjectIdentifier) error {
	_, err := c.S3.DeleteObjects(c.Ctx, &s3.DeleteObjectsInput{
		Bucket: aws.String(bucket),
		Delete: &types.Delete{Objects: objects, Quiet: aws.Bool(true)},
	})
	if err != nil {
		return fmt.Errorf("delete batch of %d: %s", len(objects), FormatAPIError(err))
	}
	return nil
}
