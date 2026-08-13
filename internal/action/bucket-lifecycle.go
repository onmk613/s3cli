// bucket-lifecycle.go 实现桶生命周期配置管理.
//
// 三个入口, 与其他桶配置命令 (cors/policy/encryption) 风格一致:
//   - SetLifecycleRule    set     --id/--prefix/--expire-days/... upsert 单条规则;
//                                 -f 文件整体替换整份配置
//   - RemoveLifecycleRules remove --id 删单条; --all --force 清空
//   - ListLifecycle       list    表格或 --json 输出整份配置
//
// 兼容保留: SetLifecycle (bucket create --set-lifecycle 使用).

package action

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	myprint "s3cli/pkg/fmtutil"
	s3 "s3cli/pkg/s3iface"
)

// LifecycleOptions 控制 SetLifecycle 的参数 (兼容旧入口).
//
// 二选一: 指定 ConfigFile 用自定义配置文件覆盖, 或用 Prefix + TTL 生成一条
// 过期 (Expiration) 规则. TTL 解析为天数 (向上取整).
type LifecycleOptions struct {
	Prefix     string // 过期规则作用的 key 前缀
	TTL        string // 过期时间, 如 "30d" / "12h" / "1w" / "2m" (裸数字按天)
	ConfigFile string // 自定义配置文件 (JSON/XML); 非空时覆盖 Prefix/TTL (-f)
}

// LifecycleRuleOptions 承载单条生命周期规则的全部可设字段 (set 命令).
// 指针字段表示"未设置"; set 以显式给出的字段为准构造完整规则
// (同 ID 规则已存在时整条覆盖, 未给出的字段/动作不会保留).
type LifecycleRuleOptions struct {
	ID     string // --id 显式指定; 缺省时按规则内容生成确定性 ID
	Status *bool  // true=Enabled, false=Disabled

	Prefix *string // 对象前缀
	Tags   *string // 'k1=v1&k2=v2'
	SizeLT *int64  // 对象大小小于 (字节, 支持 1MiB/1G 等写法)
	SizeGT *int64  // 对象大小大于

	ExpiryDate              *string // 'YYYY-MM-DD' 过期日期
	ExpiryDays              *int    // 过期天数 (--expire-days)
	ExpireDeleteMarker      *bool   // 过期 (zombie) 删除标记
	ExpireAllObjectVersions *bool   // 过期所有对象版本

	TransitionDays *int    // 过渡天数
	TransitionTier *string // 过渡到的存储层级 (storage class)

	NoncurrentExpireDays     *int    // 非当前版本过期天数
	NoncurrentExpireNewer    *int    // 保留的非当前版本数
	NoncurrentTransitionDays *int    // 非当前版本过渡天数
	NoncurrentTransitionTier *string // 非当前版本过渡层级

	ConfigFile string // set: 非空时从文件整体替换整份配置
}

// RemoveLifecycleOptions 控制 RemoveLifecycleRules (lifecycle remove).
type RemoveLifecycleOptions struct {
	ID    string // 删除指定 ID 的规则
	All   bool   // 删除全部规则 (需 Force)
	Force bool   // --all 必须搭配 --force
}

// ListLifecycleOptions 控制 ListLifecycle (lifecycle list).
type ListLifecycleOptions struct {
	Expiry     bool // 仅显示过期字段
	Transition bool // 仅显示过渡字段
	JSON       bool // JSON 输出
}

// SetLifecycle 设置生命周期: ConfigFile 非空时从本地文件 (JSON/XML) 加载,
// 否则按 Prefix + TTL 生成一条过期规则. 两种模式必须给出其一.
func (c *Action) SetLifecycle(opt LifecycleOptions, bucket string) error {
	var cfg *s3.LifecycleConfig
	if opt.ConfigFile != "" {
		loaded, err := loadLifecycleFile(opt.ConfigFile)
		if err != nil {
			return err
		}
		cfg = loaded
	} else {
		if opt.TTL == "" {
			return fmt.Errorf("lifecycle set: either --prefix+--ttl or --from-file is required")
		}
		days, err := ParseTTLDays(opt.TTL)
		if err != nil {
			return err
		}
		cfg = buildTTLLifecycle(opt.Prefix, days)
	}

	if err := validateLifecycleConfig(cfg); err != nil {
		return err
	}
	if err := c.S3.SetBucketLifecycle(c.Ctx, bucket, cfg); err != nil {
		return fmt.Errorf("set lifecycle %s: %s", bucket, FormatAPIError(err))
	}

	myprint.PrintfBoldGreen("Lifecycle set for %s %s (%d rules)\n", c.Alias, bucket, len(cfg.Rules))
	return nil
}

