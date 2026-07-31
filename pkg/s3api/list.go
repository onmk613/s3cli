//go:build !aws

// list.go 实现对象列举: ListObjectsV2 与 ListObjectVersions, 以及对应的自动分页器.
// 分页器封装了 continuation-token / key-marker 的传递逻辑, 简化全量遍历场景.

package s3api

import (
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"s3cli/pkg/s3iface"
)

// listObjectsV2Result 对应 S3 ListObjectsV2 响应体.
//
//	<ListBucketResult>
//	  <Name>bucket</Name>
//	  <Prefix>prefix</Prefix>
//	  <KeyCount>1</KeyCount>
//	  <MaxKeys>1000</MaxKeys>
//	  <IsTruncated>true</IsTruncated>
//	  <NextContinuationToken>...</NextContinuationToken>
//	  <Contents>
//	    <Key>key</Key>
//	    <LastModified>2009-10-12T17:50:30.000Z</LastModified>
//	    <ETag>"etag"</ETag>
//	    <Size>1234</Size>
//	    <StorageClass>STANDARD</StorageClass>
//	    <Owner>...</Owner>
//	  </Contents>
//	  <CommonPrefixes>
//	    <Prefix>prefix/</Prefix>
//	  </CommonPrefixes>
//	</ListBucketResult>
type listObjectsV2Result struct {
	XMLName               xml.Name `xml:"ListBucketResult"`
	Name                  string
	Prefix                string
	Delimiter             string
	MaxKeys               int
	IsTruncated           bool
	ContinuationToken     string
	NextContinuationToken string
	KeyCount              int
	Contents              []ObjectInfo `xml:"Contents"`
	CommonPrefixes        []commonPrefix
	StartAfter            string
}

// commonPrefix 对应 ListObjectsV2 响应中的 CommonPrefixes 节点.
type commonPrefix struct {
	Prefix string
}

// ListObjectsV2 列出指定 bucket 下的对象.
//
// 支持 prefix / delimiter / 分页 (ContinuationToken) / StartAfter.
// 当 Delimiter 非空 (通常为 "/") 时, CommonPrefixes 返回"目录"前缀.
func (c *Client) ListObjectsV2(ctx context.Context, bucket string, opts *ListObjectsV2Options) (*ListObjectsV2Output, error) {
	if opts == nil {
		opts = &ListObjectsV2Options{}
	}

	urlValues := make(url.Values)
	urlValues.Set("list-type", "2")
	if opts.Prefix != "" {
		urlValues.Set("prefix", opts.Prefix)
	}
	if opts.Delimiter != "" {
		urlValues.Set("delimiter", opts.Delimiter)
	}
	if opts.MaxKeys > 0 {
		urlValues.Set("max-keys", strconv.Itoa(opts.MaxKeys))
	}
	if opts.ContinuationToken != "" {
		urlValues.Set("continuation-token", opts.ContinuationToken)
	}
	if opts.StartAfter != "" {
		urlValues.Set("start-after", opts.StartAfter)
	}
	if opts.FetchOwner {
		urlValues.Set("fetch-owner", "true")
	}

	reqMeta := requestMetadata{
		bucketName:  bucket,
		queryValues: urlValues,
	}

	resp, err := c.Do(ctx, http.MethodGet, reqMeta)
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	var result listObjectsV2Result
	if err := xmlDecoder(resp.Body, &result); err != nil {
		return nil, err
	}

	out := &ListObjectsV2Output{
		Name:                  result.Name,
		Prefix:                result.Prefix,
		Delimiter:             result.Delimiter,
		MaxKeys:               result.MaxKeys,
		KeyCount:              result.KeyCount,
		IsTruncated:           result.IsTruncated,
		ContinuationToken:     result.ContinuationToken,
		NextContinuationToken: result.NextContinuationToken,
		StartAfter:            result.StartAfter,
		Contents:              result.Contents,
	}
	for _, cp := range result.CommonPrefixes {
		out.CommonPrefixes = append(out.CommonPrefixes, cp.Prefix)
	}
	return out, nil
}

// ListObjectsV2Paginator 封装 ListObjectsV2 的自动分页逻辑.
type ListObjectsV2Paginator struct {
	client    *Client
	bucket    string
	opts      *ListObjectsV2Options
	token     string
	hasMore   bool
	firstPage bool
}

// NewListObjectsV2Paginator 创建一个分页器.
func NewListObjectsV2Paginator(client *Client, bucket string, opts *ListObjectsV2Options) *ListObjectsV2Paginator {
	return &ListObjectsV2Paginator{
		client:    client,
		bucket:    bucket,
		opts:      opts,
		firstPage: true,
		hasMore:   true,
	}
}

// HasMorePages 返回是否还有更多页.
func (p *ListObjectsV2Paginator) HasMorePages() bool {
	return p.hasMore
}

// NextPage 获取下一页.
func (p *ListObjectsV2Paginator) NextPage(ctx context.Context) (*ListObjectsV2Output, error) {
	if !p.hasMore {
		return nil, io.EOF
	}

	o := *p.opts
	if p.token != "" {
		o.ContinuationToken = p.token
	}

	out, err := p.client.ListObjectsV2(ctx, p.bucket, &o)
	if err != nil {
		p.hasMore = false
		return nil, err
	}

	p.firstPage = false
	if out.IsTruncated && out.NextContinuationToken != "" {
		p.token = out.NextContinuationToken
		p.hasMore = true
	} else {
		p.hasMore = false
	}
	return out, nil
}

