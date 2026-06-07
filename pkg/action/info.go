package action

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"strings"

	myprint "s3cli/pkg/fmtutil"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// InfoOptions info 命令参数
type infoOptions struct {
	JSON bool
}

// Info 打印桶或对象的元信息
func (c *S3Client) Info(bucket, prefix string, outputJSON bool) error {
	opt := infoOptions{JSON: outputJSON}
	if prefix == "" {
		return c.infoBucket(opt, bucket)
	}

	ok, err := c.IsS3File(bucket, prefix)
	if err != nil {
		return fmt.Errorf("check s3 path: %s", FormatAPIError(err))
	}
	if !ok {
		return fmt.Errorf("%s: not a file", c.S3Path(bucket, prefix))
	}

	return c.infoObject(opt, bucket, prefix)
}

// ─── 对象信息 ───────────────────────────────────────────────────────────────────

func (c *S3Client) infoObject(opt infoOptions, bucket, key string) error {
	head, err := c.S3.HeadObject(c.Ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket), Key: aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("head object: %s", FormatAPIError(err))
	}

	// ACL
	var aclOwner, aclGrants any
	if a, err := c.S3.GetObjectAcl(c.Ctx, &s3.GetObjectAclInput{
		Bucket: aws.String(bucket), Key: aws.String(key),
	}); err == nil {
		aclOwner = a.Owner
		aclGrants = a.Grants
	} else {
		myprint.Warnf("Cannot read ACL for %s: %s", c.S3Path(bucket, key), FormatAPIError(err))
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
		myprint.Warnf("Cannot read tags for %s: %s", c.S3Path(bucket, key), FormatAPIError(err))
	}

	// JSON 输出
	if opt.JSON {
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
		myprint.Println(string(b))
		return nil
	}

	// 文本输出
	myprint.Printf("%s (object):\n", c.S3Path(bucket, key))
	myprint.Printf("    Content-Length:       %d (", aws.ToInt64(head.ContentLength))
	myprint.PrintfCyan("%s", FormatBytes(aws.ToInt64(head.ContentLength)))
	myprint.Printf(")\n")
	myprint.Printf("    Content-Type:        %s\n", aws.ToString(head.ContentType))
	if head.ContentEncoding != nil {
		myprint.Printf("    Content-Encoding:    %s\n", aws.ToString(head.ContentEncoding))
	}
	if head.ContentDisposition != nil {
		myprint.Printf("    Content-Disposition: %s\n", aws.ToString(head.ContentDisposition))
	}
	if head.CacheControl != nil {
		myprint.Printf("    Cache-Control:       %s\n", aws.ToString(head.CacheControl))
	}
	myprint.Printf("    ETag:                %s\n", aws.ToString(head.ETag))
	if head.LastModified != nil {
		myprint.Printf("    Last-Modified:       %s\n", head.LastModified.Format("2006-01-02 15:04:05"))
	}
	if head.StorageClass != "" {
		myprint.Printf("    Storage-Class:       %s\n", string(head.StorageClass))
	}
	if head.ServerSideEncryption != "" {
		myprint.Printf("    SSE:                 %s\n", string(head.ServerSideEncryption))
	}
	if head.SSEKMSKeyId != nil {
		myprint.Printf("    SSE-KMS-Key-Id:      %s\n", aws.ToString(head.SSEKMSKeyId))
	}
	if head.VersionId != nil {
		myprint.Printf("    Version-Id:          %s\n", aws.ToString(head.VersionId))
	}
	if pc := aws.ToInt32(head.PartsCount); pc > 0 {
		myprint.Printf("    Parts-Count:         %d\n", pc)
	}
	if head.ReplicationStatus != "" {
		myprint.Printf("    Replication:         %s\n", string(head.ReplicationStatus))
	}
	if head.ObjectLockMode != "" {
		myprint.Printf("    ObjectLock-Mode:     %s\n", string(head.ObjectLockMode))
	}
	if head.ObjectLockRetainUntilDate != nil {
		myprint.Printf("    ObjectLock-Until:    %s\n",
			head.ObjectLockRetainUntilDate.Format("2006-01-02 15:04:05"))
	}
	if len(head.Metadata) > 0 {
		myprint.Println("    Metadata:")
		for k, v := range head.Metadata {
			myprint.Printf("        x-amz-meta-%s: %s\n", k, v)
		}
	}
	if len(tags) > 0 {
		myprint.Println("    Tags:")
		for k, v := range tags {
			myprint.Printf("        %s = %s\n", k, v)
		}
	}
	if aclGrants != nil {
		myprint.Println("    ACL:")
		b, _ := json.MarshalIndent(map[string]any{
			"Owner": aclOwner, "Grants": aclGrants,
		}, "        ", "  ")
		myprint.Printf("        %s\n", string(b))
	}

	return nil
}

// ─── 桶信息 ─────────────────────────────────────────────────────────────────────

func (c *S3Client) infoBucket(opt infoOptions, bucket string) error {
	info := map[string]any{"Bucket": bucket}

	// Location
	loc := "us-east-1"
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

	// JSON 输出
	if opt.JSON {
		// Policy 解析为 JSON 对象而非字符串
		if policy != "" {
			var policyObj any
			if json.Unmarshal([]byte(policy), &policyObj) == nil {
				info["Policy"] = policyObj
			}
		}
		b, _ := json.MarshalIndent(info, "", "  ")
		myprint.Println(string(b))
		return nil
	}

	// 文本输出
	myprint.Printf("%s (bucket):\n", c.S3Path(bucket, ""))
	myprint.Printf("    Location:        %s\n", loc)
	if versioning != "" {
		myprint.Printf("    Versioning:      %s\n", versioning)
	}
	if policy != "" {
		myprint.Printf("    Policy:\n%s\n", indent(policy, "        "))
	}
	if len(corsRules) > 0 {
		corsConfig := corsConfiguration{
			XMLName:   xml.Name{Local: "CORSConfiguration"},
			CORSRules: corsRules,
		}
		if b, err := xml.MarshalIndent(corsConfig, "        ", "  "); err == nil {
			myprint.Printf("    CORS:\n        %s\n", string(b))
		}
	}
	if len(aclGrants) > 0 {
		myprint.Println("    ACL Grants:")
		for _, g := range aclGrants {
			myprint.Printf("        %s: %s\n", getGranteeName(g.Grantee), string(g.Permission))
		}
	}
	if url != "" {
		myprint.Printf("    URL:             %s\n", url)
	}

	return nil
}

// ─── 辅助函数 ───────────────────────────────────────────────────────────────────

func getGranteeName(grantee *s3types.Grantee) string {
	if grantee == nil {
		return "Unknown"
	}
	switch {
	case grantee.ID != nil:
		return fmt.Sprintf("CanonicalUser:%s", aws.ToString(grantee.ID))
	case grantee.URI != nil:
		return fmt.Sprintf("Group:%s", aws.ToString(grantee.URI))
	case grantee.DisplayName != nil:
		return fmt.Sprintf("DisplayName:%s", aws.ToString(grantee.DisplayName))
	case grantee.Type != "":
		return fmt.Sprintf("Type:%s", string(grantee.Type))
	}
	return "Unknown"
}

func indent(s, prefix string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}

type corsConfiguration struct {
	XMLName   xml.Name           `xml:"CORSConfiguration"`
	CORSRules []s3types.CORSRule `xml:"CORSRule"`
}
