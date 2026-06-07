package action

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

type S3Client struct {
	S3    *s3.Client
	Alias string
	Ctx   context.Context
}

// S3Path 格式化路径为 "s3://alias/bucket/key"（S3 标准 URI 格式）
func (c *S3Client) S3Path(bucket, key string) string {
	if key == "" {
		return c.Alias + ":" + bucket
	}
	return c.Alias + ":" + bucket + "/" + key
}

// S3PathStatic 静态版本，无需 S3Client 实例
func S3PathStatic(alias, bucket, key string) string {
	if key == "" {
		return alias + ":" + bucket
	}
	return alias + ":" + bucket + "/" + key
}

// IsS3File 检查路径是文件 (true) 还是目录 / 不存在 (false)
func (c *S3Client) IsS3File(bucket, key string) (bool, error) {
	// 空 key 表示目标是 bucket 本身，不可能是文件
	if key == "" {
		return false, nil
	}

	_, err := c.S3.HeadObject(c.Ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket), Key: aws.String(key),
	})
	if err == nil {
		return true, nil
	}
	var apiErr *smithy.GenericAPIError
	if !errors.As(err, &apiErr) {
		return false, err
	}
	switch apiErr.ErrorCode() {
	case "NotFound":
		return c.checkIfDirectory(bucket, key)
	case "Forbidden":
		return false, fmt.Errorf("access denied to bucket '%s'", bucket)
	}
	return false, fmt.Errorf("S3 error: %w", err)
}

func (c *S3Client) checkIfDirectory(bucket, key string) (bool, error) {
	listResp, err := c.S3.ListObjectsV2(c.Ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket), Prefix: aws.String(key + "/"),
		Delimiter: aws.String("/"), MaxKeys: aws.Int32(1),
	})
	if err != nil {
		return false, err
	}
	if len(listResp.CommonPrefixes) > 0 || len(listResp.Contents) > 0 {
		return false, nil
	}
	return false, fmt.Errorf("path '%s' does not exist in bucket '%s'", key, bucket)
}

type Cred struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	BaseEndpoint    string
}

func (c *S3Client) GetCreds() (Cred, error) {
	// 通过 Options() 拿到配置
	s3options := c.S3.Options()
	var awsCreds aws.Credentials

	// 从 CredentialsProvider 中 Retrieve 凭证
	if s3options.Credentials != nil {
		var err error
		awsCreds, err = s3options.Credentials.Retrieve(c.Ctx)
		if err != nil {
			return Cred{}, err
		}
	} else {
		return Cred{}, fmt.Errorf("no credentials provider found")
	}

	if s3options.BaseEndpoint == nil {
		return Cred{}, fmt.Errorf("endpoint not configured")
	}

	return Cred{
		AccessKeyID:     awsCreds.AccessKeyID,
		SecretAccessKey: awsCreds.SecretAccessKey,
		SessionToken:    awsCreds.SessionToken,
		BaseEndpoint:    *s3options.BaseEndpoint,
	}, nil
}
