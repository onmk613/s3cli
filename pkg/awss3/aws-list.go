//go:build aws

// aws-list.go 实现 AWS 上的对象列举: ListObjectsV2 / ListObjectVersions 及分页器.

package awss3

import (
	"context"

	"s3cli/pkg/s3iface"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func (a *AWS) ListObjectsV2(ctx context.Context, bucket string, opts *s3iface.ListObjectsV2Options) (*s3iface.ListObjectsV2Output, error) {
	if opts == nil {
		opts = &s3iface.ListObjectsV2Options{}
	}
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
	}
	if opts.Prefix != "" {
		input.Prefix = aws.String(opts.Prefix)
	}
	if opts.Delimiter != "" {
		input.Delimiter = aws.String(opts.Delimiter)
	}
	if opts.MaxKeys > 0 {
		input.MaxKeys = aws.Int32(int32(opts.MaxKeys))
	}
	if opts.ContinuationToken != "" {
		input.ContinuationToken = aws.String(opts.ContinuationToken)
	}
	if opts.StartAfter != "" {
		input.StartAfter = aws.String(opts.StartAfter)
	}
	if opts.FetchOwner {
		input.FetchOwner = aws.Bool(true)
	}

	out, err := a.client.ListObjectsV2(ctx, input)
	if err != nil {
		return nil, sdkErr(err)
	}
	result := &s3iface.ListObjectsV2Output{
		Name:                  aws.ToString(out.Name),
		Prefix:                aws.ToString(out.Prefix),
		Delimiter:             aws.ToString(out.Delimiter),
		MaxKeys:               int(aws.ToInt32(out.MaxKeys)),
		KeyCount:              int(aws.ToInt32(out.KeyCount)),
		IsTruncated:           aws.ToBool(out.IsTruncated),
		ContinuationToken:     aws.ToString(out.ContinuationToken),
		NextContinuationToken: aws.ToString(out.NextContinuationToken),
		StartAfter:            aws.ToString(out.StartAfter),
	}
	for _, o := range out.Contents {
		result.Contents = append(result.Contents, toObjectInfo(o))
	}
	for _, p := range out.CommonPrefixes {
		result.CommonPrefixes = append(result.CommonPrefixes, aws.ToString(p.Prefix))
	}
	return result, nil
}

func (a *AWS) ListObjectVersions(ctx context.Context, bucket string, opts *s3iface.ListObjectVersionsOptions) (*s3iface.ListObjectVersionsOutput, error) {
	if opts == nil {
		opts = &s3iface.ListObjectVersionsOptions{}
	}
	input := &s3.ListObjectVersionsInput{
		Bucket: aws.String(bucket),
	}
	if opts.Prefix != "" {
		input.Prefix = aws.String(opts.Prefix)
	}
	if opts.Delimiter != "" {
		input.Delimiter = aws.String(opts.Delimiter)
	}
	if opts.MaxKeys > 0 {
		input.MaxKeys = aws.Int32(int32(opts.MaxKeys))
	}
	if opts.KeyMarker != "" {
		input.KeyMarker = aws.String(opts.KeyMarker)
	}
	if opts.VersionIDMarker != "" {
		input.VersionIdMarker = aws.String(opts.VersionIDMarker)
	}

	out, err := a.client.ListObjectVersions(ctx, input)
	if err != nil {
		return nil, sdkErr(err)
	}
	result := &s3iface.ListObjectVersionsOutput{
		Name:                aws.ToString(out.Name),
		Prefix:              aws.ToString(out.Prefix),
		Delimiter:           aws.ToString(out.Delimiter),
		MaxKeys:             int(aws.ToInt32(out.MaxKeys)),
		IsTruncated:         aws.ToBool(out.IsTruncated),
		KeyMarker:           aws.ToString(out.KeyMarker),
		VersionIDMarker:     aws.ToString(out.VersionIdMarker),
		NextKeyMarker:       aws.ToString(out.NextKeyMarker),
		NextVersionIDMarker: aws.ToString(out.NextVersionIdMarker),
	}
	for _, v := range out.Versions {
		result.Versions = append(result.Versions, toObjectVersion(v))
	}
	for _, m := range out.DeleteMarkers {
		result.DeleteMarkers = append(result.DeleteMarkers, toDeleteMarker(m))
	}
	for _, p := range out.CommonPrefixes {
		result.CommonPrefixes = append(result.CommonPrefixes, aws.ToString(p.Prefix))
	}
	return result, nil
}

// ---- 分页器 ----

type awsListObjectsV2Paginator struct {
	client  *AWS
	bucket  string
	opts    *s3iface.ListObjectsV2Options
	token   string
	hasMore bool
}

func (a *AWS) NewListObjectsV2Paginator(bucket string, opts *s3iface.ListObjectsV2Options) s3iface.ListObjectsV2Paginator {
	return &awsListObjectsV2Paginator{
		client:  a,
		bucket:  bucket,
		opts:    opts,
		hasMore: true,
	}
}

func (p *awsListObjectsV2Paginator) HasMorePages() bool { return p.hasMore }

func (p *awsListObjectsV2Paginator) NextPage(ctx context.Context) (*s3iface.ListObjectsV2Output, error) {
	if !p.hasMore {
		return nil, nil
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
	if out.IsTruncated && out.NextContinuationToken != "" {
		p.token = out.NextContinuationToken
		p.hasMore = true
	} else {
		p.hasMore = false
	}
	return out, nil
}

type awsListObjectVersionsPaginator struct {
	client    *AWS
	bucket    string
	opts      *s3iface.ListObjectVersionsOptions
	keyMarker string
	verMarker string
	hasMore   bool
}

func (a *AWS) NewListObjectVersionsPaginator(bucket string, opts *s3iface.ListObjectVersionsOptions) s3iface.ListObjectVersionsPaginator {
	return &awsListObjectVersionsPaginator{
		client:  a,
		bucket:  bucket,
		opts:    opts,
		hasMore: true,
	}
}

func (p *awsListObjectVersionsPaginator) HasMorePages() bool { return p.hasMore }

func (p *awsListObjectVersionsPaginator) NextPage(ctx context.Context) (*s3iface.ListObjectVersionsOutput, error) {
	if !p.hasMore {
		return nil, nil
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
