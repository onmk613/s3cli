// Package action 定义 S3 操作的接口抽象。
// 通过接口解耦命令层与具体实现，便于单元测试 mock 和未来扩展其他存储后端。
package action

// ============================================================================
// 组合接口 — 供命令层消费
// ============================================================================

// S3Operations 是所有 S3 操作的完整接口。
// 命令层应依赖此接口（或更小的子接口），而非具体的 *S3Client。
type S3Operations interface {
	ObjectReader
	ObjectWriter
	BucketManager
	BucketConfigurator
	MultipartManager
	TagManager
	Signer

	// Path helpers
	S3Path(bucket, key string) string
	IsS3File(bucket, key string) (bool, error)
	GetCreds() (Cred, error)
}

// ============================================================================
// 子接口 — 按职责拆分，允许命令仅声明自己需要的能力
// ============================================================================

// ObjectReader 对象读取操作。
type ObjectReader interface {
	ListObjects(bucket, prefix string, listAll bool) error
	GetObject(opt GetOptions, bucket, prefix, localpath string) error
	CatObject(opt CatOptions, bucket, key string) error
	Info(bucket, prefix string, outputJSON bool) error
	DuObject(bucket, prefix string) error
	FindObjects(opt FindOptions, bucket, prefix string) error
	TreeObjects(opt TreeOptions, bucket, prefix string) error
	ListOjbectVersions(bucket, prefix string) error
}

// ObjectWriter 对象写入 / 删除 / 移动操作。
type ObjectWriter interface {
	PutObject(opt PutOptions, bucket, prefix, localpath string, isS3Dir bool) error
	PipeUpload(opt PipeOptions, bucket, key string) error
	DeleteObjects(bucket, prefix string, recursive bool) error
	CopyObjects(srcBucket, srcKey, destBucket, destKey string, recursive bool, scrollMax int) error
	Mv(srcBucket, srcKey, destBucket, destKey string, recursive bool, scrollMax int) error
}

// BucketManager 桶的创建与删除。
type BucketManager interface {
	MakeBuckets(opt MakeBucketOptions, bucketname string) error
	RemoveBuckets(bucket string, force bool) error
}

// BucketConfigurator 桶级配置（CORS / 生命周期 / Policy / 加密 / 版本 / 通知）。
type BucketConfigurator interface {
	SetCors(corsFile string, bucket string) error
	GetCors(bucket string) error
	DelCors(bucket string) error
	SetLifecycle(lifecyclefile, bucketname string) error
	GetLifecycle(bucket string) error
	DelLifecycle(bucket string) error
	SetPolicy(policyfile, bucketname string) error
	GetPolicy(bucketname string) error
	DelPolicy(bucketname string) error
	SetEncryption(opt EncryptionOptions, bucket string) error
	GetEncryption(bucket string) error
	DelEncryption(bucket string) error
	SetVersioning(bucket string, status bool) error
	GetVersioning(bucket string) error
	SetNotification(configfile, bucket string) error
	GetNotification(bucket string) error
	DelNotification(bucket string) error
}

// MultipartManager 分片上传管理。
type MultipartManager interface {
	MpuList(bucket, prefix string) error
	MpuAbort(bucket, prefix, uploadID string) error
}

// TagManager 对象/桶标签管理。
type TagManager interface {
	SetTag(bucket, prefix string, tagStr map[string]string) error
	GetTag(bucket, prefix string) error
	DelTag(bucket, prefix string) error
}

// Signer 预签名 URL 生成。
type Signer interface {
	Signurl(opt SignurlOptions, bucketname, key string) error
}

// ============================================================================
// 编译时检查：确保 *S3Client 实现了所有子接口
// ============================================================================

var (
	_ ObjectReader       = (*S3Client)(nil)
	_ ObjectWriter       = (*S3Client)(nil)
	_ BucketManager      = (*S3Client)(nil)
	_ BucketConfigurator = (*S3Client)(nil)
	_ MultipartManager   = (*S3Client)(nil)
	_ TagManager         = (*S3Client)(nil)
	_ Signer             = (*S3Client)(nil)
)
