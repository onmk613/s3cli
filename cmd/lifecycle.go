package cmd

import (
	"fmt"
	"s3cli/internal/action"
	"s3cli/internal/s3path"
	"s3cli/pkg/i18n"

	"github.com/spf13/cobra"
)

// LifecycleCmd 管理桶的生命周期规则 (list/set/remove 命令, 与其他桶配置命令风格一致).
func LifecycleCmd() *cobra.Command {
	lifecycleCmd := &cobra.Command{
		Use:   "lifecycle",
		Short: i18n.T("Manage bucket lifecycle rules", "管理存储桶生命周期规则"),
	}
	lifecycleCmd.AddCommand(
		LifecycleListCmd(),
		LifecycleSetCmd(),
		LifecycleRemoveCmd(),
	)
	return lifecycleCmd
}

// LifecycleListCmd 列出生命周期规则; 默认表格, --json 输出整份配置 JSON.
func LifecycleListCmd() *cobra.Command {
	var opt action.ListLifecycleOptions
	cmd := &cobra.Command{
		Use:               "list [alias:bucket] ...",
		Aliases:           []string{"ls", "get"},
		Short:             i18n.T("List lifecycle rules (--json prints the whole config)", "列出生命周期规则（--json 打印整份配置）"),
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.ListLifecycle(dst.Bucket, opt)
		}),
	}
	cmd.Flags().BoolVar(&opt.Expiry, "expiry", false, i18n.T("Display only expiration fields", "只显示过期相关字段"))
	cmd.Flags().BoolVar(&opt.Transition, "transition", false, i18n.T("Display only transition fields", "只显示转换相关字段"))
	cmd.Flags().BoolVar(&opt.JSON, "json", false, jsonOutputDesc())
	return cmd
}

// LifecycleSetCmd 创建或整体替换生命周期规则:
//   - 带规则 flag (--id/--prefix/--expire-days/...) 时 upsert 单条规则:
//     同 --id 已存在则整条覆盖, 否则新建; --id 缺省时按规则内容生成确定性 ID (幂等).
//   - 带 --from-file 时从 JSON/XML 文件整体替换整份配置.
func LifecycleSetCmd() *cobra.Command {
	var opt action.LifecycleRuleOptions
	var disable bool
	cmd := &cobra.Command{
		Use:               "set [alias:bucket] ...",
		Short:             i18n.T("Create/replace a lifecycle rule (flags), or load whole config (--from-file)", "创建/替换生命周期规则（标志），或加载整份配置（--from-file）"),
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
	}
	materialize := addLifecycleRuleFlags(cmd, &opt)
	cmd.Flags().BoolVar(&disable, "disable", false, i18n.T("Set the rule status to Disabled (default: Enabled)", "把规则状态设为 Disabled（默认：Enabled）"))
	cmd.RunE = NewRunE(func(S3 action.Action, dst *s3path.Path) error {
		if err := materialize(); err != nil {
			return err
		}
		if disable {
			opt.Status = new(false)
		}
		if opt.ConfigFile != "" && (disable || lifecycleRuleFieldsSet(opt)) {
			return fmt.Errorf("--from-file cannot be combined with rule flags or --disable")
		}
		return S3.SetLifecycleRule(opt, dst.Bucket)
	})
	return cmd
}

// LifecycleRemoveCmd 删除生命周期规则: --id 删单条, --all --force 清空整份配置.
func LifecycleRemoveCmd() *cobra.Command {
	var opt action.RemoveLifecycleOptions
	cmd := &cobra.Command{
		Use:               "remove [alias:bucket] ...",
		Aliases:           []string{"rm", "del"},
		Short:             i18n.T("Remove a lifecycle rule by --id, or all rules with --all --force", "按 --id 删除单条生命周期规则，或用 --all --force 删除全部规则"),
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.RemoveLifecycleRules(opt, dst.Bucket)
		}),
	}
	cmd.Flags().StringVar(&opt.ID, "id", "", i18n.T("ID of the lifecycle rule to remove", "要删除的生命周期规则 ID"))
	cmd.Flags().BoolVar(&opt.All, "all", false, i18n.T("Remove all lifecycle rules of the bucket (requires --force)", "删除存储桶的所有生命周期规则（需配合 --force）"))
	cmd.Flags().BoolVar(&opt.Force, "force", false, i18n.T("Force removal (required with --all)", "强制删除（配合 --all 使用）"))
	return cmd
}

// lifecycleRuleFieldsSet 判断是否给出了除 --from-file 以外的规则构造 flag.
func lifecycleRuleFieldsSet(opt action.LifecycleRuleOptions) bool {
	if opt.ID != "" || opt.Status != nil {
		return true
	}
	if opt.Prefix != nil || opt.Tags != nil || opt.SizeLT != nil || opt.SizeGT != nil {
		return true
	}
	if opt.ExpiryDate != nil || opt.ExpiryDays != nil || opt.ExpireDeleteMarker != nil || opt.ExpireAllObjectVersions != nil {
		return true
	}
	if opt.TransitionDays != nil || opt.TransitionTier != nil {
		return true
	}
	if opt.NoncurrentExpireDays != nil || opt.NoncurrentExpireNewer != nil ||
		opt.NoncurrentTransitionDays != nil || opt.NoncurrentTransitionTier != nil {
		return true
	}
	return false
}

