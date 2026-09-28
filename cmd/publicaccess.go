package cmd

import (
	"s3cli/internal/action"
	"s3cli/internal/i18n"
	"s3cli/internal/s3path"

	"github.com/spf13/cobra"
)

// PublicAccessBlockCmd 管理桶的公共访问阻断配置 (get/set/remove), 挂载在 `s3cli bucket` 下。
func PublicAccessBlockCmd() *cobra.Command {
	pabCmd := &cobra.Command{
		Use:     "public-access-block",
		Aliases: []string{"pab"},
		Short:   i18n.T("Manage bucket public access block configuration", "管理存储桶公共访问阻断配置"),
	}
	pabCmd.AddCommand(PublicAccessBlockGetCmd(), PublicAccessBlockSetCmd(), PublicAccessBlockRemoveCmd())
	return pabCmd
}

// PublicAccessBlockGetCmd 打印桶的公共访问阻断配置 (JSON)。
func PublicAccessBlockGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "get [alias:bucket] ...",
		Short:             i18n.T("Print bucket public access block configuration (JSON)", "打印存储桶公共访问阻断配置（JSON）"),
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.GetPublicAccessBlock(dst.Bucket)
		}),
	}
}

// PublicAccessBlockSetCmd 设置桶的公共访问阻断配置.
//
// 四个开关都用 Bool + Flags().Changed 读取: 未给出的项保持 nil (不参与判断),
// 显式给出的项 (含 --flag=false) 才会写入配置。
func PublicAccessBlockSetCmd() *cobra.Command {
	var opt action.PublicAccessBlockOptions
	var blockACLs, ignoreACLs, blockPolicy, restrictBuckets bool

	cmd := &cobra.Command{
		Use:               "set [alias:bucket] ...",
		Short:             i18n.T("Set bucket public access block configuration (at least one flag required)", "设置存储桶公共访问阻断配置（至少一个标志）"),
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
	}
	// RunE 在命令构造完成后赋值: 闭包内需要读取 cmd.Flags().Changed (RunE 在运行时才被调用)。
	cmd.RunE = NewRunE(func(S3 action.Action, dst *s3path.Path) error {
		f := cmd.Flags()
		if f.Changed("block-public-acls") {
			opt.BlockPublicACLs = &blockACLs
		}
		if f.Changed("ignore-public-acls") {
			opt.IgnorePublicACLs = &ignoreACLs
		}
		if f.Changed("block-public-policy") {
			opt.BlockPublicPolicy = &blockPolicy
		}
		if f.Changed("restrict-public-buckets") {
			opt.RestrictPublicBuckets = &restrictBuckets
		}
		return S3.SetPublicAccessBlock(opt, dst.Bucket)
	})

	f := cmd.Flags()
	f.BoolVar(&blockACLs, "block-public-acls", false, i18n.T("Reject PUT Bucket ACL calls that would make the bucket public", "拒绝会把存储桶变为公共的 PUT Bucket ACL 调用"))
	f.BoolVar(&ignoreACLs, "ignore-public-acls", false, i18n.T("Ignore existing public ACLs on the bucket", "忽略存储桶上已有的公共 ACL"))
	f.BoolVar(&blockPolicy, "block-public-policy", false, i18n.T("Reject PUT Bucket Policy calls that would make the bucket public", "拒绝会把存储桶变为公共的 PUT Bucket Policy 调用"))
	f.BoolVar(&restrictBuckets, "restrict-public-buckets", false, i18n.T("Restrict access to buckets that have public policies", "限制对具有公共策略的存储桶的访问"))
	return cmd
}

// PublicAccessBlockRemoveCmd 删除桶的公共访问阻断配置。
func PublicAccessBlockRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "remove [alias:bucket] ...",
		Aliases:           []string{"rm", "del", "delete"},
		Short:             i18n.T("Delete bucket public access block configuration", "删除存储桶公共访问阻断配置"),
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.DelPublicAccessBlock(dst.Bucket)
		}),
	}
}
