// bucket-lifecycle.go 实现桶生命周期配置管理: SetLifecycle (统一入口:
// --prefix + --ttl 参数生成规则, 或自定义 JSON/XML 文件) / GetLifecycle / DelLifecycle.

package action

import (
	"bytes"
	"fmt"
	"math"
	"strconv"
	"strings"

	myprint "s3cli/pkg/fmtutil"
	s3 "s3cli/pkg/s3iface"
)

// LifecycleOptions 控制 SetLifecycle 的参数.
//
// 二选一: 指定 ConfigFile 用自定义配置文件覆盖, 或用 Prefix + TTL 生成一条
// 过期 (Expiration) 规则. TTL 解析为天数 (向上取整).
type LifecycleOptions struct {
	Prefix     string // 过期规则作用的 key 前缀
	TTL        string // 过期时间, 如 "30d" / "12h" / "1w" / "2m" (裸数字按天)
	ConfigFile string // 自定义配置文件 (JSON/XML); 非空时覆盖 Prefix/TTL (-f)
}

// SetLifecycle 设置生命周期: ConfigFile 非空时从本地文件 (JSON/XML) 加载,
// 否则按 Prefix + TTL 生成一条过期规则. 两种模式必须给出其一.
func (c *S3Client) SetLifecycle(opt LifecycleOptions, bucket string) error {
	var cfg *s3.LifecycleConfig
	if opt.ConfigFile != "" {
		loaded, err := loadLifecycleFile(opt.ConfigFile)
		if err != nil {
			return err
		}
		cfg = loaded
	} else {
		if opt.TTL == "" {
			return fmt.Errorf("lifecycle set: either --prefix+--ttl or -f/--from-file is required")
		}
		days, err := parseTTLDays(opt.TTL)
		if err != nil {
			return err
		}
		cfg = buildTTLLifecycle(opt.Prefix, days)
	}

	if len(cfg.Rules) == 0 {
		return fmt.Errorf("no lifecycle rules configured")
	}
	for i, r := range cfg.Rules {
		if r.Status == "" {
			return fmt.Errorf("rule[%d] missing required field 'Status' (Enabled/Disabled)", i)
		}
		if r.Filter == nil {
			return fmt.Errorf("rule[%d] must specify 'Filter' or legacy 'Prefix'", i)
		}
	}

	if err := c.S3.SetBucketLifecycle(c.Ctx, bucket, cfg); err != nil {
		return fmt.Errorf("set lifecycle %s: %s", bucket, FormatAPIError(err))
	}

	myprint.PrintfBoldGreen("Lifecycle set for %s %s (%d rules)\n", c.Alias, bucket, len(cfg.Rules))
	return nil
}

// loadLifecycleFile 从本地文件加载生命周期配置 (自动识别 JSON / XML).
func loadLifecycleFile(file string) (*s3.LifecycleConfig, error) {
	data, format, err := loadAWSConfigFile(file)
	if err != nil {
		return nil, err
	}
	cfg, err := parseLifecycleConfig(data, format)
	if err != nil {
		return nil, fmt.Errorf("parse lifecycle file %s: %w", file, err)
	}
	return cfg, nil
}

// buildTTLLifecycle 按前缀 + 过期天数构造一条 Enabled 过期规则.
// XMLNS 留空: SetBucketLifecycle -> LifecycleConfig.ToXML 会自动补齐.
func buildTTLLifecycle(prefix string, days int) *s3.LifecycleConfig {
	d := days
	return &s3.LifecycleConfig{
		Rules: []s3.LifecycleRule{
			{
				ID:     "ttl-expire",
				Status: "Enabled",
				Filter: &s3.Filter{Prefix: prefix},
				Expiration: &s3.Expiration{
					Days: &d,
				},
			},
		},
	}
}

// parseTTLDays 把 TTL 字符串解析为 S3 Expiration 所需的天数 (向上取整, 最小 1).
// 支持的单位: d/day(s) 天, h/hour(s) 小时, w/week(s) 周, m/mo/month(s) 月(=30天), y/year(s) 年(=365天).
// 裸数字 (如 "30") 按天处理.
func parseTTLDays(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("--ttl is empty")
	}

	// 分离数值部分与单位部分
	numStr := s
	unit := ""
	for i, r := range s {
		if (r >= '0' && r <= '9') || r == '.' {
			continue
		}
		numStr = s[:i]
		unit = strings.ToLower(strings.TrimSpace(s[i:]))
		break
	}
	n, err := strconv.ParseFloat(numStr, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid --ttl %q: expected a positive number", s)
	}

	var days float64
	switch unit {
	case "", "d", "day", "days":
		days = n
	case "h", "hour", "hours":
		days = n / 24
	case "w", "week", "weeks":
		days = n * 7
	case "m", "mo", "month", "months":
		days = n * 30
	case "y", "year", "years":
		days = n * 365
	default:
		return 0, fmt.Errorf("invalid --ttl unit %q: use d/h/w/m/y", unit)
	}

	d := int(math.Ceil(days))
	if d < 1 {
		d = 1
	}
	return d, nil
}

// GetLifecycle 打印生命周期.
func (c *S3Client) GetLifecycle(bucket string) error {
	cfg, err := c.S3.GetBucketLifecycle(c.Ctx, bucket)
	if err != nil {
		return fmt.Errorf("get lifecycle %s: %s", bucket, FormatAPIError(err))
	}
	return c.printBucketConfigJSON(bucket, "lifecycle:", cfg)
}

// DelLifecycle 删除生命周期.
func (c *S3Client) DelLifecycle(bucket string) error {
	return c.deleteBucketConfig(bucket, "lifecycle", "Lifecycle deleted for %s %s\n",
		func() error { return c.S3.DeleteBucketLifecycle(c.Ctx, bucket) })
}

// parseLifecycleConfig 解析生命周期配置文件, 支持 JSON 和 XML 格式.
func parseLifecycleConfig(data []byte, format string) (*s3.LifecycleConfig, error) {
	switch format {
	case "json":
		var c s3.LifecycleConfig
		if err := unmarshalAWS(data, "json", &c); err != nil {
			return nil, err
		}
		return &c, nil
	case "xml":
		return s3.ParseBucketLifecycleConfig(bytes.NewReader(data))
	}
	return nil, fmt.Errorf("unknown format %q", format)
}
