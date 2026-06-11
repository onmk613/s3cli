package action

import (
	"encoding/json"
	"encoding/xml"
	"fmt"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/utils"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// SetCors 给桶设置 CORS 规则 (XML 或 JSON 自动识别)
func (c *S3Client) SetCors(corsFile string, bucket string) error {
	data, format, err := utils.LoadAWSConfigFile(corsFile)
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

	_, err = c.S3.PutBucketCors(c.Ctx, &s3.PutBucketCorsInput{
		Bucket:            aws.String(bucket),
		CORSConfiguration: cfg,
	})
	if err != nil {
		return fmt.Errorf("set cors %s: %s", bucket, FormatAPIError(err))
	}

	myprint.PrintfBoldGreen("CORS configuration set for %s %s\n", c.Alias, bucket)
	return nil
}

// GetCors 打印桶的 CORS 规则 (JSON)
func (c *S3Client) GetCors(bucket string) error {
	out, err := c.S3.GetBucketCors(c.Ctx, &s3.GetBucketCorsInput{Bucket: aws.String(bucket)})
	if err != nil {
		return fmt.Errorf("get cors %s: %s", bucket, FormatAPIError(err))
	}
	b, err := json.MarshalIndent(map[string]any{"CORSRules": out.CORSRules}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal cors: %w", err)
	}

	myprint.PrintfBoldBlue("# %s %s cors:\n", c.Alias, bucket)
	myprint.PrintlnGreen(string(b))
	return nil
}

// DelCors 删除桶 CORS
func (c *S3Client) DelCors(bucket string) error {
	if _, err := c.S3.DeleteBucketCors(c.Ctx, &s3.DeleteBucketCorsInput{Bucket: aws.String(bucket)}); err != nil {
		return fmt.Errorf("delete cors %s: %s", bucket, FormatAPIError(err))
	}

	myprint.PrintfBoldGreen("CORS configuration deleted for %s %s\n", c.Alias, bucket)
	return nil
}

// parseCORSConfig 解析 CORS 配置文件，支持 JSON 和 XML 格式。
func parseCORSConfig(data []byte, format string) (*s3types.CORSConfiguration, error) {
	switch format {
	case "json":
		var c s3types.CORSConfiguration
		if err := utils.UnmarshalAWS(data, "json", &c); err != nil {
			return nil, err
		}
		return &c, nil
	case "xml":
		var x xmlCORSConfiguration
		if err := xml.Unmarshal(data, &x); err != nil {
			return nil, fmt.Errorf("parse xml: %w", err)
		}
		return x.toSDK(), nil
	}
	return nil, fmt.Errorf("unknown format %q", format)
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

func (x *xmlCORSConfiguration) toSDK() *s3types.CORSConfiguration {
	out := &s3types.CORSConfiguration{CORSRules: make([]s3types.CORSRule, 0, len(x.Rules))}
	for _, r := range x.Rules {
		rule := s3types.CORSRule{
			AllowedMethods: r.AllowedMethods,
			AllowedOrigins: r.AllowedOrigins,
			AllowedHeaders: r.AllowedHeaders,
			ExposeHeaders:  r.ExposeHeaders,
			MaxAgeSeconds:  r.MaxAgeSeconds,
		}
		if r.ID != "" {
			rule.ID = aws.String(r.ID)
		}
		out.CORSRules = append(out.CORSRules, rule)
	}
	return out
}
