package action

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

type S3Client struct {
	S3    *s3.Client
	Alias string
	Ctx   context.Context
}

// S3Path 格式化路径为 "alias:bucket/key", 和命令行格式一样
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

	// 优先按 HTTP 状态码判断（兼容各类 S3 服务对 404/403 的不同错误类型）。
	// HeadObject 无响应体，SDK 对 404 可能解析为 *types.NotFound、*types.NoSuchKey、
	// 或仅是带状态码的 *http.ResponseError，统一无法保证是 *smithy.GenericAPIError。
	var respErr *awshttp.ResponseError
	if errors.As(err, &respErr) {
		switch respErr.HTTPStatusCode() {
		case 404:
			// 对象不存在：可能是目录前缀，继续探测。
			return c.checkIfDirectory(bucket, key)
		case 403:
			return false, fmt.Errorf("access denied to bucket '%s'", bucket)
		}
	}

	// 再按 smithy.APIError 接口的错误码兜底（接口而非具体类型，覆盖面更广）。
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "NotFound", "NoSuchKey", "404":
			return c.checkIfDirectory(bucket, key)
		case "Forbidden", "AccessDenied", "403":
			return false, fmt.Errorf("access denied to bucket '%s'", bucket)
		}
	}
	return false, fmt.Errorf("S3 error: %w", err)
}

// checkIfDirectory 在 HeadObject 返回 404 后判断 key 是否为目录前缀。
// 返回 (false, nil) 表示是目录前缀（非文件），(false, err) 表示路径不存在。
func (c *S3Client) checkIfDirectory(bucket, key string) (bool, error) {
	// 1) 先按标准目录探测：prefix = key + "/"。
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

	// 2) 兜底：用裸 key 作为前缀探测（覆盖无尾斜杠的伪目录前缀，
	//    例如 key="dir/name" 实际匹配 "dir/name/..." 或 "dir/name-xxx"）。
	listResp2, err := c.S3.ListObjectsV2(c.Ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket), Prefix: aws.String(key), MaxKeys: aws.Int32(1),
	})
	if err != nil {
		return false, err
	}
	if len(listResp2.Contents) > 0 {
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
