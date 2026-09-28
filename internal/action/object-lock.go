// object-lock.go 实现 Object Lock 相关动作:
//   - 桶级默认保留配置: Get/SetObjectLockConfiguration (`bucket object-lock get|set`)
//   - 对象级保留设置:   Get/SetObjectRetention      (`object retention get|set`)
//   - 对象级法律留存:   Get/SetObjectLegalHold      (`object legal-hold get|set`)
//
// 注意: api.PutObjectRetention / PutObjectLegalHold 的签名不接收 bypass-governance
// 开关, 因此命令行不提供 --bypass-governance (宁可不给, 也不静默忽略)。

package action

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"s3cli/internal/action/render"
	"s3cli/internal/api"
	myprint "s3cli/internal/fmtutil"
	"s3cli/internal/i18n"
)

// ObjectLockConfigOptions 控制 `bucket object-lock set` 的参数.
type ObjectLockConfigOptions struct {
	ConfigFile string // --from-file: AWS CLI 兼容的 JSON 配置
}

// RetentionOptions 控制 `object retention set` 的参数.
type RetentionOptions struct {
	Mode        string // GOVERNANCE / COMPLIANCE
	RetainUntil string // RFC3339, 如 2030-01-02T15:04:05Z
}

// LegalHoldOptions 控制 `object legal-hold set` 的参数.
type LegalHoldOptions struct {
	Status string // ON / OFF
}

// ObjectLockConfig 允许的保留模式 / 法律留存状态.
var (
	retentionModes = map[string]bool{"GOVERNANCE": true, "COMPLIANCE": true}
	legalHoldState = map[string]bool{"ON": true, "OFF": true}
)

// GetObjectLockConfiguration 打印桶的 Object Lock 配置 (JSON)。
func (c *Action) GetObjectLockConfiguration(bucket string) error {
	cfg, err := c.S3.GetObjectLockConfiguration(c.Ctx, bucket)
	if err != nil {
		return objectLockErr("get object-lock configuration", c.S3Path(bucket, ""), err)
	}
	return c.printBucketConfigJSON(bucket, "object-lock", cfg)
}

// SetObjectLockConfiguration 从 JSON 文件设置桶的 Object Lock 默认保留配置.
func (c *Action) SetObjectLockConfiguration(opt ObjectLockConfigOptions, bucket string) error {
	if strings.TrimSpace(opt.ConfigFile) == "" {
		return errors.New(i18n.T("object-lock set: --from-file is required", "设置对象锁：必须指定 --from-file"))
	}
	cfg, err := loadJSONConfig[api.ObjectLockConfiguration](opt.ConfigFile, "object-lock")
	if err != nil {
		return err
	}
	// JSON 解析对未识别键宽容 (见 unmarshalAWS): 键名拼错只会留下空配置,
	// 直接发给服务端只会换回一个难以定位的 MalformedXML。
	if cfg.ObjectLockEnabled == "" && cfg.Rule == nil {
		return fmt.Errorf(i18n.T("no object lock configuration found in %s (expected ObjectLockEnabled / Rule)", "在 %s 中未找到对象锁配置（应为 ObjectLockEnabled / Rule）"), opt.ConfigFile)
	}

	if err := c.S3.PutObjectLockConfiguration(c.Ctx, bucket, cfg); err != nil {
		return objectLockErr("set object-lock configuration", c.S3Path(bucket, ""), err)
	}

	myprint.PrintfBoldGreen(i18n.T("Object Lock configuration set for %s\n", "已为 %s 设置对象锁配置\n"), c.S3Path(bucket, ""))
	return nil
}

// GetObjectRetention 打印对象的保留设置 (JSON)。
func (c *Action) GetObjectRetention(bucket, key, versionID string) error {
	if err := requireObjectKey("retention", bucket, key); err != nil {
		return err
	}
	retention, err := c.S3.GetObjectRetention(c.Ctx, bucket, key, versionID)
	if err != nil {
		return objectLockErr("get retention", c.S3Path(bucket, key), err)
	}
	return c.printObjectConfigJSON(bucket, key, "retention", retention)
}

// SetObjectRetention 设置对象的保留模式与到期时间.
func (c *Action) SetObjectRetention(opt RetentionOptions, bucket, key, versionID string) error {
	if err := requireObjectKey("retention", bucket, key); err != nil {
		return err
	}
	retention, err := buildRetention(opt)
	if err != nil {
		return err
	}
	if err := c.S3.PutObjectRetention(c.Ctx, bucket, key, versionID, retention); err != nil {
		return objectLockErr("set retention", c.S3Path(bucket, key), err)
	}

	myprint.PrintfBoldGreen(i18n.T("Retention set for %s (mode=%s, retain-until=%s)\n", "已为 %s 设置保留策略（模式=%s，到期=%s）\n"),
		c.S3Path(bucket, key), retention.Mode, retention.RetainUntilDate)
	return nil
}