// SetLifecycleRule 创建或整体替换单条生命周期规则 (upsert):
//   - ConfigFile 非空: 从文件 (JSON/XML, "-" 表示 stdin) 整体替换整份配置.
//   - 否则按参数构造完整规则: 桶中已存在同 ID 规则则整条覆盖, 否则追加.
//     --id 缺省时按规则内容生成确定性 ID, 因此重复执行相同命令是幂等的.
func (c *Action) SetLifecycleRule(opt LifecycleRuleOptions, bucket string) error {
	if opt.ConfigFile != "" {
		return c.SetLifecycle(LifecycleOptions{ConfigFile: opt.ConfigFile}, bucket)
	}
	rule, err := buildLifecycleRule(opt)
	if err != nil {
		return err
	}
	cfg, err := c.getLifecycle(bucket)
	if err != nil {
		return err
	}
	for i := range cfg.Rules {
		if cfg.Rules[i].ID == rule.ID {
			cfg.Rules[i] = rule
			if err := c.S3.SetBucketLifecycle(c.Ctx, bucket, cfg); err != nil {
				return fmt.Errorf("set lifecycle rule %s: %s", bucket, FormatAPIError(err))
			}
			myprint.PrintfBoldGreen("Lifecycle rule %s updated on %s %s\n", rule.ID, c.Alias, bucket)
			return nil
		}
	}
	cfg.Rules = append(cfg.Rules, rule)
	if err := c.S3.SetBucketLifecycle(c.Ctx, bucket, cfg); err != nil {
		return fmt.Errorf("set lifecycle rule %s: %s", bucket, FormatAPIError(err))
	}
	myprint.PrintfBoldGreen("Lifecycle rule %s added to %s %s\n", rule.ID, c.Alias, bucket)
	return nil
}

// RemoveLifecycleRules 删除生命周期规则:
// --id 删单条; --all --force 删除全部.
func (c *Action) RemoveLifecycleRules(opt RemoveLifecycleOptions, bucket string) error {
	if opt.All {
		if !opt.Force {
			return fmt.Errorf("lifecycle remove: --all requires --force")
		}
		if err := c.S3.DeleteBucketLifecycle(c.Ctx, bucket); err != nil {
			// 无配置时视为已删除 (幂等)
			var apiErr *s3.ErrorResponse
			if errors.As(err, &apiErr) && (apiErr.Code == "NoSuchLifecycleConfiguration" || apiErr.StatusCode == 404) {
				myprint.PrintfBoldGreen("Lifecycle deleted for %s %s\n", c.Alias, bucket)
				return nil
			}
			return fmt.Errorf("remove lifecycle %s: %s", bucket, FormatAPIError(err))
		}
		myprint.PrintfBoldGreen("Lifecycle deleted for %s %s\n", c.Alias, bucket)
		return nil
	}
	if opt.ID == "" {
		return fmt.Errorf("lifecycle remove: either --id or --all is required")
	}
	cfg, err := c.getLifecycle(bucket)
	if err != nil {
		return err
	}
	out := cfg.Rules[:0]
	found := false
	for _, r := range cfg.Rules {
		if r.ID == opt.ID {
			found = true
			continue
		}
		out = append(out, r)
	}
	if !found {
		return fmt.Errorf("lifecycle rule with ID %q not found on %s/%s", opt.ID, c.Alias, bucket)
	}
	cfg.Rules = out
	if err := c.S3.SetBucketLifecycle(c.Ctx, bucket, cfg); err != nil {
		return fmt.Errorf("remove lifecycle rule %s: %s", bucket, FormatAPIError(err))
	}
	myprint.PrintfBoldGreen("Lifecycle rule %s removed from %s %s\n", opt.ID, c.Alias, bucket)
	return nil
}

