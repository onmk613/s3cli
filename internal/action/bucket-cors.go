package action

import (
	"bytes"
	"fmt"

	myprint "s3cli/pkg/fmtutil"
	s3 "s3cli/pkg/s3api"
)

// SetCors 给桶设置 CORS 规则 (XML 或 JSON 自动识别)
func (c *S3Client) SetCors(corsFile string, bucket string) error {
	data, format, err := loadAWSConfigFile(corsFile)
	if err != nil {
		return err
	}

	cfg, err := parseCORSConfig(data, format)
	if err != nil {
		return fmt.Errorf("parse cors file %s: %w", corsFile, err)
	}

	if len(cfg.CORSRules) == 0 {
		return fmt.Errorf("no CORS rules found in %s", corsFile)
	}

	if err := c.S3.SetBucketCors(c.Ctx, bucket, cfg); err != nil {
		return fmt.Errorf("set cors %s: %s", bucket, FormatAPIError(err))
	}

	myprint.PrintfBoldGreen("CORS configuration set for %s %s\n", c.Alias, bucket)
	return nil
}

// GetCors 打印桶的 CORS 规则 (JSON)
func (c *S3Client) GetCors(bucket string) error {
	cfg, err := c.S3.GetBucketCors(c.Ctx, bucket)
	if err != nil {
		return fmt.Errorf("get cors %s: %s", bucket, FormatAPIError(err))
	}
	return c.printBucketConfigJSON(bucket, "cors:", map[string]any{"CORSRules": cfg.CORSRules})
}

// DelCors 删除桶 CORS
func (c *S3Client) DelCors(bucket string) error {
	return c.deleteBucketConfig(bucket, "cors", "CORS configuration deleted for %s %s\n",
		func() error { return c.S3.DeleteBucketCors(c.Ctx, bucket) })
}

// parseCORSConfig 解析 CORS 配置文件，支持 JSON 和 XML 格式。
func parseCORSConfig(data []byte, format string) (*s3.CorsConfig, error) {
	switch format {
	case "json":
		var c s3.CorsConfig
		if err := unmarshalAWS(data, "json", &c); err != nil {
			return nil, err
		}
		return &c, nil
	case "xml":
		return s3.ParseBucketCorsConfig(bytes.NewReader(data))
	}
	return nil, fmt.Errorf("unknown format %q", format)
}
