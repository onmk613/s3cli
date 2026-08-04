package cmd

import (
	"fmt"
	"s3cli/internal/action"
	"s3cli/internal/s3path"

	"github.com/spf13/cobra"
)

// LifecycleCmd 管理桶的生命周期规则 (子命令与 mc ilm rule 对齐:
// add/edit/list/remove/export/import; 兼容旧名 set/get/del).
func LifecycleCmd() *cobra.Command {
	lifecycleCmd := &cobra.Command{
		Use:   "lifecycle",
		Short: "Manage lifecycle rules (mc ilm compatible)",
	}
	lifecycleCmd.AddCommand(
		LifecycleAddCmd(),
		LifecycleEditCmd(),
		LifecycleListCmd(),
		LifecycleRemoveCmd(),
		LifecycleExportCmd(),
		LifecycleImportCmd(),
	)
	return lifecycleCmd
}

// LifecycleAddCmd 添加生命周期规则 (mc ilm rule add), 全部参数直接生成规则, 不依赖文件.
func LifecycleAddCmd() *cobra.Command {
	var opt action.LifecycleRuleOptions
	var ttl string
	cmd := &cobra.Command{
		Use:               "add [alias:bucket] ...",
		Aliases:           []string{"set"},
		Short:             "Add a lifecycle rule (mc ilm rule add compatible)",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
	}
	materialize := addLifecycleRuleFlags(cmd, &opt)
	cmd.RunE = NewRunE(func(S3 action.Action, dst *s3path.Path) error {
		if err := materialize(); err != nil {
			return err
		}
		if ttl != "" {
			if opt.ExpiryDays != nil {
				return fmt.Errorf("--ttl conflicts with --expire-days: use only one")
			}
			days, err := action.ParseTTLDays(ttl)
			if err != nil {
				return err
			}
			opt.ExpiryDays = &days
		}
		return S3.AddLifecycleRule(opt, dst.Bucket)
	})
	cmd.Flags().StringVar(&ttl, "ttl", "", "Expiration TTL, e.g. 30d / 12h / 1w / 2m (bare number = days, alias of --expire-days)")
	_ = cmd.Flags().MarkHidden("ttl")
	cmd.ValidArgsFunction = AutoCompleteBucket
	return cmd
}

// LifecycleEditCmd 按 ID 修改生命周期规则 (mc ilm rule edit).
func LifecycleEditCmd() *cobra.Command {
	var opt action.LifecycleRuleOptions
	var enable, disable bool
	cmd := &cobra.Command{
		Use:               "edit [alias:bucket] ...",
		Short:             "Modify a lifecycle rule by --id (mc ilm rule edit compatible)",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
	}
	materialize := addLifecycleRuleFlags(cmd, &opt)
	cmd.RunE = NewRunE(func(S3 action.Action, dst *s3path.Path) error {
		if err := materialize(); err != nil {
			return err
		}
		if enable && disable {
			return fmt.Errorf("--enable and --disable are mutually exclusive")
		}
		if enable {
			v := true
			opt.Status = &v
		}
		if disable {
			v := false
			opt.Status = &v
		}
		return S3.EditLifecycleRule(opt, dst.Bucket)
	})
	cmd.Flags().BoolVar(&enable, "enable", false, "Enable the rule")
	cmd.Flags().BoolVar(&disable, "disable", false, "Disable the rule")
	cmd.ValidArgsFunction = AutoCompleteBucket
	return cmd
}

// LifecycleListCmd 列出生命周期规则 (mc ilm rule list); 默认表格, --json 输出 JSON.
func LifecycleListCmd() *cobra.Command {
	var opt action.ListLifecycleOptions
	cmd := &cobra.Command{
		Use:               "list [alias:bucket] ...",
		Aliases:           []string{"ls"},
		Short:             "List lifecycle rules (mc ilm rule list compatible)",
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

// LifecycleRemoveCmd 删除生命周期规则 (mc ilm rule remove): --id 删单条, --all --force 清空.
func LifecycleRemoveCmd() *cobra.Command {
	var opt action.RemoveLifecycleOptions
	cmd := &cobra.Command{
		Use:               "remove [alias:bucket] ...",
		Aliases:           []string{"rm", "del"},
		Short:             "Remove lifecycle rules by --id or --all --force (mc ilm rule remove compatible)",
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

// LifecycleExportCmd 导出整份生命周期配置 JSON (mc ilm rule export; 兼容旧 get).
func LifecycleExportCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "export [alias:bucket] ...",
		Aliases:           []string{"get"},
		Short:             "Export lifecycle configuration as JSON (mc ilm rule export compatible)",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.ExportLifecycle(dst.Bucket)
		}),
	}
}

// LifecycleImportCmd 从 JSON/XML 文件 (或 stdin) 整体导入生命周期配置 (mc ilm rule import).
func LifecycleImportCmd() *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:               "import [alias:bucket] ...",
		Short:             "Import lifecycle configuration from a JSON/XML file or stdin (mc ilm rule import compatible)",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.ImportLifecycle(file, dst.Bucket)
		}),
	}
	cmd.Flags().StringVarP(&file, "from-file", "", "-", "Config file (JSON/XML), or - to read from stdin")
	cmd.ValidArgsFunction = AutoCompleteBucket
	return cmd
}

// addLifecycleRuleFlags 注册 mc ilm rule add/edit 的全部参数, 并返回 materialize 闭包:
// 在 RunE 阶段把显式设置的 flag 值物化为指针字段.
func addLifecycleRuleFlags(cmd *cobra.Command, opt *action.LifecycleRuleOptions) func() error {
	var prefix, tags, sizeLT, sizeGT, expiryDate, transitionTier, noncurrentTransitionTier string
	var expireDays, transitionDays, noncurrentExpireDays, noncurrentExpireNewer, noncurrentTransitionDays int
	var expireDeleteMarker, expireAllObjectVersions bool

	f := cmd.Flags()
	f.StringVar(&opt.ID, "id", "", "ID of the rule (auto-generated if empty)")
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
	f.StringVarP(&opt.ConfigFile, "from-file", "", "", "Load entire lifecycle config from a JSON/XML file (overrides flags)")
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