// ListLifecycle 列出桶的生命周期规则: 按动作分段输出
// 详细表格 (Expiration / NoncurrentVersionExpiration / Transition /
// NoncurrentVersionTransition / AbortIncompleteMultipartUpload);
// --json 输出 JSON; --expiry/--transition 只显示对应动作段.
func (c *Action) ListLifecycle(bucket string, opt ListLifecycleOptions) error {
	cfg, err := c.getLifecycle(bucket)
	if err != nil {
		return err
	}
	if opt.JSON {
		return c.printBucketConfigJSON(bucket, "lifecycle:", cfg)
	}

	type section struct {
		title  string
		header []string
		rows   [][6]string
	}
	var sections []section

	prefixOf := func(r s3.LifecycleRule) string {
		if r.Filter != nil {
			if r.Filter.Prefix != "" {
				return r.Filter.Prefix
			}
			if r.Filter.And != nil && r.Filter.And.Prefix != "" {
				return r.Filter.And.Prefix
			}
		}
		return "-"
	}
	tagsOf := func(r s3.LifecycleRule) string {
		if r.Filter == nil {
			return "-"
		}
		if r.Filter.Tag != nil {
			return r.Filter.Tag.Key + "=" + r.Filter.Tag.Value
		}
		if r.Filter.And != nil && len(r.Filter.And.Tags) > 0 {
			var tags []string
			for _, t := range r.Filter.And.Tags {
				tags = append(tags, t.Key+"="+t.Value)
			}
			return strings.Join(tags, ",")
		}
		return "-"
	}
	base := func(r s3.LifecycleRule) [2]string { return [2]string{r.ID, r.Status} }

	// 1) 当前版本过期 (Expiration)
	if !opt.Transition {
		var rows [][6]string
		for _, r := range cfg.Rules {
			if r.Expiration == nil {
				continue
			}
			e := r.Expiration
			days := "0"
			switch {
			case e.Days != nil:
				days = strconv.Itoa(*e.Days)
			case e.Date != "":
				days = e.Date
			}
			delMarker := "-"
			if e.ExpiredObjectDeleteMarker != nil {
				delMarker = strconv.FormatBool(*e.ExpiredObjectDeleteMarker)
			}
			allVersions := "-"
			if e.ExpiredObjectAllVersions != nil {
				allVersions = strconv.FormatBool(*e.ExpiredObjectAllVersions)
			}
			// ExpireAllVersions 无独立列; 非空时并入 DeleteMarker 列显示
			if allVersions == "true" {
				delMarker = "all"
			}
			b := base(r)
			rows = append(rows, [6]string{b[0], b[1], prefixOf(r), tagsOf(r), days, delMarker})
		}
		if len(rows) > 0 {
			sections = append(sections, section{
				title:  "Expiration for latest version (Expiration)",
				header: []string{"ID", "Status", "Prefix", "Tags", "Days to Expire", "Expire DeleteMarker"},
				rows:   rows,
			})
		}
	}

	// 2) 非当前版本过期 (NoncurrentVersionExpiration)
	if !opt.Transition {
		var rows [][6]string
		for _, r := range cfg.Rules {
			n := r.NoncurrentVersionExpiration
			if n == nil {
				continue
			}
			days := "0"
			if n.NoncurrentDays != nil {
				days = strconv.Itoa(*n.NoncurrentDays)
			}
			keep := "0"
			if n.NewerNoncurrentVersions != nil {
				keep = strconv.Itoa(*n.NewerNoncurrentVersions)
			}
			b := base(r)
			rows = append(rows, [6]string{b[0], b[1], prefixOf(r), tagsOf(r), days, keep})
		}
		if len(rows) > 0 {
			sections = append(sections, section{
				title:  "Expiration for older versions (NoncurrentVersionExpiration)",
				header: []string{"ID", "Status", "Prefix", "Tags", "Days to Expire", "Keep Versions"},
				rows:   rows,
			})
		}
	}

	// 3) 当前版本过渡 (Transition)
	if !opt.Expiry {
		var rows [][6]string
		for _, r := range cfg.Rules {
			if len(r.Transitions) == 0 {
				continue
			}
			for _, t := range r.Transitions {
				days := "0"
				if t.Days != nil {
					days = strconv.Itoa(*t.Days)
				}
				if t.Date != "" {
					days = t.Date
				}
				b := base(r)
				rows = append(rows, [6]string{b[0], b[1], prefixOf(r), tagsOf(r), days, t.StorageClass})
			}
		}
		if len(rows) > 0 {
			sections = append(sections, section{
				title:  "Transition for latest version (Transition)",
				header: []string{"ID", "Status", "Prefix", "Tags", "Days to Tier", "Tier"},
				rows:   rows,
			})
		}
	}

	// 4) 非当前版本过渡 (NoncurrentVersionTransition)
	if !opt.Expiry {
		var rows [][6]string
		for _, r := range cfg.Rules {
			if len(r.NoncurrentVersionTransitions) == 0 {
				continue
			}
			for _, t := range r.NoncurrentVersionTransitions {
				days := "0"
				if t.NoncurrentDays != nil {
					days = strconv.Itoa(*t.NoncurrentDays)
				}
				b := base(r)
				rows = append(rows, [6]string{b[0], b[1], prefixOf(r), tagsOf(r), days, t.StorageClass})
			}
		}
		if len(rows) > 0 {
			sections = append(sections, section{
				title:  "Transition for older versions (NoncurrentVersionTransition)",
				header: []string{"ID", "Status", "Prefix", "Tags", "Days to Tier", "Tier"},
				rows:   rows,
			})
		}
	}

	// 5) 中止未完成分片上传 (AbortIncompleteMultipartUpload)
	if !opt.Transition && !opt.Expiry {
		var rows [][6]string
		for _, r := range cfg.Rules {
			if r.AbortIncompleteMultipartUpload == nil {
				continue
			}
			days := "0"
			if r.AbortIncompleteMultipartUpload.DaysAfterInitiation != nil {
				days = strconv.Itoa(*r.AbortIncompleteMultipartUpload.DaysAfterInitiation)
			}
			b := base(r)
			rows = append(rows, [6]string{b[0], b[1], prefixOf(r), tagsOf(r), days, "-"})
		}
		if len(rows) > 0 {
			sections = append(sections, section{
				title:  "AbortIncompleteMultipartUpload",
				header: []string{"ID", "Status", "Prefix", "Tags", "Days to Abort", ""},
				rows:   rows,
			})
		}
	}

	if len(cfg.Rules) == 0 {
		myprint.PrintfBoldYellow("%s: no lifecycle rules configured\n", c.S3Path(bucket, ""))
		return nil
	}

	myprint.PrintfBoldBlue("# %s %s lifecycle rules (%d):\n", c.Alias, bucket, len(cfg.Rules))
	for _, sec := range sections {
		printLifecycleSection(sec.title, sec.header, sec.rows)
	}
	return nil
}

