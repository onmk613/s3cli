package cmd

import (
	"s3cli/internal/action"
	"s3cli/internal/config"
	"s3cli/internal/i18n"
	"s3cli/internal/s3path"

	"github.com/spf13/cobra"
)

// BucketACLCmd 管理桶 ACL (get/set), 挂载在 `s3cli bucket` 下。
func BucketACLCmd() *cobra.Command {
	aclCmd := &cobra.Command{
		Use:   "acl",
		Short: i18n.T("Manage bucket ACL", "管理存储桶 ACL"),
	}
	aclCmd.AddCommand(BucketACLGetCmd(), BucketACLSetCmd())
	return aclCmd
}

// BucketACLGetCmd 打印桶 ACL (原始 XML, --json 包装为 JSON)。
func BucketACLGetCmd() *cobra.Command {
	var opt action.ACLGetOptions
	cmd := &cobra.Command{
		Use:               "get [alias:bucket] ...",
		Short:             i18n.T("Print bucket ACL as XML (--json wraps it as JSON)", "以 XML 打印存储桶 ACL（--json 包装为 JSON）"),
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			opt.JSON = config.G.F.JSON
			return S3.GetBucketACL(opt, dst.Bucket)
		}),
	}
	return cmd
}

// BucketACLSetCmd 设置桶 ACL (canned ACL 与 grant 头)。
func BucketACLSetCmd() *cobra.Command {
	var opt action.ACLSetOptions
	cmd := &cobra.Command{
		Use:               "set [alias:bucket] ...",
		Short:             i18n.T("Set bucket ACL via --acl and/or --grant-* (at least one required)", "通过 --acl 和/或 --grant-* 设置存储桶 ACL（至少一项）"),
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.SetBucketACL(opt, dst.Bucket)
		}),
	}
	addACLSetFlags(cmd, &opt)
	return cmd
}

// addACLSetFlags 注册 ACL 设置相关 flag (桶/对象共用, 保证两处语义一致)。
func addACLSetFlags(cmd *cobra.Command, opt *action.ACLSetOptions) {
	f := cmd.Flags()
	f.StringVar(&opt.ACL, "acl", "", i18n.T("Canned ACL: private / public-read / public-read-write / authenticated-read", "canned ACL：private / public-read / public-read-write / authenticated-read"))
	f.StringArrayVar(&opt.GrantRead, "grant-read", nil, i18n.T("Grant read permission to a grantee (repeatable): <id> | id=<id> | uri=<uri> | emailAddress=<email>", "授予读取权限（可重复）：<id> | id=<id> | uri=<uri> | emailAddress=<email>"))
	f.StringArrayVar(&opt.GrantWrite, "grant-write", nil, i18n.T("Grant write permission to a grantee (repeatable)", "授予写入权限（可重复）"))
	f.StringArrayVar(&opt.GrantReadACP, "grant-read-acp", nil, i18n.T("Grant read-ACP permission to a grantee (repeatable)", "授予读取 ACL 权限（可重复）"))
	f.StringArrayVar(&opt.GrantWriteACP, "grant-write-acp", nil, i18n.T("Grant write-ACP permission to a grantee (repeatable)", "授予写入 ACL 权限（可重复）"))
	f.StringArrayVar(&opt.GrantFullControl, "grant-full-control", nil, i18n.T("Grant full control to a grantee (repeatable)", "授予完全控制权限（可重复）"))
}
