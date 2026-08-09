package cmd

import (
	"fmt"
	"s3cli/internal/action"
	"s3cli/internal/s3path"

	"github.com/spf13/cobra"
)

// LifecycleCmd 管理桶的生命周期规则 (list/set/remove 命令, 与其他桶配置命令风格一致).
func LifecycleCmd() *cobra.Command {
	lifecycleCmd := &cobra.Command{
		Use:   "lifecycle",
		Short: "Manage bucket lifecycle rules",
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
		Short:             "List lifecycle rules (--json prints the whole config)",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.ListLifecycle(dst.Bucket, opt)
		}),
	}
	cmd.Flags().BoolVar(&opt.Expiry, "expiry", false, "Display only expiration fields")
	cmd.Flags().BoolVar(&opt.Transition, "transition", false, "Display only transition fields")
	cmd.Flags().BoolVar(&opt.JSON, "json", false, "Output format: text or json (supported commands emit structured results)")
	cmd.ValidArgsFunction = AutoCompleteBucket
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
		Short:             "Create/replace a lifecycle rule (flags), or load whole config (--from-file)",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
	}
	materialize := addLifecycleRuleFlags(cmd, &opt)
	cmd.Flags().BoolVar(&disable, "disable", false, "Set the rule status to Disabled (default: Enabled)")
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
	cmd.ValidArgsFunction = AutoCompleteBucket
	return cmd
}

// LifecycleRemoveCmd 删除生命周期规则: --id 删单条, --all --force 清空整份配置.
func LifecycleRemoveCmd() *cobra.Command {
	var opt action.RemoveLifecycleOptions
	cmd := &cobra.Command{
		Use:               "remove [alias:bucket] ...",
		Aliases:           []string{"rm", "del"},
		Short:             "Remove a lifecycle rule by --id, or all rules with --all --force",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.RemoveLifecycleRules(opt, dst.Bucket)
		}),
	}
	cmd.Flags().StringVar(&opt.ID, "id", "", "ID of the lifecycle rule to remove")
	cmd.Flags().BoolVar(&opt.All, "all", false, "Remove all lifecycle rules of the bucket (requires --force)")
	cmd.Flags().BoolVar(&opt.Force, "force", false, "Force removal (required with --all)")
	cmd.ValidArgsFunction = AutoCompleteBucket
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
	f.StringVar(&opt.ID, "id", "", "ID of the rule (auto-derived from rule content if empty)")
	f.StringVar(&prefix, "prefix", "", "Object prefix")
	f.StringVar(&tags, "tags", "", "Tag filter: '<key1>=<value1>&<key2>=<value2>'")
	f.StringVar(&sizeLT, "size-lt", "", "Select objects smaller than this size, e.g. 1MiB / 500K / 1048576")
	f.StringVar(&sizeGT, "size-gt", "", "Select objects larger than this size, e.g. 1MiB / 500K / 1048576")
	f.IntVar(&expireDays, "expire-days", 0, "Number of days to expire")
	f.StringVar(&expiryDate, "expiry-date", "", "Date of expiration, format 'YYYY-MM-DD'")
	f.BoolVar(&expireDeleteMarker, "expire-delete-marker", false, "Expire zombie delete markers")
	f.BoolVar(&expireAllObjectVersions, "expire-all-object-versions", false, "Expire all object versions")
	f.IntVar(&transitionDays, "transition-days", 0, "Number of days to transition")
	f.StringVar(&transitionTier, "transition-tier", "", "Remote tier name (storage class) to transition to")
	f.IntVar(&noncurrentExpireDays, "noncurrent-expire-days", 0, "Number of days to expire noncurrent versions")
	f.IntVar(&noncurrentExpireNewer, "noncurrent-expire-newer", 0, "Number of newer noncurrent versions to retain")
	f.IntVar(&noncurrentTransitionDays, "noncurrent-transition-days", 0, "Number of days to transition noncurrent versions")
	f.StringVar(&noncurrentTransitionTier, "noncurrent-transition-tier", "", "Remote tier name to transition noncurrent versions to")
	f.StringVar(&opt.ConfigFile, "from-file", "", "Load entire lifecycle config from a JSON/XML file (overrides flags)")
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