// printLifecycleSection 渲染一个生命周期动作分段 (表格形式).
func printLifecycleSection(title string, header []string, rows [][6]string) {
	myprint.PrintfBoldBlue("\n%s\n", title)
	cols := []string{}
	for _, h := range header {
		if h != "" {
			cols = append(cols, h)
		}
	}
	tbl := myprint.NewTable(cols...)
	for _, r := range rows {
		cells := make([]myprint.Cell, 0, len(cols))
		for i := range cols {
			cells = append(cells, myprint.Cell{Text: r[i]})
		}
		tbl.AddRow(cells...)
	}
	tbl.Render()
}

// buildLifecycleRule 按参数构造一条完整规则 (set 语义: 以显式字段为准).
func buildLifecycleRule(opt LifecycleRuleOptions) (s3.LifecycleRule, error) {
	rule := s3.LifecycleRule{
		ID:     opt.ID,
		Status: "Enabled",
		Filter: buildRuleFilter(opt.Prefix, opt.Tags, opt.SizeLT, opt.SizeGT),
	}
	if opt.Status != nil {
		if *opt.Status {
			rule.Status = "Enabled"
		} else {
			rule.Status = "Disabled"
		}
	}

	expiryCount := 0
	if opt.ExpiryDays != nil {
		days := *opt.ExpiryDays
		rule.Expiration = &s3.Expiration{Days: &days}
		expiryCount++
	}
	if opt.ExpiryDate != nil {
		if err := validateLifecycleDate(*opt.ExpiryDate); err != nil {
			return rule, err
		}
		date, _ := normalizeLifecycleDate(*opt.ExpiryDate)
		rule.Expiration = &s3.Expiration{Date: date}
		expiryCount++
	}
	if opt.ExpireDeleteMarker != nil {
		dm := *opt.ExpireDeleteMarker
		rule.Expiration = &s3.Expiration{ExpiredObjectDeleteMarker: &dm}
		expiryCount++
	}
	if expiryCount > 1 {
		return rule, fmt.Errorf("only one of --expire-days/--expiry-date/--expire-delete-marker can be used in a single rule")
	}
	if opt.ExpireAllObjectVersions != nil {
		all := *opt.ExpireAllObjectVersions
		if rule.Expiration == nil {
			rule.Expiration = &s3.Expiration{}
		}
		rule.Expiration.ExpiredObjectAllVersions = &all
	}

	if opt.TransitionDays != nil {
		if opt.TransitionTier == nil {
			return rule, fmt.Errorf("--transition-tier is required when --transition-days is set")
		}
		days := *opt.TransitionDays
		rule.Transitions = []s3.Transition{{Days: &days, StorageClass: strings.ToUpper(*opt.TransitionTier)}}
	} else if opt.TransitionTier != nil {
		return rule, fmt.Errorf("--transition-days is required when --transition-tier is set")
	}

	if opt.NoncurrentExpireDays != nil || opt.NoncurrentExpireNewer != nil {
		n := &s3.NoncurrentVersionExpiration{}
		if opt.NoncurrentExpireDays != nil {
			d := *opt.NoncurrentExpireDays
			n.NoncurrentDays = &d
		}
		if opt.NoncurrentExpireNewer != nil {
			v := *opt.NoncurrentExpireNewer
			n.NewerNoncurrentVersions = &v
		}
		rule.NoncurrentVersionExpiration = n
	}

	if opt.NoncurrentTransitionDays != nil {
		if opt.NoncurrentTransitionTier == nil {
			return rule, fmt.Errorf("--noncurrent-transition-tier is required when --noncurrent-transition-days is set")
		}
		days := *opt.NoncurrentTransitionDays
		rule.NoncurrentVersionTransitions = []s3.NoncurrentVersionTransition{
			{NoncurrentDays: &days, StorageClass: strings.ToUpper(*opt.NoncurrentTransitionTier)},
		}
	} else if opt.NoncurrentTransitionTier != nil {
		return rule, fmt.Errorf("--noncurrent-transition-days is required when --noncurrent-transition-tier is set")
	}

	if !hasLifecycleAction(rule) {
		return rule, fmt.Errorf("at least one of --expire-days/--expiry-date/--expire-delete-marker/--expire-all-object-versions/--transition-days/--noncurrent-expire-days/--noncurrent-transition-days must be specified")
	}
	// ID 在动作字段全部就位后生成, 确保不同内容 (动作/天数/层级) 得到不同 ID.
	if rule.ID == "" {
		rule.ID = genLifecycleRuleID(rule)
	}
	return rule, nil
}

