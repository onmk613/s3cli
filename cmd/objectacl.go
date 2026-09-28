package cmd

import (
	"s3cli/internal/action"
	"s3cli/internal/config"
	"s3cli/internal/i18n"
	"s3cli/internal/s3path"

	"github.com/spf13/cobra"
)

func init() { Register("object", "Object Operations", NewObjectCmd) }

// NewObjectCmd 对象子资源命令的父命令 (ACL / Object Lock 等)。
// 与 `s3cli bucket <subresource>` 对称, 路径参数为 alias:bucket/key。
func NewObjectCmd() *cobra.Command {
	objectCmd := &cobra.Command{
		Use:   "object",
		Short: i18n.T("Manage object subresources (ACL / Object Lock)", "管理对象子资源（ACL / 对象锁）"),
	}
	objectCmd.AddCommand(
		ObjectACLCmd(),
		ObjectRetentionCmd(),
		ObjectLegalHoldCmd(),
	)
	return objectCmd
}

// ObjectACLCmd 管理对象 ACL (get/set)。
func ObjectACLCmd() *cobra.Command {
	aclCmd := &cobra.Command{
		Use:   "acl",
		Short: i18n.T("Manage object ACL", "管理对象 ACL"),
	}
	aclCmd.AddCommand(ObjectACLGetCmd(), ObjectACLSetCmd())
	return aclCmd
}

// ObjectACLGetCmd 打印对象 ACL (原始 XML, --json 包装为 JSON)。
func ObjectACLGetCmd() *cobra.Command {
	var opt action.ACLGetOptions
	var versionID string
	cmd := &cobra.Command{
		Use:               "get [alias:bucket/key] ...",
		Short:             i18n.T("Print object ACL as XML (--json wraps it as JSON)", "以 XML 打印对象 ACL（--json 包装为 JSON）"),
		ValidArgsFunction: AutoCompletePath,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			opt.JSON = config.G.F.JSON
			return S3.GetObjectACL(opt, dst.Bucket, dst.Key, versionID)
		}),
	}
	f := cmd.Flags()
	f.StringVar(&versionID, "version-id", "", i18n.T("Read the ACL of a specific object version", "查看对象特定版本的 ACL"))
	f.StringVar(&versionID, "vid", "", vidAliasDesc())
	return cmd
}

// ObjectACLSetCmd 设置对象 ACL (canned ACL 与 grant 头)。
func ObjectACLSetCmd() *cobra.Command {
	var opt action.ACLSetOptions
	var versionID string
	cmd := &cobra.Command{
		Use:               "set [alias:bucket/key] ...",
		Short:             i18n.T("Set object ACL via --acl and/or --grant-* (at least one required)", "通过 --acl 和/或 --grant-* 设置对象 ACL（至少一项）"),
		ValidArgsFunction: AutoCompletePath,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.SetObjectACL(opt, dst.Bucket, dst.Key, versionID)
		}),
	}
	f := cmd.Flags()
	f.StringVar(&versionID, "version-id", "", i18n.T("Set the ACL of a specific object version", "设置对象特定版本的 ACL"))
	f.StringVar(&versionID, "vid", "", vidAliasDesc())
	addACLSetFlags(cmd, &opt)
	return cmd
}
