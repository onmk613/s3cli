// bucket-cors.go 实现桶 CORS 配置管理: Set/Get/DelCors.
// SetCors 支持两种模式:
//   - 参数模式: --id/--origin/--method/--allowed-header/--expose-header/--max-age 直接生成规则, 不依赖文件
//   - 文件模式: -f/--from-file 或位置参数指定 JSON / XML 配置文件

package action

import (
	"bytes"
	"fmt"
	"strings"

	myprint "s3cli/pkg/fmtutil"
	s3 "s3cli/pkg/s3iface"
)

// CorsOptions 控制 SetCors 的参数.
//
// ConfigFile 非空时加载配置文件 (JSON/XML, 可含多条规则);
// 否则按规则字段 (Origin/Method/...) 生成单条 CORS 规则.
type CorsOptions struct {
	ID             string
	Origins        []string // 允许的来源, 如 https://example.com 或 *
	Methods        []string // 允许的方法: GET/PUT/POST/DELETE/HEAD
	AllowedHeaders []string
	ExposeHeaders  []string
	MaxAgeSeconds  int
	ConfigFile     string
}

// SetCors 给桶设置 CORS 规则. 参数模式与文件模式二选一.
func (c *Action) SetCors(opt CorsOptions, bucket string) error {
	if opt.ConfigFile != "" {
		return c.setCorsFromFile(opt.ConfigFile, bucket)
	}
	return c.setCorsFromFlags(opt, bucket)
}

// setCorsFromFile 从本地文件 (JSON/XML) 加载并设置 CORS.
func (c *Action) setCorsFromFile(corsFile, bucket string) error {
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

// setCorsFromFlags 按参数生成单条 CORS 规则并设置 (不依赖文件).
func (c *Action) setCorsFromFlags(opt CorsOptions, bucket string) error {
	if len(opt.Origins) == 0 {
		return fmt.Errorf("cors set: at least one --origin is required (or use -f/--from-file)")
	}
	if len(opt.Methods) == 0 {
		return fmt.Errorf("cors set: at least one --method is required (GET/PUT/POST/DELETE/HEAD)")
	}

	rule := s3.CorsRule{
		ID:            opt.ID,
		AllowedOrigin: append([]string(nil), opt.Origins...),
		MaxAgeSeconds: opt.MaxAgeSeconds,
	}
	for _, m := range opt.Methods {
		rule.AllowedMethod = append(rule.AllowedMethod, strings.ToUpper(strings.TrimSpace(m)))
	}
	rule.AllowedHeader = append(rule.AllowedHeader, opt.AllowedHeaders...)
	rule.ExposeHeader = append(rule.ExposeHeader, opt.ExposeHeaders...)

	cfg := &s3.CorsConfig{CORSRules: []s3.CorsRule{rule}}
	if err := c.S3.SetBucketCors(c.Ctx, bucket, cfg); err != nil {
		return fmt.Errorf("set cors %s: %s", bucket, FormatAPIError(err))
	}

	scope := strings.Join(rule.AllowedOrigin, ", ")
	myprint.PrintfBoldGreen("CORS configuration set for %s %s (%s)\n", c.Alias, bucket, scope)
	return nil
}

// GetCors 打印桶的 CORS 规则 (JSON)
func (c *Action) GetCors(bucket string) error {
	cfg, err := c.S3.GetBucketCors(c.Ctx, bucket)
	if err != nil {
		return fmt.Errorf("get cors %s: %s", bucket, FormatAPIError(err))
	}
	return c.printBucketConfigJSON(bucket, "cors:", map[string]any{"CORSRules": cfg.CORSRules})
}

// DelCors 删除桶 CORS
func (c *Action) DelCors(bucket string) error {
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