// buildRuleFilter 构造 Filter:
// 单个条件直接落到 Prefix/Tag/ObjectSize*; 多个条件合并到 And.
func buildRuleFilter(prefix, tags *string, sizeLT, sizeGT *int64) *s3.Filter {
	f := &s3.Filter{}
	var tagList []s3.Tag
	predCount := 0
	if tags != nil {
		tagList = parseILMTags(*tags)
		predCount += len(tagList)
	}
	if prefix != nil {
		f.Prefix = *prefix
		predCount++
	}
	if sizeLT != nil {
		f.ObjectSizeLessThan = sizeLT
		predCount++
	}
	if sizeGT != nil {
		f.ObjectSizeGreaterThan = sizeGT
		predCount++
	}
	if predCount == 0 {
		return nil
	}
	if predCount >= 2 {
		f.And = &s3.And{
			Prefix:                f.Prefix,
			Tags:                  tagList,
			ObjectSizeLessThan:    f.ObjectSizeLessThan,
			ObjectSizeGreaterThan: f.ObjectSizeGreaterThan,
		}
		f.Prefix = ""
		f.ObjectSizeLessThan = nil
		f.ObjectSizeGreaterThan = nil
	} else if len(tagList) >= 1 {
		f.Tag = &tagList[0]
	}
	return f
}

