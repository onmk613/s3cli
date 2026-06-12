package action

import (
	"encoding/json"
	"fmt"

	myprint "s3cli/pkg/fmtutil"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// Info 打印桶或对象的元信息
func (c *S3Client) Info(bucket, prefix string) error {
	if prefix == "" {
		return c.infoBucket(bucket)
	}

	ok, err := c.IsS3File(bucket, prefix)
	if err != nil {
		return fmt.Errorf("check s3 path: %s", FormatAPIError(err))
	}
	if !ok {
		return fmt.Errorf("%s: not a file", c.S3Path(bucket, prefix))
	}

	return c.infoObject(bucket, prefix)
}

func (c *S3Client) infoObject(bucket, key string) error {
	head, err := c.S3.HeadObject(c.Ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket), Key: aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("head object: %s", FormatAPIError(err))
	}
	myprint.PrintfBoldBlue("# %s info(object):\n", c.S3Path(bucket, key))

	// ACL
	var aclOwner, aclGrants any
	if a, err := c.S3.GetObjectAcl(c.Ctx, &s3.GetObjectAclInput{
		Bucket: aws.String(bucket), Key: aws.String(key),
	}); err == nil {
		aclOwner = a.Owner
		aclGrants = a.Grants
	} else {
		myprint.PrintfBoldYellow("Cannot read ACL for %s: %s", c.S3Path(bucket, key), FormatAPIError(err))
	}

	// Tagging
	tags := map[string]string{}
	if t, err := c.S3.GetObjectTagging(c.Ctx, &s3.GetObjectTaggingInput{
		Bucket: aws.String(bucket), Key: aws.String(key),
	}); err == nil {
		for _, kv := range t.TagSet {
			tags[aws.ToString(kv.Key)] = aws.ToString(kv.Value)
		}
	} else {
		myprint.PrintfBoldYellow("Cannot read tags for %s: %s", c.S3Path(bucket, key), FormatAPIError(err))
	}

	m := map[string]any{
		"Bucket":                bucket,
		"Key":                   key,
		"ContentLength":         aws.ToInt64(head.ContentLength),
		"ContentType":           aws.ToString(head.ContentType),
		"ContentEncoding":       aws.ToString(head.ContentEncoding),
		"ContentDisposition":    aws.ToString(head.ContentDisposition),
		"CacheControl":          aws.ToString(head.CacheControl),
		"ETag":                  aws.ToString(head.ETag),
		"LastModified":          head.LastModified,
		"StorageClass":          string(head.StorageClass),
		"VersionId":             aws.ToString(head.VersionId),
		"ServerSideEncryption":  string(head.ServerSideEncryption),
		"SSEKMSKeyId":           aws.ToString(head.SSEKMSKeyId),
		"Metadata":              head.Metadata,
		"PartsCount":            aws.ToInt32(head.PartsCount),
		"ReplicationStatus":     string(head.ReplicationStatus),
		"ObjectLockMode":        string(head.ObjectLockMode),
		"ObjectLockRetainUntil": head.ObjectLockRetainUntilDate,
		"ACLOwner":              aclOwner,
		"ACLGrants":             aclGrants,
		"Tags":                  tags,
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal info: %w", err)
	}

	myprint.PrintlnGreen(string(b))
	return nil
}

func (c *S3Client) infoBucket(bucket string) error {
	info := map[string]any{"Bucket": bucket}

	// Location
	var loc string
	if location, err := c.S3.GetBucketLocation(c.Ctx, &s3.GetBucketLocationInput{
		Bucket: aws.String(bucket),
	}); err == nil {
		if location.LocationConstraint != "" {
			loc = string(location.LocationConstraint)
		}
	}
	info["Location"] = loc

	// Versioning
	var versioning string
	if v, err := c.S3.GetBucketVersioning(c.Ctx, &s3.GetBucketVersioningInput{
		Bucket: aws.String(bucket),
	}); err == nil && v.Status != "" {
		versioning = string(v.Status)
	}
	info["Versioning"] = versioning

	// Policy
	var policy string
	if p, err := c.S3.GetBucketPolicy(c.Ctx, &s3.GetBucketPolicyInput{
		Bucket: aws.String(bucket),
	}); err == nil {
		policy = aws.ToString(p.Policy)
	}
	info["Policy"] = policy

	// CORS
	var corsRules []s3types.CORSRule
	if cors, err := c.S3.GetBucketCors(c.Ctx, &s3.GetBucketCorsInput{
		Bucket: aws.String(bucket),
	}); err == nil {
		corsRules = cors.CORSRules
	}
	info["CORS"] = corsRules

	// ACL
	var aclOwner any
	var aclGrants []s3types.Grant
	if acl, err := c.S3.GetBucketAcl(c.Ctx, &s3.GetBucketAclInput{
		Bucket: aws.String(bucket),
	}); err == nil {
		aclOwner = acl.Owner
		aclGrants = acl.Grants
	}
	info["ACLOwner"] = aclOwner
	info["ACLGrants"] = aclGrants

	// URL
	var url string
	if cred, err := c.GetCreds(); err == nil {
		url = fmt.Sprintf("%s/%s/", cred.BaseEndpoint, bucket)
	}
	info["URL"] = url

	b, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal info: %w", err)
	}

	myprint.PrintfBoldBlue("# %s %s info(bucket):\n", c.Alias, bucket)
	myprint.PrintlnGreen(string(b))
	return nil
}
