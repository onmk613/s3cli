// Package s3iface 定义 S3 操作的中立接口与数据类型 (DTO).
//
// s3iface 不依赖任何具体 S3 后端实现 (既不依赖自建 HTTP 客户端 api,
// 也不依赖官方 AWS SDK). 它只定义:
//   - 操作接口 S3Operations (桶/对象/分片/预签名等全部底层操作)
//   - 各操作的输入输出 DTO 类型 (Options / Output)
//   - 桶子资源配置类型 (CORS / 加密 / 生命周期 / 通知 / 标签 / 版本)
//   - 分页器接口 (ListObjectsV2Paginator / ListObjectVersionsPaginator)
//
// api.Client (自建 HTTP + SigV4 签名) 是 S3Operations 的唯一实现.
//
// 上层 (internal/action) 只依赖 s3iface.S3Operations, 不感知具体后端.
package s3iface

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"time"
)

// DefaultXMLNS 是 S3 配置接口的标准命名空间 (规范为 http://, 不是 https://).
const DefaultXMLNS = "http://s3.amazonaws.com/doc/2006-03-01/"

// ----------------------------------------------------------------------------
// 对象列举类型
// ----------------------------------------------------------------------------

// Owner 对应 S3 响应中的 Owner 节点.
type Owner struct {
	ID          string
	DisplayName string
}

// ObjectInfo 对应 ListObjectsV2 / ListObjectVersions 响应中单个对象的元数据.
type ObjectInfo struct {
	Key          string
	LastModified time.Time
	ETag         string
	Size         int64
	StorageClass string
	Owner        *Owner
}

// ListObjectsV2Options 控制 ListObjectsV2 的可选参数.
type ListObjectsV2Options struct {
	Prefix            string
	Delimiter         string
	MaxKeys           int
	ContinuationToken string
	StartAfter        string
	FetchOwner        bool
}

// ListObjectsV2Output 是 ListObjectsV2 的返回结构.
type ListObjectsV2Output struct {
	Name                  string
	Prefix                string
	Delimiter             string
	MaxKeys               int
	KeyCount              int
	IsTruncated           bool
	ContinuationToken     string
	NextContinuationToken string
	StartAfter            string
	Contents              []ObjectInfo
	CommonPrefixes        []string
}

// ObjectVersion 对应 ListObjectVersions 响应中的 Version 节点.
type ObjectVersion struct {
	IsLatest     bool
	VersionID    string `xml:"VersionId"`
	Key          string
	LastModified time.Time
	ETag         string
	Size         int64
	StorageClass string
	Owner        *Owner
}

// DeleteMarker 对应 ListObjectVersions 响应中的 DeleteMarker 节点.
type DeleteMarker struct {
	IsLatest     bool
	VersionID    string `xml:"VersionId"`
	Key          string
	LastModified time.Time
	Owner        *Owner
}

// ListObjectVersionsOptions 控制 ListObjectVersions 的可选参数.
type ListObjectVersionsOptions struct {
	Prefix          string
	Delimiter       string
	MaxKeys         int
	KeyMarker       string
	VersionIDMarker string
}

// ListObjectVersionsOutput 是 ListObjectVersions 的返回结构.
type ListObjectVersionsOutput struct {
	Name                string
	Prefix              string
	Delimiter           string
	MaxKeys             int
	IsTruncated         bool
	KeyMarker           string
	VersionIDMarker     string
	NextKeyMarker       string
	NextVersionIDMarker string
	Versions            []ObjectVersion
	DeleteMarkers       []DeleteMarker
	CommonPrefixes      []string
}

// BucketInfo 描述单个 bucket 的基本信息.
type BucketInfo struct {
	Name         string
	CreationDate time.Time
	BucketRegion string
}

// MakeBucketOptions 控制 CreateBucket 的可选参数.
type MakeBucketOptions struct {
	Region        string
	ObjectLocking bool
}

// ----------------------------------------------------------------------------
// 对象操作类型
// ----------------------------------------------------------------------------

// HeadObjectOutput 是 HeadObject 的返回结构.
type HeadObjectOutput struct {
	ContentLength             int64
	ContentType               string
	ContentEncoding           string
	ContentDisposition        string
	ContentLanguage           string
	CacheControl              string
	Expires                   string
	ETag                      string
	LastModified              time.Time
	StorageClass              string
	VersionID                 string
	DeleteMarker              bool
	ServerSideEncryption      string
	SSEKMSKeyID               string
	SSECustomerAlgorithm      string
	SSECustomerKeyMD5         string
	PartsCount                int32
	ReplicationStatus         string
	ObjectLockMode            string
	ObjectLockRetainUntilDate time.Time
	ObjectLockLegalHold       string
	Metadata                  map[string]string
}