// addLifecycleRuleFlags 注册单条规则的构造参数, 并返回 materialize 闭包:
// 在 RunE 阶段把显式设置的 flag 值物化为指针字段.
func addLifecycleRuleFlags(cmd *cobra.Command, opt *action.LifecycleRuleOptions) func() error {
	var prefix, tags, sizeLT, sizeGT, expiryDate, transitionTier, noncurrentTransitionTier string
	var expireDays, transitionDays, noncurrentExpireDays, noncurrentExpireNewer, noncurrentTransitionDays int
	var expireDeleteMarker, expireAllObjectVersions bool

	f := cmd.Flags()
	f.StringVar(&opt.ID, "id", "", i18n.T("ID of the rule (auto-derived from rule content if empty)", "规则 ID（为空时按规则内容自动生成）"))
	f.StringVar(&prefix, "prefix", "", i18n.T("Object prefix", "对象前缀"))
	f.StringVar(&tags, "tags", "", i18n.T("Tag filter: '<key1>=<value1>&<key2>=<value2>'", "标签过滤：'<key1>=<value1>&<key2>=<value2>'"))
	f.StringVar(&sizeLT, "size-lt", "", i18n.T("Select objects smaller than this size, e.g. 1MiB / 500K / 1048576", "只作用于小于该大小的对象，如 1MiB / 500K / 1048576"))
	f.StringVar(&sizeGT, "size-gt", "", i18n.T("Select objects larger than this size, e.g. 1MiB / 500K / 1048576", "只作用于大于该大小的对象，如 1MiB / 500K / 1048576"))
	f.IntVar(&expireDays, "expire-days", 0, i18n.T("Number of days to expire", "过期天数"))
	f.StringVar(&expiryDate, "expiry-date", "", i18n.T("Date of expiration, format 'YYYY-MM-DD'", "过期日期，格式 'YYYY-MM-DD'"))
	f.BoolVar(&expireDeleteMarker, "expire-delete-marker", false, i18n.T("Expire zombie delete markers", "清理无主的删除标记"))
	f.BoolVar(&expireAllObjectVersions, "expire-all-object-versions", false, i18n.T("Expire all object versions", "使所有对象版本过期"))
	f.IntVar(&transitionDays, "transition-days", 0, i18n.T("Number of days to transition", "转换天数"))
	f.StringVar(&transitionTier, "transition-tier", "", i18n.T("Remote tier name (storage class) to transition to", "要转换到的远端层级（存储类型）名称"))
	f.IntVar(&noncurrentExpireDays, "noncurrent-expire-days", 0, i18n.T("Number of days to expire noncurrent versions", "非当前版本过期天数"))
	f.IntVar(&noncurrentExpireNewer, "noncurrent-expire-newer", 0, i18n.T("Number of newer noncurrent versions to retain", "保留的较新非当前版本数量"))
	f.IntVar(&noncurrentTransitionDays, "noncurrent-transition-days", 0, i18n.T("Number of days to transition noncurrent versions", "非当前版本转换天数"))
	f.StringVar(&noncurrentTransitionTier, "noncurrent-transition-tier", "", i18n.T("Remote tier name to transition noncurrent versions to", "非当前版本要转换到的远端层级名称"))
	f.StringVar(&opt.ConfigFile, "from-file", "", i18n.T("Load entire lifecycle config from a JSON/XML file (overrides flags)", "从 JSON/XML 文件加载整份生命周期配置（覆盖标志设置）"))
	cmd.MarkFlagsMutuallyExclusive("expire-days", "expiry-date", "expire-delete-marker")

	changed := func(name string) bool { return f.Changed(name) }

	return func() error {
		if changed("prefix") {
			opt.Prefix = &prefix
		}
		if changed("tags") {
			opt.Tags = &tags
		}
		if changed("size-lt") {
			n, err := action.ParseByteSize(sizeLT)
			if err != nil {
				return fmt.Errorf("--size-lt: %w", err)
			}
			opt.SizeLT = &n
		}
		if changed("size-gt") {
			n, err := action.ParseByteSize(sizeGT)
			if err != nil {
				return fmt.Errorf("--size-gt: %w", err)
			}
			opt.SizeGT = &n
		}
		if changed("expire-days") {
			opt.ExpiryDays = &expireDays
		}
		if changed("expiry-date") {
			opt.ExpiryDate = &expiryDate
		}
		if changed("expire-delete-marker") {
			opt.ExpireDeleteMarker = &expireDeleteMarker
		}
		if changed("expire-all-object-versions") {
			opt.ExpireAllObjectVersions = &expireAllObjectVersions
		}
		if changed("transition-days") {
			opt.TransitionDays = &transitionDays
		}
		if changed("transition-tier") {
			opt.TransitionTier = &transitionTier
		}
		if changed("noncurrent-expire-days") {
			opt.NoncurrentExpireDays = &noncurrentExpireDays
		}
		if changed("noncurrent-expire-newer") {
			opt.NoncurrentExpireNewer = &noncurrentExpireNewer
		}
		if changed("noncurrent-transition-days") {
			opt.NoncurrentTransitionDays = &noncurrentTransitionDays
		}
		if changed("noncurrent-transition-tier") {
			opt.NoncurrentTransitionTier = &noncurrentTransitionTier
		}
		return nil
	}
}
