// types.go 定义全部 S3 操作的输入输出数据类型 (DTO): 对象列举 / 对象元数据与
// 增删改查 / 分片上传 / 预签名 / 错误响应.
//
// 这些类型同时是 api 对上层的公开契约: 上层 (internal/action) 只使用本文件与
// operations.go 中的接口类型, 不感知 HTTP / 签名 / XML 细节.
package api

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"
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

	// ChecksumMode = "ENABLED" 时请求服务端在响应里返回附加校验和
	// (x-amz-checksum-*), 供调用方校验下载完整性。
	ChecksumMode string
}

// ChecksumEnabled 报告是否请求了附加校验和。
func (o *GetObjectOptions) ChecksumEnabled() bool {
	return o != nil && strings.EqualFold(o.ChecksumMode, "ENABLED")
}

// GetObjectOutput 是 GetObject 的返回结构.
type GetObjectOutput struct {
	Body          io.ReadCloser
	Checksum      ChecksumValues
	ContentLength int64
	// ContentRange 是 Range 请求响应的原始 Content-Range 头 (如
	// "bytes 0-1023/2048"); 非 Range 请求为空。跨端复制的分片校验依赖它
	// 判定服务端确实按请求的范围返回 (个别网关会忽略 Range 返回 200 全量)。
	ContentRange              string
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

	// ChecksumAlgorithm 启用附加校验和 (CRC32/CRC32C/SHA1/SHA256)。
	// 空串表示不启用 (仍会发送 Content-MD5)。分片上传时它同时决定
	// CreateMultipartUpload 的 x-amz-sdk-checksum-algorithm 与每个分片的
	// x-amz-checksum-<alg>, 并在 CompleteMultipartUpload 中回传。
	ChecksumAlgorithm ChecksumAlgorithm
}

// PutObjectOutput 是 PutObject 的返回结构.
type PutObjectOutput struct {
	ETag                 string
	VersionID            string
	ServerSideEncryption string
	SSEKMSKeyID          string
	Checksum             ChecksumValues
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
	Checksum             ChecksumValues
}

// CompletedPart 已上传完成的分片信息.
type CompletedPart struct {
	XMLName    xml.Name `xml:"Part"`
	PartNumber int      `xml:"PartNumber"`
	ETag       string   `xml:"ETag"`

	// 附加校验和: 只填启用算法对应的那一个字段 (其余保持空, 由 omitempty 省略)。
	// 服务端在 CompleteMultipartUpload 时会用它校验每个分片。
	ChecksumCRC32  string `xml:"ChecksumCRC32,omitempty"`
	ChecksumCRC32C string `xml:"ChecksumCRC32C,omitempty"`
	ChecksumSHA1   string `xml:"ChecksumSHA1,omitempty"`
	ChecksumSHA256 string `xml:"ChecksumSHA256,omitempty"`
}

// SetChecksum 写入指定算法的校验和 (其余字段清空)。
func (p *CompletedPart) SetChecksum(alg ChecksumAlgorithm, v ChecksumValues) {
	p.ChecksumCRC32, p.ChecksumCRC32C, p.ChecksumSHA1, p.ChecksumSHA256 = "", "", "", ""
	switch alg {
	case ChecksumCRC32:
		p.ChecksumCRC32 = v.CRC32
	case ChecksumCRC32C:
		p.ChecksumCRC32C = v.CRC32C
	case ChecksumSHA1:
		p.ChecksumSHA1 = v.SHA1
	case ChecksumSHA256:
		p.ChecksumSHA256 = v.SHA256
	}
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

	// 分片校验和 (仅创建上传时声明了算法的服务端会返回)。
	// 断点续传从 ListParts 重建 CompletedPart 时必须原样带回, 否则带校验和的
	// 分片上传在 Complete 阶段可能被严格服务端拒绝, 或静默绕过整片校验。
	ChecksumCRC32  string `xml:"ChecksumCRC32,omitempty"`
	ChecksumCRC32C string `xml:"ChecksumCRC32C,omitempty"`
	ChecksumSHA1   string `xml:"ChecksumSHA1,omitempty"`
	ChecksumSHA256 string `xml:"ChecksumSHA256,omitempty"`
}

// ChecksumValues 返回该分片的全部校验和值 (未返回的字段为空串)。
func (p PartInfo) ChecksumValues() ChecksumValues {
	return ChecksumValues{
		CRC32:  p.ChecksumCRC32,
		CRC32C: p.ChecksumCRC32C,
		SHA1:   p.ChecksumSHA1,
		SHA256: p.ChecksumSHA256,
	}
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

// Error 实现 error 接口, 返回人类可读的 "Code: Message".
//
// 这里刻意不返回 JSON: 该字符串会直接出现在终端错误信息里, 且会被上层用
// %w 层层包装 (如 "list objects: NoSuchBucket: The specified bucket does
// not exist.")。结构化字段本身仍是导出的, 需要 JSON 时请显式 Marshal。
func (e *ErrorResponse) Error() string {
	switch {
	case e.Code != "" && e.Message != "":
		return e.Code + ": " + e.Message
	case e.Code != "":
		return e.Code
	case e.Message != "":
		return e.Message
	default:
		return fmt.Sprintf("S3 request failed with status %d", e.StatusCode)
	}
}

// HTTPStatus 返回响应的 HTTP 状态码 (0 表示未知)。
func (e *ErrorResponse) HTTPStatus() int { return e.StatusCode }