// GetObjectOptions 控制 GetObject 的可选参数.
type GetObjectOptions struct {
	VersionID                  string
	Range                      string
	IfMatch                    string
	IfNoneMatch                string
	IfModifiedSince            *time.Time
	IfUnmodifiedSince          *time.Time
	ResponseContentType        string
	ResponseContentEncoding    string
	ResponseContentDisposition string
	ResponseCacheControl       string
	ResponseExpires            string

	// SSE-C: 用客户提供的密钥解密已加密的对象.
	SSECustomerAlgorithm string
	SSECustomerKey       string
	SSECustomerKeyMD5    string
}

// GetObjectOutput 是 GetObject 的返回结构.
type GetObjectOutput struct {
	Body                      io.ReadCloser
	ContentLength             int64
	ContentType               string
	ContentEncoding           string
	ContentDisposition        string
	ContentLanguage           string
	CacheControl              string
	Expires                   string
	ETag                      string
	LastModified              time.Time
	StorageClass              string
	VersionID                 string
	DeleteMarker              bool
	ServerSideEncryption      string
	SSEKMSKeyID               string
	SSECustomerAlgorithm      string
	SSECustomerKeyMD5         string
	PartsCount                int32
	ReplicationStatus         string
	ObjectLockMode            string
	ObjectLockRetainUntilDate time.Time
	ObjectLockLegalHold       string
	AcceptRanges              string
	Metadata                  map[string]string
}

// PutObjectOptions 控制 PutObject 的可选参数.
type PutObjectOptions struct {
	ContentType          string
	ContentEncoding      string
	ContentDisposition   string
	ContentLanguage      string
	CacheControl         string
	StorageClass         string
	Metadata             map[string]string
	Tagging              string // 'k1=v1&k2=v2'
	ServerSideEncryption string
	SSEKMSKeyID          string
	// SSE-C (客户提供密钥): 算法固定 AES256; Key 为 base64 编码的 32 字节原始密钥;
	// KeyMD5 为 base64 编码的密钥 MD5.
	SSECustomerAlgorithm      string
	SSECustomerKey            string
	SSECustomerKeyMD5         string
	ObjectLockMode            string
	ObjectLockRetainUntilDate string
	ObjectLockLegalHold       string
}

// PutObjectOutput 是 PutObject 的返回结构.
type PutObjectOutput struct {
	ETag                 string
	VersionID            string
	ServerSideEncryption string
	SSEKMSKeyID          string
}

// CopyObjectOptions 控制 CopyObject 的可选参数.
type CopyObjectOptions struct {
	SourceVersionID      string
	MetadataDirective    string
	TaggingDirective     string
	Metadata             map[string]string
	Tagging              string
	StorageClass         string
	ContentType          string
	ServerSideEncryption string
	SSEKMSKeyID          string
	// 目标端 SSE-C (加密写入目标对象)
	SSECustomerAlgorithm string
	SSECustomerKey       string
	SSECustomerKeyMD5    string
	// 源端 SSE-C (解密用 SSE-C 加密的源对象)
	SourceSSECustomerAlgorithm string
	SourceSSECustomerKey       string
	SourceSSECustomerKeyMD5    string
	IfMatch                    string
	IfNoneMatch                string
	IfModifiedSince            string
	IfUnmodifiedSince          string
}

// UploadPartCopyOptions 控制 UploadPartCopy (服务端分片复制, 用于 >5GB 对象的服务端拷贝).
// 通过 CopySourceRange 从源对象截取一段作为一个分片, 服务端零下载完成大对象拷贝.
type UploadPartCopyOptions struct {
	SrcVersionID    string // 源对象版本
	CopySourceRange string // 源字节范围 "bytes=start-end", 复制大对象分片时必填

	// 源端 SSE-C (解密源对象)
	SourceSSECustomerAlgorithm string
	SourceSSECustomerKey       string
	SourceSSECustomerKeyMD5    string
	// 目标端 SSE-C (加密目标分片)
	SSECustomerAlgorithm string
	SSECustomerKey       string
	SSECustomerKeyMD5    string
}

// UploadPartCopyOutput 是 UploadPartCopy 的返回结构.
type UploadPartCopyOutput struct {
	ETag                 string
	LastModified         time.Time
	ServerSideEncryption string
	SSEKMSKeyID          string
}

// CopyObjectOutput 是 CopyObject 的返回结构.
type CopyObjectOutput struct {
	ETag                 string
	LastModified         string
	VersionID            string
	ServerSideEncryption string
	SSEKMSKeyID          string
}

// DeleteObjectOutput 是 DeleteObject 的返回结构.
type DeleteObjectOutput struct {
	VersionID    string
	DeleteMarker bool
}

