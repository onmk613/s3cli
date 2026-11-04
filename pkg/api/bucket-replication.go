package api

import (
	"context"
	"encoding/xml"
)

// ReplicationConfiguration 桶级复制配置.
type ReplicationConfiguration struct {
	XMLName xml.Name          `xml:"ReplicationConfiguration" json:"-"`
	Role    string            `xml:"Role" json:"Role"`
	Rules   []ReplicationRule `xml:"Rule" json:"Rules"`
}

// ReplicationRule 单条复制规则.
type ReplicationRule struct {
	XMLName                 xml.Name               `xml:"Rule" json:"-"`
	ID                      string                 `xml:"ID,omitempty" json:"ID,omitempty"`
	Priority                *int                   `xml:"Priority,omitempty" json:"Priority,omitempty"`
	Status                  string                 `xml:"Status" json:"Status"` // Enabled / Disabled
	Prefix                  string                 `xml:"Prefix,omitempty" json:"Prefix,omitempty"`
	Filter                  *ReplicationFilter     `xml:"Filter,omitempty" json:"Filter,omitempty"`
	Destination             ReplicationDestination `xml:"Destination" json:"Destination"`
	DeleteMarkerReplication *bool                  `xml:"DeleteMarkerReplication,omitempty" json:"DeleteMarkerReplication,omitempty"`
}

// ReplicationFilter 复制过滤.
type ReplicationFilter struct {
	XMLName xml.Name `xml:"Filter" json:"-"`
	Prefix  string   `xml:"Prefix,omitempty" json:"Prefix,omitempty"`
	Tag     *Tag     `xml:"Tag,omitempty" json:"Tag,omitempty"`
}

// ReplicationDestination 复制目标.
type ReplicationDestination struct {
	Bucket       string `xml:"Bucket" json:"Bucket"`
	StorageClass string `xml:"StorageClass,omitempty" json:"StorageClass,omitempty"`
	Account      string `xml:"Account,omitempty" json:"Account,omitempty"`
}

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
