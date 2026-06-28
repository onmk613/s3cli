package api

import (
	"encoding/xml"
	"time"
)

type BucketInfo struct {
	Name         string
	CreationDate time.Time
}

type xmlCORSConfiguration struct {
	XMLName xml.Name      `xml:"CORSConfiguration"`
	Rules   []xmlCORSRule `xml:"CORSRule"`
}

type xmlCORSRule struct {
	ID             string   `xml:"ID,omitempty"`
	AllowedOrigins []string `xml:"AllowedOrigin"`
	AllowedMethods []string `xml:"AllowedMethod"`
	AllowedHeaders []string `xml:"AllowedHeader"`
	ExposeHeaders  []string `xml:"ExposeHeader"`
	MaxAgeSeconds  *int32   `xml:"MaxAgeSeconds"`
}

// ============ 数据结构定义 ============ (test)

type ObjectInfo struct {
	Key          string
	Size         int64
	ETag         string
	LastModified time.Time
	ContentType  string
	Metadata     map[string]string
	StorageClass string
}

type PutOption func(*putOptions)

type putOptions struct {
	ContentType   string
	ContentLength int64
	Metadata      map[string]string
	StorageClass  string
}

func WithContentType(ct string) PutOption {
	return func(o *putOptions) { o.ContentType = ct }
}

func WithContentLength(length int64) PutOption {
	return func(o *putOptions) { o.ContentLength = length }
}

func WithMetadata(meta map[string]string) PutOption {
	return func(o *putOptions) { o.Metadata = meta }
}

func WithStorageClass(sc string) PutOption {
	return func(o *putOptions) { o.StorageClass = sc }
}

type ListOption func(*listOptions)

type listOptions struct {
	Delimiter         string
	MaxKeys           int32
	ContinuationToken string
}

func WithDelimiter(delimiter string) ListOption {
	return func(o *listOptions) { o.Delimiter = delimiter }
}

func WithMaxKeys(max int32) ListOption {
	return func(o *listOptions) { o.MaxKeys = max }
}

func WithContinuationToken(token string) ListOption {
	return func(o *listOptions) { o.ContinuationToken = token }
}

type ListObjectsResult struct {
	Objects        []ObjectInfo
	CommonPrefixes []string // 目录（配合 Delimiter 使用）
	IsTruncated    bool
	NextToken      string
}

type DeleteError struct {
	Key     string
	Code    string
	Message string
}

type CompletedPart struct {
	PartNumber int32
	ETag       string
}

type PartInfo struct {
	PartNumber   int32
	ETag         string
	Size         int64
	LastModified time.Time
}