// ObjectIdentifier 标识要删除的对象 (key + 可选 versionID).
type ObjectIdentifier struct {
	Key       string `xml:"Key"`
	VersionID string `xml:"VersionId,omitempty"`
}

// DeletedObject 描述已成功删除的对象.
type DeletedObject struct {
	Key          string
	VersionID    string
	DeleteMarker bool
}

// DeleteObjectError 描述批量删除中单个对象的失败.
type DeleteObjectError struct {
	Key     string
	Code    string
	Message string
}

// DeleteObjectsOutput 是 DeleteObjects 的返回结构.
type DeleteObjectsOutput struct {
	Deleted []DeletedObject
	Errors  []DeleteObjectError
}

// ----------------------------------------------------------------------------
// 分片上传类型
// ----------------------------------------------------------------------------

// CreateMultipartUploadOutput 是 CreateMultipartUpload 的返回结构.
type CreateMultipartUploadOutput struct {
	UploadID             string
	ServerSideEncryption string
	SSEKMSKeyID          string
}

// UploadPartOutput 是 UploadPart 的返回结构.
type UploadPartOutput struct {
	ETag                 string
	SSEKMSKeyID          string
	ServerSideEncryption string
}

// CompletedPart 已上传完成的分片信息.
type CompletedPart struct {
	XMLName    xml.Name `xml:"Part"`
	PartNumber int      `xml:"PartNumber"`
	ETag       string   `xml:"ETag"`
}

// CompleteMultipartUploadOutput 是 CompleteMultipartUpload 的返回结构.
type CompleteMultipartUploadOutput struct {
	Location             string
	Bucket               string
	Key                  string
	ETag                 string
	VersionID            string
	ServerSideEncryption string
	SSEKMSKeyID          string
}

// UploadInfo 单个进行中的分片上传.
type UploadInfo struct {
	XMLName      xml.Name `xml:"Upload"`
	Key          string
	UploadID     string `xml:"UploadId"`
	Initiated    time.Time
	StorageClass string
}

// ListMultipartUploadsOptions 控制 ListMultipartUploads 的可选参数.
type ListMultipartUploadsOptions struct {
	Prefix         string
	Delimiter      string
	KeyMarker      string
	UploadIDMarker string
	MaxUploads     int
}

// ListMultipartUploadsOutput 是 ListMultipartUploads 的返回结构.
type ListMultipartUploadsOutput struct {
	Bucket             string
	KeyMarker          string
	UploadIDMarker     string
	NextKeyMarker      string
	NextUploadIDMarker string
	MaxUploads         int
	IsTruncated        bool
	Uploads            []UploadInfo
}

// PartInfo 单个分片信息.
type PartInfo struct {
	XMLName      xml.Name `xml:"Part"`
	PartNumber   int
	LastModified time.Time
	ETag         string
	Size         int64
}

// ListPartsOutput 是 ListParts 的返回结构.
type ListPartsOutput struct {
	Bucket               string
	Key                  string
	UploadID             string
	PartNumberMarker     int
	NextPartNumberMarker int
	MaxParts             int
	IsTruncated          bool
	Parts                []PartInfo
}

// ----------------------------------------------------------------------------
// 预签名 / 错误类型
// ----------------------------------------------------------------------------

// PresignOptions 控制预签名 URL 的生成.
type PresignOptions struct {
	Method                     string
	Expires                    time.Duration
	VersionID                  string
	ResponseContentType        string
	ResponseContentDisposition string
	ResponseCacheControl       string
}

// ErrorResponse 对应 S3 XML 错误响应体, 实现 error 接口.
type ErrorResponse struct {
	XMLName    xml.Name `xml:"Error" json:"-"`
	Code       string   `xml:"Code" json:"code,omitempty"`
	Message    string   `xml:"Message" json:"message,omitempty"`
	BucketName string   `xml:"BucketName" json:"bucketName,omitempty"`
	Key        string   `xml:"Key" json:"key,omitempty"`
	Resource   string   `xml:"Resource" json:"resource,omitempty"`
	RequestID  string   `xml:"RequestId" json:"requestId,omitempty"`
	HostID     string   `xml:"HostId" json:"hostId,omitempty"`
	Region     string   `xml:"Region" json:"region,omitempty"`
	StatusCode int      `xml:"-" json:"statusCode,omitempty"`
}

// Error 实现 error 接口，返回 JSON 字符串.
func (e *ErrorResponse) Error() string {
	b, err := json.Marshal(e)
	if err != nil {
		return fmt.Sprintf("%s: %s (status: %d, request-id: %s)",
			e.Code, e.Message, e.StatusCode, e.RequestID)
	}
	return string(b)
}
