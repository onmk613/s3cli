package api

import "encoding/xml"

// DefaultXMLNS 是 S3 配置接口的标准命名空间 (规范为 http://, 不是 https://).
const DefaultXMLNS = "http://s3.amazonaws.com/doc/2006-03-01/"

// Owner 对应 S3 响应中的 Owner 节点.
type Owner struct {
	ID          string
	DisplayName string
}

// Tagging 描述单个键值标签, 同时用于桶标签与对象标签.
type Tagging struct {
	XMLName xml.Name `xml:"Tag"`
	Key     string   `xml:"Key"`
	Value   string   `xml:"Value"`
}

// ACLOptions 控制 ACL 设置 (canned ACL + grant 头).
//
// S3 ACL 的 XML (含 xsi:type 的 Grantee) 较繁琐且易错; 此处 PUT 采用 canned ACL
// 头 (x-amz-acl) 与 grant 头 (x-amz-grant-*), 覆盖绝大多数场景且不依赖 XML;
// GET 返回原始 XML ([]byte) 供调用方自行解析或展示.
type ACLOptions struct {
	ACL              string `json:"ACL,omitempty"` // x-amz-acl: private|public-read|public-read-write|authenticated-read|bucket-owner-read|bucket-owner-full-control|log-delivery-write
	GrantFullControl string `json:"GrantFullControl,omitempty"`
	GrantRead        string `json:"GrantRead,omitempty"`
	GrantReadACP     string `json:"GrantReadACP,omitempty"`
	GrantWrite       string `json:"GrantWrite,omitempty"`
	GrantWriteACP    string `json:"GrantWriteACP,omitempty"`
}
