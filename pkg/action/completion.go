package action

import (
	"s3cli/pkg/s3api"
)

// CompleteBucket 返回当前 alias 下所有 bucket 名（已按 prefix 过滤，最多 max 个）。
// 供 shell 补全使用，不做任何打印。
func (c *S3Client) CompleteBucket(prefix string) ([]string, error) {
	buckets, err := c.S3.ListBuckets(c.Ctx)
	if err != nil {
		return nil, err
	}
	var result []string
	for _, b := range buckets {
		if hasPrefix(b.Name, prefix) {
			result = append(result, c.S3Path(b.Name, ""))
		}
	}
	return result, nil
}

// CompleteKey 返回指定 bucket 下以 keyPrefix 为前缀的对象 key 和"目录"前缀。
// 使用 Delimiter="/" 让 S3 返回 CommonPrefixes（目录），最多返回 max 个。
// 供 shell 补全使用，不做任何打印。
func (c *S3Client) CompleteKey(bucket, keyPrefix string, max int) ([]string, error) {
	out, err := c.S3.ListObjectsV2(c.Ctx, bucket, &s3api.ListObjectsV2Options{
		Prefix:    keyPrefix,
		Delimiter: "/",
		MaxKeys:   max,
	})
	if err != nil {
		return nil, err
	}

	var keys []string
	for _, p := range out.CommonPrefixes {
		keys = append(keys, c.S3Path(bucket, p))
	}
	for _, item := range out.Contents {
		keys = append(keys, c.S3Path(bucket, item.Key))
	}
	return keys, nil
}

// hasPrefix 是 strings.HasPrefix 的本地副本，避免引入额外 import。
func hasPrefix(s, prefix string) bool {
	if len(prefix) > len(s) {
		return false
	}
	return s[:len(prefix)] == prefix
}
