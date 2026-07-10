package s3api

import "time"

type listAllMyBucketsResult struct {
	Owner   owner
	Buckets struct {
		Bucket []BucketInfo
	}
}

type BucketInfo struct {
	Name         string
	CreationDate time.Time
	BucketRegion string
}

type owner struct {
	ID          string
	DisplayName string
}

// ObjectInfo 对应 ListObjectsV2 响应中 Contents 节点的单个对象.
type ObjectInfo struct {
	Key          string
	LastModified time.Time
	ETag         string
	Size         int64
	StorageClass string
	Owner        *owner
}
