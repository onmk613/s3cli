// Package s3api 提供一个不依赖 AWS SDK 的轻量级 S3 兼容客户端.
//
// 它基于 net/http 直接实现 S3 REST API. 常规请求统一使用 AWS Signature Version 4
// 签名; 预签名 URL 同时支持 SigV4 (PresignedURL) 与 SigV2 (PresignV2, 兼容旧式服务).
// 可对接 AWS S3、MinIO、SeaweedFS、Ceph RGW 等任何 S3 兼容的对象存储服务.
//
// 典型用法:
//
//	client, err := s3api.New(&s3api.Options{
//	    Endpoint:  "https://s3.example.com",
//	    AccessKey: "AKIA...",
//	    SecretKey: "secret",
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	objs, err := client.ListObjectsV2(ctx, "my-bucket", &s3api.ListObjectsV2Options{Prefix: "2024/"})
//
// 文件组织:
//   - api.go:              Client 核心类型、请求构建、发送、重试
//   - signer.go:           AWS SigV4 签名实现
//   - error.go:            S3 错误响应解析
//   - utils.go:            通用工具函数 (哈希、编码、桶名校验等)
//   - bucket-*.go:         桶级操作 (创建/删除/子资源配置)
//   - object-*.go:         对象级操作 (上传/下载/复制/删除/标签等)
//   - list.go:             ListObjectsV2 / ListObjectVersions 及分页器
//   - multipart-upload.go: 分片上传相关操作
//   - presigned.go:        预签名 URL (SigV4 / SigV2)
package s3api
