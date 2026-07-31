// Package action 实现 s3cli 的业务操作层: 它在 s3iface.S3Operations 接口之上
// 组织面向命令的原子操作 (ls/put/get/cp/mv/rm/mirror/diff/info/tag/...).
//
// 设计要点:
//   - S3Client.S3 字段是 s3iface.S3Operations 接口 (见 common.go), 底层实现由
//     build tag 编译期选定: 默认自建请求的 s3api.Client, 加 -tags aws 时为官方
//     SDK 的 awss3.AWS (见 internal/client/backend-*.go). 业务操作层不感知具体实现;
//   - 流式传输 (put/get/cp/mv) 统一走 stream.go 的 RunStream, 提供并发、进度条与
//     预统计; 大文件自动走分片上传 (multipart-transfer.go) 并支持断点续传;
//   - mirror (object-mirror.go) 做双端流式归并同步, diff (diff.go) 做内容比对.
//
// 文件组织:
//   - interface.go:            操作接口定义与编译期实现检查
//   - common.go:               S3Client 核心、路径/存在性判断、对象遍历
//   - utils.go / awsfile.go:   通用工具与 AWS 配置文件解析
//   - stream.go:               流式传输框架 (RunStream)
//   - bucket-*.go:             桶级操作 (创建/删除/子资源配置)
//   - object-*.go:             对象级操作 (列举/上传/下载/复制/删除/标签等)
//   - multipart-*.go:          分片上传与断点续传
//   - object-mirror.go / mirror-stream.go / mirror-copy.go / mirror-manifest.go: 双端镜像同步
//   - diff.go:                 本地 / S3 内容差异比对
//   - share.go / info.go / ...: 预签名、元信息、shell 补全等杂项
package action