// GetObjectLegalHold 打印对象的法律留存状态 (JSON)。
func (c *Action) GetObjectLegalHold(bucket, key, versionID string) error {
	if err := requireObjectKey("legal-hold", bucket, key); err != nil {
		return err
	}
	hold, err := c.S3.GetObjectLegalHold(c.Ctx, bucket, key, versionID)
	if err != nil {
		return objectLockErr("get legal-hold", c.S3Path(bucket, key), err)
	}
	return c.printObjectConfigJSON(bucket, key, "legal-hold", hold)
}

// SetObjectLegalHold 设置对象的法律留存状态 (ON / OFF)。
func (c *Action) SetObjectLegalHold(opt LegalHoldOptions, bucket, key, versionID string) error {
	if err := requireObjectKey("legal-hold", bucket, key); err != nil {
		return err
	}
	status := strings.ToUpper(strings.TrimSpace(opt.Status))
	if status == "" {
		return errors.New(i18n.T("legal-hold set: --status is required (ON / OFF)", "设置法律留存：必须指定 --status（ON / OFF）"))
	}
	if !legalHoldState[status] {
		return fmt.Errorf(i18n.T("legal-hold set: invalid status %q (expected ON or OFF)", "设置法律留存：状态 %q 无效（应为 ON 或 OFF）"), opt.Status)
	}

	hold := &api.ObjectLockLegalHold{Status: status}
	if err := c.S3.PutObjectLegalHold(c.Ctx, bucket, key, versionID, hold); err != nil {
		return objectLockErr("set legal-hold", c.S3Path(bucket, key), err)
	}

	myprint.PrintfBoldGreen(i18n.T("Legal hold set for %s (status=%s)\n", "已为 %s 设置法律留存（状态=%s）\n"), c.S3Path(bucket, key), status)
	return nil
}

// buildRetention 校验并构造对象保留设置.
func buildRetention(opt RetentionOptions) (*api.ObjectLockRetention, error) {
	mode := strings.ToUpper(strings.TrimSpace(opt.Mode))
	if mode == "" {
		return nil, errors.New(i18n.T("retention set: --mode is required (GOVERNANCE / COMPLIANCE)", "设置保留策略：必须指定 --mode（GOVERNANCE / COMPLIANCE）"))
	}
	if !retentionModes[mode] {
		return nil, fmt.Errorf(i18n.T("retention set: invalid mode %q (expected GOVERNANCE or COMPLIANCE)", "设置保留策略：模式 %q 无效（应为 GOVERNANCE 或 COMPLIANCE）"), opt.Mode)
	}

	raw := strings.TrimSpace(opt.RetainUntil)
	if raw == "" {
		return nil, errors.New(i18n.T("retention set: --retain-until is required (RFC3339, e.g. 2030-01-02T15:04:05Z)", "设置保留策略：必须指定 --retain-until（RFC3339，如 2030-01-02T15:04:05Z）"))
	}
	until, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, fmt.Errorf(i18n.T("retention set: --retain-until %q is not a valid RFC3339 timestamp: %w", "设置保留策略：--retain-until %q 不是合法的 RFC3339 时间：%w"), raw, err)
	}

	// 统一按 UTC 输出, 保证不同服务端/时区写法都能被接受。
	return &api.ObjectLockRetention{Mode: mode, RetainUntilDate: until.UTC().Format(time.RFC3339)}, nil
}

// requireObjectKey 拒绝把裸桶当成对象路径使用 (对象级子资源必须带 key)。
func requireObjectKey(op, bucket, key string) error {
	if key == "" {
		return fmt.Errorf(i18n.T("%s requires an object key in bucket %s, not a bare bucket", "%s 需要存储桶 %s 中的对象 key，而非裸存储桶"), op, bucket)
	}
	return nil
}

// printObjectConfigJSON 打印对象级子资源 (retention / legal-hold) 的 JSON 文档,
// 与 printBucketConfigJSON 同风格, 标题改用对象路径。
func (c *Action) printObjectConfigJSON(bucket, key, header string, cfg any) error {
	myprint.PrintfBoldBlue("# %s %s\n", c.S3Path(bucket, key), header)
	return render.PrintJSONDoc(cfg)
}

// objectLockErr 为 Object Lock 操作补充最常见的失败原因提示:
// 404 通常是桶未启用 Object Lock (或对象无该设置), 403 通常是缺少相应权限。
// 分类一律经由 api 包的谓词, 不在此处自行匹配错误码字符串。
func objectLockErr(op, path string, err error) error {
	switch {
	case api.IsNotFound(err):
		return fmt.Errorf(i18n.T("%s %s: %w (the bucket may not have Object Lock enabled, or the object has no such setting)", "%s %s：%w（存储桶可能未启用对象锁，或对象没有该设置）"), op, path, err)
	case api.IsAccessDenied(err):
		return fmt.Errorf(i18n.T("%s %s: %w (missing permission, e.g. s3:PutObjectRetention / s3:PutObjectLegalHold)", "%s %s：%w（缺少权限，如 s3:PutObjectRetention / s3:PutObjectLegalHold）"), op, path, err)
	}
	return fmt.Errorf("%s %s: %w", op, path, err)
}