// listObjectVersionsResult 对应 S3 ListObjectVersions 响应体.
type listObjectVersionsResult struct {
	XMLName             xml.Name `xml:"ListVersionsResult"`
	Name                string
	Prefix              string
	Delimiter           string
	MaxKeys             int
	IsTruncated         bool
	KeyMarker           string
	VersionIDMarker     string
	NextKeyMarker       string
	NextVersionIDMarker string
	Versions            []objectVersion `xml:"Version"`
	DeleteMarkers       []deleteMarker  `xml:"DeleteMarker"`
	CommonPrefixes      []commonPrefix
}

// objectVersion / deleteMarker 类型别名定义在 s3iface_types.go.

// ListObjectVersionsOptions / ListObjectVersionsOutput 类型别名定义在 s3iface_types.go.

// ListObjectVersions 列出 bucket 下对象的所有版本 (含 delete marker).
func (c *Client) ListObjectVersions(ctx context.Context, bucket string, opts *ListObjectVersionsOptions) (*ListObjectVersionsOutput, error) {
	if opts == nil {
		opts = &ListObjectVersionsOptions{}
	}

	urlValues := make(url.Values)
	urlValues.Set("versions", "")
	if opts.Prefix != "" {
		urlValues.Set("prefix", opts.Prefix)
	}
	if opts.Delimiter != "" {
		urlValues.Set("delimiter", opts.Delimiter)
	}
	if opts.MaxKeys > 0 {
		urlValues.Set("max-keys", strconv.Itoa(opts.MaxKeys))
	}
	if opts.KeyMarker != "" {
		urlValues.Set("key-marker", opts.KeyMarker)
	}
	if opts.VersionIDMarker != "" {
		urlValues.Set("version-id-marker", opts.VersionIDMarker)
	}

	reqMeta := requestMetadata{
		bucketName:  bucket,
		queryValues: urlValues,
	}

	resp, err := c.Do(ctx, http.MethodGet, reqMeta)
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	var result listObjectVersionsResult
	if err := xmlDecoder(resp.Body, &result); err != nil {
		return nil, err
	}

	out := &ListObjectVersionsOutput{
		Name:                result.Name,
		Prefix:              result.Prefix,
		Delimiter:           result.Delimiter,
		MaxKeys:             result.MaxKeys,
		IsTruncated:         result.IsTruncated,
		KeyMarker:           result.KeyMarker,
		VersionIDMarker:     result.VersionIDMarker,
		NextKeyMarker:       result.NextKeyMarker,
		NextVersionIDMarker: result.NextVersionIDMarker,
		Versions:            result.Versions,
		DeleteMarkers:       result.DeleteMarkers,
	}
	for _, cp := range result.CommonPrefixes {
		out.CommonPrefixes = append(out.CommonPrefixes, cp.Prefix)
	}
	return out, nil
}

// ListObjectVersionsPaginator 封装 ListObjectVersions 的自动分页逻辑.
type ListObjectVersionsPaginator struct {
	client    *Client
	bucket    string
	opts      *ListObjectVersionsOptions
	keyMarker string
	verMarker string
	hasMore   bool
}

// NewListObjectVersionsPaginator 创建一个分页器.
func NewListObjectVersionsPaginator(client *Client, bucket string, opts *ListObjectVersionsOptions) *ListObjectVersionsPaginator {
	return &ListObjectVersionsPaginator{
		client:  client,
		bucket:  bucket,
		opts:    opts,
		hasMore: true,
	}
}

// HasMorePages 返回是否还有更多页.
func (p *ListObjectVersionsPaginator) HasMorePages() bool {
	return p.hasMore
}

// NextPage 获取下一页.
func (p *ListObjectVersionsPaginator) NextPage(ctx context.Context) (*ListObjectVersionsOutput, error) {
	if !p.hasMore {
		return nil, io.EOF
	}

	o := *p.opts
	if p.keyMarker != "" {
		o.KeyMarker = p.keyMarker
	}
	if p.verMarker != "" {
		o.VersionIDMarker = p.verMarker
	}

	out, err := p.client.ListObjectVersions(ctx, p.bucket, &o)
	if err != nil {
		p.hasMore = false
		return nil, err
	}

	if out.IsTruncated && out.NextKeyMarker != "" {
		p.keyMarker = out.NextKeyMarker
		p.verMarker = out.NextVersionIDMarker
		p.hasMore = true
	} else {
		p.hasMore = false
	}
	return out, nil
}

// NewListObjectsV2Paginator 是 Client 的方法, 满足 s3iface.S3Operations 接口.
// 委托给包级构造函数.
func (c *Client) NewListObjectsV2Paginator(bucket string, opts *ListObjectsV2Options) s3iface.ListObjectsV2Paginator {
	return NewListObjectsV2Paginator(c, bucket, opts)
}

// NewListObjectVersionsPaginator 是 Client 的方法, 满足 s3iface.S3Operations 接口.
func (c *Client) NewListObjectVersionsPaginator(bucket string, opts *ListObjectVersionsOptions) s3iface.ListObjectVersionsPaginator {
	return NewListObjectVersionsPaginator(c, bucket, opts)
}
