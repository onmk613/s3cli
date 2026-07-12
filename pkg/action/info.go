package action

import (
	"encoding/json"
	"fmt"

	myprint "s3cli/pkg/fmtutil"
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
	head, err := c.S3.HeadObject(c.Ctx, bucket, key, "")
	if err != nil {
		return fmt.Errorf("head object: %s", FormatAPIError(err))
	}
	myprint.PrintfBoldBlue("# %s info(object):\n", c.S3Path(bucket, key))

	// Tagging
	tags := map[string]string{}
	if t, err := c.S3.GetObjectTagging(c.Ctx, bucket, key, ""); err == nil {
		for _, kv := range t {
			tags[kv.Key] = kv.Value
		}
	} else {
		myprint.PrintfBoldYellow("Cannot read tags for %s: %s", c.S3Path(bucket, key), FormatAPIError(err))
	}

	m := map[string]any{
		"Bucket":                bucket,
		"Key":                   key,
		"ContentLength":         head.ContentLength,
		"ContentType":           head.ContentType,
		"ContentEncoding":       head.ContentEncoding,
		"ContentDisposition":    head.ContentDisposition,
		"CacheControl":          head.CacheControl,
		"ETag":                  head.ETag,
		"LastModified":          head.LastModified,
		"StorageClass":          head.StorageClass,
		"VersionId":             head.VersionID,
		"ServerSideEncryption":  head.ServerSideEncryption,
		"SSEKMSKeyId":           head.SSEKMSKeyID,
		"Metadata":              head.Metadata,
		"PartsCount":            head.PartsCount,
		"ReplicationStatus":     head.ReplicationStatus,
		"ObjectLockMode":        head.ObjectLockMode,
		"ObjectLockRetainUntil": head.ObjectLockRetainUntilDate,
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
	if location, err := c.S3.GetBucketLocation(c.Ctx, bucket); err == nil {
		loc = location
	}
	info["Location"] = loc

	// Versioning
	var versioning string
	if v, err := c.S3.GetBucketVersioning(c.Ctx, bucket); err == nil {
		versioning = string(v)
	}
	info["Versioning"] = versioning

	// Policy
	var policy string
	if p, err := c.S3.GetBucketPolicy(c.Ctx, bucket); err == nil {
		policy = string(p)
	}
	info["Policy"] = policy

	// CORS
	var corsRules any
	if cors, err := c.S3.GetBucketCors(c.Ctx, bucket); err == nil {
		corsRules = cors.CORSRules
	}
	info["CORS"] = corsRules

	// URL
	var url string
	if cred, err := c.GetS3Credentials(); err == nil {
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