// hasLifecycleAction 判断规则是否至少包含一个动作.
func hasLifecycleAction(r s3.LifecycleRule) bool {
	if r.Expiration != nil {
		return true
	}
	if len(r.Transitions) > 0 {
		return true
	}
	if r.NoncurrentVersionExpiration != nil {
		return true
	}
	if len(r.NoncurrentVersionTransitions) > 0 {
		return true
	}
	return false
}

// validateLifecycleConfig 校验整份配置的基本合法性.
func validateLifecycleConfig(cfg *s3.LifecycleConfig) error {
	if len(cfg.Rules) == 0 {
		return fmt.Errorf("no lifecycle rules configured")
	}
	for i, r := range cfg.Rules {
		if r.Status == "" {
			return fmt.Errorf("rule[%d] missing required field 'Status' (Enabled/Disabled)", i)
		}
		if r.ID == "" {
			return fmt.Errorf("rule[%d] missing required field 'ID'", i)
		}
	}
	return nil
}

// validateLifecycleDate 校验 'YYYY-MM-DD' (或完整 ISO 8601) 日期.
func validateLifecycleDate(date string) error {
	if _, err := normalizeLifecycleDate(date); err != nil {
		return fmt.Errorf("invalid date %q: must be YYYY-MM-DD", date)
	}
	return nil
}

// normalizeLifecycleDate 把 'YYYY-MM-DD' 归一化为完整 ISO 8601 ('YYYY-MM-DDT00:00:00Z'),
// 已是 ISO 8601 的输入原样返回.
func normalizeLifecycleDate(date string) (string, error) {
	if t, err := time.Parse("2006-01-02", date); err == nil {
		return t.UTC().Format("2006-01-02T15:04:05Z"), nil
	}
	if t, err := time.Parse(time.RFC3339, date); err == nil {
		return t.UTC().Format("2006-01-02T15:04:05Z"), nil
	}
	return "", fmt.Errorf("invalid date %q: must be YYYY-MM-DD", date)
}

// ----------------------------------------------------------------------------
// 解析辅助
// ----------------------------------------------------------------------------

// genLifecycleRuleID 按规则内容生成确定性 ID: <action>-<scope>-<hash8>.
// 同一命令重复执行产生相同 ID, set 因此幂等 (不会追加重复规则).
func genLifecycleRuleID(rule s3.LifecycleRule) string {
	action := "rule"
	switch {
	case rule.Expiration != nil:
		action = "expire"
	case len(rule.Transitions) > 0:
		action = "transition"
	case rule.NoncurrentVersionExpiration != nil:
		action = "nc-expire"
	case len(rule.NoncurrentVersionTransitions) > 0:
		action = "nc-transition"
	case rule.AbortIncompleteMultipartUpload != nil:
		action = "abort-mpu"
	}
	prefix := ""
	if rule.Filter != nil {
		prefix = rule.Filter.Prefix
		if rule.Filter.And != nil {
			prefix = rule.Filter.And.Prefix
		}
	}
	if prefix == "" {
		prefix = "all"
	}
	prefix = sanitizeIDPart(prefix)
	// 用 JSON 序列化做 hash 输入: 指针字段序列化为值 (稳定), 字段顺序固定.
	b, _ := json.Marshal(rule)
	h := sha256.Sum256(b)
	return fmt.Sprintf("%s-%s-%s", action, prefix, hex.EncodeToString(h[:4]))
}

// sanitizeIDPart 把 ID 片段规整为 [a-zA-Z0-9._-], 截断到 40 字符.
func sanitizeIDPart(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 40 {
		out = out[:40]
	}
	if out == "" {
		out = "all"
	}
	return out
}

// parseILMTags 解析 'k1=v1&k2=v2' 标签串; 无 '=' 的项 Key 生效、Value 为空.
func parseILMTags(s string) []s3.Tag {
	var out []s3.Tag
	for _, part := range strings.Split(s, "&") {
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, "=", 2)
		t := s3.Tag{Key: kv[0]}
		if len(kv) == 2 {
			t.Value = kv[1]
		}
		out = append(out, t)
	}
	return out
}

