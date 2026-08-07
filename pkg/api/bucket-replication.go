// bucket-replication.go 实现桶级复制 (Replication) 配置管理.
// 类型 ReplicationConfiguration 等定义在中立包 s3iface.

package api

import "context"

// GetBucketReplication 获取 bucket 的复制配置.
func (c *Client) GetBucketReplication(ctx context.Context, bucket string) (*ReplicationConfiguration, error) {
	var result ReplicationConfiguration
	if err := c.getBucketSubresourceXML(ctx, bucket, "replication", &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// PutBucketReplication 设置 bucket 的复制配置.
func (c *Client) PutBucketReplication(ctx context.Context, bucket string, config *ReplicationConfiguration) error {
	body, err := marshalXMLWithHeader(config)
	if err != nil {
		return err
	}
	return c.putBucketSubresource(ctx, bucket, "replication", body)
}

// DeleteBucketReplication 删除 bucket 的复制配置.
func (c *Client) DeleteBucketReplication(ctx context.Context, bucket string) error {
	return c.deleteBucketSubresource(ctx, bucket, "replication")
}
