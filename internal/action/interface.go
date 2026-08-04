package action

// CmdOperations 是所有 cmd 操作抽象
type CmdOperations interface {
	ListObjects(opt ListOptions, bucket, prefix string) error
	GetObject(opt GetOptions, bucket, prefix, localPath string) error
	CatObject(opt CatOptions, bucket, key string) error
	Info(opt InfoOptions, bucket, prefix string) error
	StatObjects(opt StatOptions, bucket, prefix string) error
	DuObject(opt DuOptions, bucket, prefix string) error
	FindObjects(opt FindOptions, bucket, prefix string) error
	TreeObjects(opt TreeOptions, bucket, prefix string) error
	ListObjectVersions(bucket, prefix string) error

	PutObject(opt PutOptions, bucket, prefix, localPath string, isS3Dir bool) error
	PipeUpload(opt PipeOptions, bucket, key string) error
	DeleteObjects(bucket, prefix string, opt DelOptions) error
	CopyObjects(opt CopyOptions, srcBucket, srcKey, destBucket, destKey string) error
	Mv(opt CopyOptions, srcBucket, srcKey, destBucket, destKey string) error

	MakeBuckets(opt MakeBucketOptions, bucket string) error
	RemoveBuckets(bucket string, force bool) error

	SetCors(opt CorsOptions, bucket string) error
	GetCors(bucket string) error
	DelCors(bucket string) error
	SetLifecycle(opt LifecycleOptions, bucket string) error
	GetLifecycle(bucket string) error
	DelLifecycle(bucket string) error
	AddLifecycleRule(opt LifecycleRuleOptions, bucket string) error
	EditLifecycleRule(opt LifecycleRuleOptions, bucket string) error
	RemoveLifecycleRules(opt RemoveLifecycleOptions, bucket string) error
	ListLifecycle(bucket string, opt ListLifecycleOptions) error
	ExportLifecycle(bucket string) error
	ImportLifecycle(file, bucket string) error
	SetPolicy(opt PolicyOptions, bucket string) error
	GetPolicy(bucket string) error
	DelPolicy(bucket string) error
	SetEncryption(opt EncryptionOptions, bucket string) error
	GetEncryption(bucket string) error
	DelEncryption(bucket string) error
	SetVersioning(bucket string, status string) error
	GetVersioning(bucket string) error
	SetNotification(configFile, bucket string) error
	GetNotification(bucket string) error
	DelNotification(bucket string) error

	MpuList(opt MpuListOptions, bucket, prefix string) error
	MpuAbort(bucket, prefix, uploadID string) error

	SetTag(bucket, prefix string, tagStr map[string]string) error
	GetTag(opt TagOptions, bucket, prefix string) error
	DelTag(bucket, prefix string) error

	Share(opt ShareOptions, bucket, key string) error
	SelectObjects(opt SelectOptions, bucket, prefix string) error

	// S3Path Path helpers
	S3Path(bucket, key string) string
	IsS3File(bucket, key string) (bool, error)
	GetS3Credentials() (Cred, error)
}

// 确保
var _ CmdOperations = (*Action)(nil)