// ParseByteSize 解析大小字符串, 支持裸数字 (字节) 与带单位写法, 如
// "1048576" / "1MiB" / "1MB" / "10k" / "1.5G". 单位均为 1024 进制.
func ParseByteSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty size")
	}
	i := 0
	for i < len(s) {
		c := s[i]
		if (c >= '0' && c <= '9') || c == '.' || c == '-' {
			i++
			continue
		}
		break
	}
	numStr := s[:i]
	unit := strings.ToLower(strings.TrimSpace(s[i:]))
	n, err := strconv.ParseFloat(numStr, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid size %q: expected a positive number", s)
	}
	var mult float64
	switch unit {
	case "", "b":
		mult = 1
	case "k", "kb", "kib":
		mult = 1 << 10
	case "m", "mb", "mib":
		mult = 1 << 20
	case "g", "gb", "gib":
		mult = 1 << 30
	case "t", "tb", "tib":
		mult = 1 << 40
	case "p", "pb", "pib":
		mult = 1 << 50
	default:
		return 0, fmt.Errorf("unknown size unit %q in %q: use B/K/M/G/T/P (KiB/MiB/GiB...)", unit, s)
	}
	v := int64(math.Round(n * mult))
	if v <= 0 {
		return 0, fmt.Errorf("invalid size %q", s)
	}
	return v, nil
}

// strOrNil 空字符串转为 nil 指针.
func strOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ----------------------------------------------------------------------------
// 文件 / 获取
// ----------------------------------------------------------------------------

// loadAWSConfigArg 读取配置文件或 stdin ("-"), 自动识别 JSON/XML.
func loadAWSConfigArg(arg string) ([]byte, string, error) {
	if arg == "-" {
		data, err := io.ReadAll(io.LimitReader(os.Stdin, 16*1024*1024))
		if err != nil {
			return nil, "", fmt.Errorf("read stdin: %w", err)
		}
		return detectConfigFormat(data)
	}
	data, format, err := loadAWSConfigFile(arg)
	if err != nil {
		return nil, "", err
	}
	return data, format, nil
}

// detectConfigFormat 按首字符识别 JSON/XML 并去掉 BOM.
func detectConfigFormat(data []byte) ([]byte, string, error) {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, "", fmt.Errorf("empty input")
	}
	switch trimmed[0] {
	case '{', '[':
		return data, "json", nil
	case '<':
		return data, "xml", nil
	}
	return data, "", fmt.Errorf("unsupported format: must be JSON or XML")
}

// getLifecycle 读取桶生命周期配置; 桶无配置 (NoSuchLifecycleConfiguration) 时返回空配置.
func (c *Action) getLifecycle(bucket string) (*s3.LifecycleConfig, error) {
	cfg, err := c.S3.GetBucketLifecycle(c.Ctx, bucket)
	if err == nil {
		if cfg == nil {
			cfg = &s3.LifecycleConfig{}
		}
		return cfg, nil
	}
	var apiErr *s3.ErrorResponse
	if errors.As(err, &apiErr) && (apiErr.Code == "NoSuchLifecycleConfiguration" || apiErr.StatusCode == 404) {
		return &s3.LifecycleConfig{}, nil
	}
	return nil, fmt.Errorf("get lifecycle %s: %s", bucket, FormatAPIError(err))
}

// loadLifecycleFile 从本地文件加载生命周期配置 (自动识别 JSON / XML).
func loadLifecycleFile(file string) (*s3.LifecycleConfig, error) {
	data, format, err := loadAWSConfigArg(file)
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
	return &s3.LifecycleConfig{
		Rules: []s3.LifecycleRule{
			{
				ID:     "ttl-expire",
				Status: "Enabled",
				Filter: &s3.Filter{Prefix: prefix},
				Expiration: &s3.Expiration{
					Days: new(days),
				},
			},
		},
	}
}

// parseTTLDays 把 TTL 字符串解析为 S3 Expiration 所需的天数 (向上取整, 最小 1).
// 支持的单位: d/day(s) 天, h/hour(s) 小时, w/week(s) 周, m/mo/month(s) 月(=30天), y/year(s) 年(=365天).
// 裸数字 (如 "30") 按天处理.
func ParseTTLDays(s string) (int, error) {
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
