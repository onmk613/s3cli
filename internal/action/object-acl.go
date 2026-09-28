// object-acl.go 实现对象级 ACL 的读取与设置: Get/SetObjectACL.
//
// 与桶级 ACL 共用 ACLGetOptions / ACLSetOptions 与 printACL,
// 区别仅在于路径带对象 key, 并可选 --version-id 指定版本。

package action

import (
	"errors"
	"fmt"

	myprint "s3cli/internal/fmtutil"
	"s3cli/internal/i18n"
)

// GetObjectACL 读取并打印对象 ACL (可指定 versionID)。
func (c *Action) GetObjectACL(opt ACLGetOptions, bucket, key, versionID string) error {
	if key == "" {
		return errors.New(i18n.T("object acl requires an object key, not a bare bucket", "对象 ACL 需要指定对象 key，而非裸存储桶"))
	}
	data, err := c.S3.GetObjectACL(c.Ctx, bucket, key, versionID)
	if err != nil {
		return fmt.Errorf("get object acl %s: %w", c.S3Path(bucket, key), err)
	}
	return printACL(opt, c.S3Path(bucket, key), data)
}

// SetObjectACL 设置对象 ACL (canned ACL 与 grant 头至少给出一项)。
func (c *Action) SetObjectACL(opt ACLSetOptions, bucket, key, versionID string) error {
	if key == "" {
		return errors.New(i18n.T("object acl requires an object key, not a bare bucket", "对象 ACL 需要指定对象 key，而非裸存储桶"))
	}
	apiOpt, err := buildACLOptions(opt)
	if err != nil {
		return err
	}
	if err := c.S3.PutObjectACL(c.Ctx, bucket, key, versionID, apiOpt); err != nil {
		return fmt.Errorf("set object acl %s: %w", c.S3Path(bucket, key), err)
	}
	myprint.PrintfBoldGreen(i18n.T("ACL set for %s (%s)\n", "已为 %s 设置 ACL（%s）\n"), c.S3Path(bucket, key), describeACLOptions(apiOpt))
	return nil
}
