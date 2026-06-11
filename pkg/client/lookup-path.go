package client

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	smithyendpoints "github.com/aws/smithy-go/endpoints"
)

// customBucketResolver 使用一个含 %(bucket) 占位符的模板,
// 把 bucket 名替换进去, 得到最终的 endpoint URL.
//
// 例如:
//
//		Template = "https://www.%(bucket).example.com"
//	    BucketPlaceholder = "%(bucket)"
//		bucket   = "mydata"
//		->         https://www.mydata.example.com
type customBucketResolver struct {
	Template          string
	BucketPlaceholder string
}

var _ s3.EndpointResolverV2 = (*customBucketResolver)(nil)

func (r *customBucketResolver) ResolveEndpoint(ctx context.Context, params s3.EndpointParameters) (smithyendpoints.Endpoint, error) {
	if r.Template == "" {
		return smithyendpoints.Endpoint{}, fmt.Errorf("custom endpoint template is empty")
	}
	if params.Bucket == nil || *params.Bucket == "" {
		return smithyendpoints.Endpoint{}, fmt.Errorf("bucket is required for custom addressing")
	}
	bucket := *params.Bucket

	raw := strings.ReplaceAll(r.Template, r.BucketPlaceholder, bucket)

	u, err := url.Parse(raw)
	if err != nil {
		return smithyendpoints.Endpoint{}, fmt.Errorf("parse custom endpoint %q: %w", raw, err)
	}

	if u.Host == "" {
		return smithyendpoints.Endpoint{}, fmt.Errorf("custom endpoint %q has empty host", raw)
	}

	return smithyendpoints.Endpoint{URI: *u}, nil
}
