// interface.go 定义 awss3.AWS: 基于官方 aws-sdk-go-v2 的 s3iface.S3Operations 实现.
//
// AWS 将所有 S3 操作适配到中立接口 s3iface.S3Operations, 与自建 HTTP 客户端
// s3api.Client 可互换. 内部完成 SDK 类型 ↔ s3iface DTO 类型的双向转换.

package awss3

import (
	"context"

	"s3cli/pkg/s3iface"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// AWS 封装官方 S3 SDK 客户端, 实现 s3iface.S3Operations.
type AWS struct {
	client   *s3.Client
	presign  *s3.PresignClient
	creds    aws.Credentials // 构造时缓存, 供 AccessKey/SecretKey 等访问器使用
	endpoint string
}

// NewAWS 根据已配置的 *s3.Client 构造 AWS.
// ctx 用于预取凭证 (缓存后无需后续请求的 context).
func NewAWS(ctx context.Context, client *s3.Client) (*AWS, error) {
	opts := client.Options()
	a := &AWS{
		client:  client,
		presign: s3.NewPresignClient(client),
	}
	if opts.Credentials != nil {
		creds, err := opts.Credentials.Retrieve(ctx)
		if err != nil {
			return nil, err
		}
		a.creds = creds
	}
	if opts.BaseEndpoint != nil {
		a.endpoint = *opts.BaseEndpoint
	}
	return a, nil
}

// ---- 客户端元数据访问器 ----

func (a *AWS) AccessKey() string    { return a.creds.AccessKeyID }
func (a *AWS) SecretKey() string    { return a.creds.SecretAccessKey }
func (a *AWS) SessionToken() string { return a.creds.SessionToken }
func (a *AWS) Endpoint() string     { return a.endpoint }

// 编译期断言: AWS 必须实现 s3iface.S3Operations 的全部方法.
// 方法实现分布在 aws-bucket.go / aws-object.go / aws-list.go / aws-multipart.go /
// aws-config.go / aws-tagging.go / aws-presign.go.

// 确保 AWS 实现 S3Operations 接口.
var _ s3iface.S3Operations = (*AWS)(nil)
