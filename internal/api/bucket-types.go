// bucket-types.go 定义桶子资源配置的 DTO 类型 (CORS / 加密 / 生命周期 / 通知 / 标签 / 版本).
// 这些类型描述 S3 线协议 (XML) 与本地配置文件 (JSON) 的数据结构.
//
// 厂商差异 (不同 S3 兼容实现) 在此处的落点见 quirks.go: 元素命名与命名空间可由
// Quirks 覆盖, 无需改动本文件的类型定义.
//
// 线协议为 XML; JSON tag 用于本地配置文件读写与展示.
// XMLName/XMLNS 仅服务于 XML, 对 JSON 标记为 "-".

package api

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// ----------------------------------------------------------------------------
// CORS
// ----------------------------------------------------------------------------

// CorsConfig 是 bucket 的 CORS 配置容器.
type CorsConfig struct {
	XMLName   xml.Name   `xml:"CORSConfiguration" json:"-"`
	XMLNS     string     `xml:"xmlns,attr,omitempty" json:"-"`
	CORSRules []CorsRule `xml:"CORSRule" json:"CORSRules,omitempty"`
}

// CorsRule 是单条 CORS 规则.
//
// 列表字段在 XML 中是单数元素名 (<AllowedOrigin> 逐个出现), 在 AWS CLI / SDK JSON 中是
// 复数键 (AllowedOrigins), 故两种 tag 名不同; JSON 侧沿用 AWS 命名以便
// `aws s3api get-bucket-cors` 的输出可直接使用。
type CorsRule struct {
	AllowedHeader []string `xml:"AllowedHeader,omitempty" json:"AllowedHeaders,omitempty"`
	AllowedMethod []string `xml:"AllowedMethod,omitempty" json:"AllowedMethods,omitempty"`
	AllowedOrigin []string `xml:"AllowedOrigin,omitempty" json:"AllowedOrigins,omitempty"`
	ExposeHeader  []string `xml:"ExposeHeader,omitempty" json:"ExposeHeaders,omitempty"`
	ID            string   `xml:"ID,omitempty" json:"ID,omitempty"`
	MaxAgeSeconds int      `xml:"MaxAgeSeconds,omitempty" json:"MaxAgeSeconds,omitempty"`
}

// ParseBucketCorsConfig 从 XML 解析 CORS 配置.
func ParseBucketCorsConfig(reader io.Reader) (*CorsConfig, error) {
	var c CorsConfig
	err := xml.NewDecoder(io.LimitReader(reader, 128*1024)).Decode(&c)
	if err != nil {
		return nil, fmt.Errorf("decoding xml: %w", err)
	}
	if c.XMLNS == "" {
		c.XMLNS = DefaultXMLNS
	}
	for i, rule := range c.CORSRules {
		for j, method := range rule.AllowedMethod {
			c.CORSRules[i].AllowedMethod[j] = strings.ToUpper(method)
		}
	}
	return &c, nil
}

// ToXML 将 CORS 配置序列化为 XML.
func (c *CorsConfig) ToXML() ([]byte, error) {
	if c.XMLNS == "" {
		c.XMLNS = DefaultXMLNS
	}
	data, err := xml.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("marshaling xml: %w", err)
	}
	return append([]byte(xml.Header), data...), nil
}

// ----------------------------------------------------------------------------
// 服务端加密
// ----------------------------------------------------------------------------

// ServerSideEncryptionByDefault 描述默认加密算法.
type ServerSideEncryptionByDefault struct {
	XMLName        xml.Name `xml:"ApplyServerSideEncryptionByDefault" json:"-"`
	SSEAlgorithm   string   `xml:"SSEAlgorithm" json:"SSEAlgorithm"`
	KMSMasterKeyID string   `xml:"KMSMasterKeyID,omitempty" json:"KMSMasterKeyID,omitempty"`
}

// ServerSideEncryptionRule 单条加密规则.
type ServerSideEncryptionRule struct {
	XMLName                            xml.Name                      `xml:"Rule" json:"-"`
	ApplyServerSideEncryptionByDefault ServerSideEncryptionByDefault `xml:"ApplyServerSideEncryptionByDefault" json:"ApplyServerSideEncryptionByDefault"`
	BucketKeyEnabled                   *bool                         `xml:"BucketKeyEnabled,omitempty" json:"BucketKeyEnabled,omitempty"`
}

// ServerSideEncryptionConfiguration 加密配置.
type ServerSideEncryptionConfiguration struct {
	XMLName xml.Name                   `xml:"ServerSideEncryptionConfiguration" json:"-"`
	Rules   []ServerSideEncryptionRule `xml:"Rule" json:"Rules"`
}

// ----------------------------------------------------------------------------
// 生命周期
// ----------------------------------------------------------------------------

// LifecycleConfig 是 bucket 生命周期配置.
//
// 注意: 不声明 XMLName 根元素约束 —— 服务端返回的根元素不统一
// (有的返回 <LifecycleConfiguration>, 有的返回
// <BucketLifecycleConfiguration>), 去掉约束即可同时兼容;
// 序列化时由 ToXML 显式包裹为 <LifecycleConfiguration> (PUT 的标准根元素).
type LifecycleConfig struct {
	XMLNS string          `xml:"xmlns,attr,omitempty" json:"-"`
	Rules []LifecycleRule `xml:"Rule" json:"Rules,omitempty"`
}

// LifecycleRule 单条生命周期规则.
type LifecycleRule struct {
	XMLName                        xml.Name                        `xml:"Rule" json:"-"`
	ID                             string                          `xml:"ID,omitempty" json:"ID,omitempty"`
	Status                         string                          `xml:"Status" json:"Status"`
	Filter                         *Filter                         `xml:"Filter,omitempty" json:"Filter,omitempty"`
	Transitions                    []Transition                    `xml:"Transition,omitempty" json:"Transition,omitempty"`
	Expiration                     *Expiration                     `xml:"Expiration,omitempty" json:"Expiration,omitempty"`
	NoncurrentVersionExpiration    *NoncurrentVersionExpiration    `xml:"NoncurrentVersionExpiration,omitempty" json:"NoncurrentVersionExpiration,omitempty"`
	NoncurrentVersionTransitions   []NoncurrentVersionTransition   `xml:"NoncurrentVersionTransition,omitempty" json:"NoncurrentVersionTransitions,omitempty"`
	AbortIncompleteMultipartUpload *AbortIncompleteMultipartUpload `xml:"AbortIncompleteMultipartUpload,omitempty" json:"AbortIncompleteMultipartUpload,omitempty"`
}

// Filter 过滤规则.
type Filter struct {
	XMLName               xml.Name `xml:"Filter" json:"-"`
	Prefix                string   `xml:"Prefix,omitempty" json:"Prefix,omitempty"`
	Tag                   *Tag     `xml:"Tag,omitempty" json:"Tag,omitempty"`
	And                   *And     `xml:"And,omitempty" json:"And,omitempty"`
	ObjectSizeLessThan    *int64   `xml:"ObjectSizeLessThan,omitempty" json:"ObjectSizeLessThan,omitempty"`
	ObjectSizeGreaterThan *int64   `xml:"ObjectSizeGreaterThan,omitempty" json:"ObjectSizeGreaterThan,omitempty"`
}

// Tag 标签过滤.
type Tag struct {
	XMLName xml.Name `xml:"Tag" json:"-"`
	Key     string   `xml:"Key" json:"Key"`
	Value   string   `xml:"Value" json:"Value"`
}

// And 组合过滤条件.
type And struct {
	XMLName               xml.Name `xml:"And" json:"-"`
	Prefix                string   `xml:"Prefix,omitempty" json:"Prefix,omitempty"`
	Tags                  []Tag    `xml:"Tag,omitempty" json:"Tags,omitempty"`
	ObjectSizeLessThan    *int64   `xml:"ObjectSizeLessThan,omitempty" json:"ObjectSizeLessThan,omitempty"`
	ObjectSizeGreaterThan *int64   `xml:"ObjectSizeGreaterThan,omitempty" json:"ObjectSizeGreaterThan,omitempty"`
}

// Transition 过渡规则.
type Transition struct {
	XMLName      xml.Name `xml:"Transition" json:"-"`
	Days         *int     `xml:"Days,omitempty" json:"Days,omitempty"`
	Date         string   `xml:"Date,omitempty" json:"Date,omitempty"`
	StorageClass string   `xml:"StorageClass" json:"StorageClass"`
}

// Expiration 过期规则.
type Expiration struct {
	XMLName                   xml.Name `xml:"Expiration" json:"-"`
	Days                      *int     `xml:"Days,omitempty" json:"Days,omitempty"`
	Date                      string   `xml:"Date,omitempty" json:"Date,omitempty"`
	ExpiredObjectDeleteMarker *bool    `xml:"ExpiredObjectDeleteMarker,omitempty" json:"ExpiredObjectDeleteMarker,omitempty"`
	// ExpiredObjectAllVersions 由 --expire-all-object-versions 生成.
	ExpiredObjectAllVersions *bool `xml:"ExpiredObjectAllVersions,omitempty" json:"ExpiredObjectAllVersions,omitempty"`
}

// NoncurrentVersionExpiration 非当前版本过期.
type NoncurrentVersionExpiration struct {
	XMLName                 xml.Name `xml:"NoncurrentVersionExpiration" json:"-"`
	NoncurrentDays          *int     `xml:"NoncurrentDays,omitempty" json:"NoncurrentDays,omitempty"`
	NewerNoncurrentVersions *int     `xml:"NewerNoncurrentVersions,omitempty" json:"NewerNoncurrentVersions,omitempty"`
}

// NoncurrentVersionTransition 非当前版本过渡.
type NoncurrentVersionTransition struct {
	XMLName                 xml.Name `xml:"NoncurrentVersionTransition" json:"-"`
	NoncurrentDays          *int     `xml:"NoncurrentDays,omitempty" json:"NoncurrentDays,omitempty"`
	NewerNoncurrentVersions *int     `xml:"NewerNoncurrentVersions,omitempty" json:"NewerNoncurrentVersions,omitempty"`
	StorageClass            string   `xml:"StorageClass" json:"StorageClass"`
}

// AbortIncompleteMultipartUpload 中止未完成的分片上传.
type AbortIncompleteMultipartUpload struct {
	XMLName             xml.Name `xml:"AbortIncompleteMultipartUpload" json:"-"`
	DaysAfterInitiation *int     `xml:"DaysAfterInitiation" json:"DaysAfterInitiation"`
}

// ParseBucketLifecycleConfig 从 XML 解析生命周期配置.
func ParseBucketLifecycleConfig(reader io.Reader) (*LifecycleConfig, error) {
	var c LifecycleConfig
	err := xml.NewDecoder(io.LimitReader(reader, 10*1024*1024)).Decode(&c)
	if err != nil {
		return nil, fmt.Errorf("decoding lifecycle xml: %w", err)
	}
	if c.XMLNS == "" {
		c.XMLNS = DefaultXMLNS
	}
	return &c, nil
}

// DefaultLifecycleRootElement 是 PUT ?lifecycle 的标准根元素名.
const DefaultLifecycleRootElement = "LifecycleConfiguration"

// ToXML 将生命周期配置序列化为 XML, 根元素固定为 <LifecycleConfiguration>.
func (c *LifecycleConfig) ToXML() ([]byte, error) {
	return marshalLifecycleXML(c, "", "")
}

// marshalLifecycleXML 序列化生命周期配置, 允许覆盖根元素名与命名空间.
// root 为空时用 DefaultLifecycleRootElement; xmlns 为空时沿用 c.XMLNS, 仍为空则用 DefaultXMLNS.
// 厂商差异 (如要求 <BucketLifecycleConfiguration>) 由 Quirks 经此处注入, 无需改类型定义.
func marshalLifecycleXML(c *LifecycleConfig, root, xmlns string) ([]byte, error) {
	if root == "" {
		root = DefaultLifecycleRootElement
	}
	if xmlns == "" {
		xmlns = c.XMLNS
	}
	if xmlns == "" {
		xmlns = DefaultXMLNS
	}
	// XMLName 不写 tag: 根元素名由运行期值决定, 便于按厂商覆盖.
	wrapper := struct {
		XMLName xml.Name
		XMLNS   string          `xml:"xmlns,attr,omitempty"`
		Rules   []LifecycleRule `xml:"Rule,omitempty"`
	}{
		XMLName: xml.Name{Local: root},
		XMLNS:   xmlns,
		Rules:   c.Rules,
	}
	data, err := xml.Marshal(wrapper)
	if err != nil {
		return nil, fmt.Errorf("marshaling lifecycle xml: %w", err)
	}
	return append([]byte(xml.Header), data...), nil
}

// ----------------------------------------------------------------------------
// 事件通知
//
// 这些类型同时承载两种线格式, 字段名按各自规范独立标注:
//   - XML (S3 线协议): <CloudFunctionConfiguration>/<CloudFunction> 等历史命名
//   - JSON (AWS CLI / SDK 兼容): LambdaFunctionArn / QueueArn / TopicArn,
//     过滤条件为 Filter.Key.FilterRules
//
// 因此 `bucket event get` 输出的 JSON 可直接回灌 `bucket event set`, 也可直接用于
// aws s3api put-bucket-notification-configuration。回归测试见
// bucket-notification-json_test.go。
// ----------------------------------------------------------------------------

// TopicConfiguration 主题通知配置.
type TopicConfiguration struct {
	XMLName  xml.Name            `xml:"TopicConfiguration" json:"-"`
	ID       string              `xml:"Id,omitempty" json:"Id,omitempty"`
	TopicARN string              `xml:"Topic" json:"TopicArn,omitempty"`
	Events   []string            `xml:"Event" json:"Events,omitempty"`
	Filter   *NotificationFilter `xml:"Filter,omitempty" json:"Filter,omitempty"`
}

// QueueConfiguration 队列通知配置.
type QueueConfiguration struct {
	XMLName  xml.Name            `xml:"QueueConfiguration" json:"-"`
	ID       string              `xml:"Id,omitempty" json:"Id,omitempty"`
	QueueARN string              `xml:"Queue" json:"QueueArn,omitempty"`
	Events   []string            `xml:"Event" json:"Events,omitempty"`
	Filter   *NotificationFilter `xml:"Filter,omitempty" json:"Filter,omitempty"`
}

// LambdaFunctionConfiguration Lambda 函数通知配置.
type LambdaFunctionConfiguration struct {
	XMLName   xml.Name            `xml:"CloudFunctionConfiguration" json:"-"`
	ID        string              `xml:"Id,omitempty" json:"Id,omitempty"`
	LambdaARN string              `xml:"CloudFunction" json:"LambdaFunctionArn,omitempty"`
	Events    []string            `xml:"Event" json:"Events,omitempty"`
	Filter    *NotificationFilter `xml:"Filter,omitempty" json:"Filter,omitempty"`
}

// NotificationFilter 通知过滤规则.
//
// S3Key 字段在 XML 中写作 <S3Key>, 在 AWS CLI JSON 中写作 "Key"
// (对应 SDK 模型的 NotificationConfigurationFilter.Key), 故两种 tag 名不同。
type NotificationFilter struct {
	XMLName xml.Name `xml:"Filter" json:"-"`
	S3Key   struct {
		XMLName     xml.Name     `xml:"S3Key" json:"-"`
		FilterRules []FilterRule `xml:"FilterRule" json:"FilterRules,omitempty"`
	} `xml:"S3Key" json:"Key"`
}

// FilterRule 单条过滤规则.
type FilterRule struct {
	XMLName xml.Name `xml:"FilterRule" json:"-"`
	Name    string   `xml:"Name" json:"Name,omitempty"`
	Value   string   `xml:"Value" json:"Value,omitempty"`
}

// NotificationConfiguration 桶事件通知配置.
//
// 写方向: 默认使用与 AWS 线协议一致的 <CloudFunctionConfiguration>/<CloudFunction>,
// 可经 Quirks.LambdaNotificationElement 切换为 <LambdaFunctionConfiguration>/<LambdaFunction>.
// 读方向: UnmarshalXML 始终同时接受两种命名, 见下方实现.
// JSON 方向: 字段名与 AWS CLI 一致 (LambdaFunctionConfigurations 等), XMLName 不参与 JSON.
type NotificationConfiguration struct {
	XMLName                      xml.Name                      `xml:"NotificationConfiguration" json:"-"`
	TopicConfigurations          []TopicConfiguration          `xml:"TopicConfiguration,omitempty" json:"TopicConfigurations,omitempty"`
	QueueConfigurations          []QueueConfiguration          `xml:"QueueConfiguration,omitempty" json:"QueueConfigurations,omitempty"`
	LambdaFunctionConfigurations []LambdaFunctionConfiguration `xml:"CloudFunctionConfiguration,omitempty" json:"LambdaFunctionConfigurations,omitempty"`
}

// lambdaFunctionConfigurationModern 是 Lambda 通知的现代命名变体, 仅用于序列化:
// LambdaFunctionConfiguration 的 XMLName tag 固定为 CloudFunctionConfiguration,
// 会覆盖外层字段 tag, 因此需要独立的类型承载 <LambdaFunctionConfiguration>.
type lambdaFunctionConfigurationModern struct {
	XMLName   xml.Name            `xml:"LambdaFunctionConfiguration"`
	ID        string              `xml:"Id,omitempty"`
	LambdaARN string              `xml:"LambdaFunction"`
	Events    []string            `xml:"Event"`
	Filter    *NotificationFilter `xml:"Filter,omitempty"`
}

// UnmarshalXML 宽容解析事件通知配置: AWS 返回 <CloudFunctionConfiguration>/<CloudFunction>,
// 部分厂商返回 <LambdaFunctionConfiguration>/<LambdaFunction>; 两者都接受, 并统一归一化到
// LambdaFunctionConfigurations (读方向放宽不改变对外行为, 故不设开关).
func (n *NotificationConfiguration) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	type raw struct {
		XMLName       xml.Name                            `xml:"NotificationConfiguration"`
		Topics        []TopicConfiguration                `xml:"TopicConfiguration"`
		Queues        []QueueConfiguration                `xml:"QueueConfiguration"`
		CloudLambdas  []LambdaFunctionConfiguration       `xml:"CloudFunctionConfiguration"`
		ModernLambdas []lambdaFunctionConfigurationModern `xml:"LambdaFunctionConfiguration"`
	}
	var r raw
	if err := d.DecodeElement(&r, &start); err != nil {
		return err
	}
	n.XMLName = r.XMLName
	n.TopicConfigurations = r.Topics
	n.QueueConfigurations = r.Queues
	n.LambdaFunctionConfigurations = r.CloudLambdas
	for _, l := range r.ModernLambdas {
		n.LambdaFunctionConfigurations = append(n.LambdaFunctionConfigurations, LambdaFunctionConfiguration{
			XMLName:   xml.Name{Local: "CloudFunctionConfiguration"},
			ID:        l.ID,
			LambdaARN: l.LambdaARN,
			Events:    l.Events,
			Filter:    l.Filter,
		})
	}
	return nil
}

// ----------------------------------------------------------------------------
// 标签 / 版本
// ----------------------------------------------------------------------------

// Tagging 描述单个键值标签, 同时用于桶标签与对象标签.
type Tagging struct {
	XMLName xml.Name `xml:"Tag"`
	Key     string   `xml:"Key"`
	Value   string   `xml:"Value"`
}

// BucketVersioningStatus 版本控制状态.
type BucketVersioningStatus string

const (
	// VersioningEnabled 启用版本控制.
	VersioningEnabled BucketVersioningStatus = "Enabled"
	// VersioningSuspended 暂停版本控制.
	VersioningSuspended BucketVersioningStatus = "Suspended"
)

// ----------------------------------------------------------------------------
// 公共访问阻断 (Public Access Block)
// ----------------------------------------------------------------------------

// PublicAccessBlockConfiguration 桶级公共访问阻断配置.
type PublicAccessBlockConfiguration struct {
	XMLName               xml.Name `xml:"PublicAccessBlockConfiguration" json:"-"`
	BlockPublicAcls       bool     `xml:"BlockPublicAcls" json:"BlockPublicAcls"`
	IgnorePublicAcls      bool     `xml:"IgnorePublicAcls" json:"IgnorePublicAcls"`
	BlockPublicPolicy     bool     `xml:"BlockPublicPolicy" json:"BlockPublicPolicy"`
	RestrictPublicBuckets bool     `xml:"RestrictPublicBuckets" json:"RestrictPublicBuckets"`
}

// ----------------------------------------------------------------------------
// Object Lock
// ----------------------------------------------------------------------------

// ObjectLockConfiguration 桶级 Object Lock 配置 (含默认保留规则).
type ObjectLockConfiguration struct {
	XMLName           xml.Name        `xml:"ObjectLockConfiguration" json:"-"`
	ObjectLockEnabled string          `xml:"ObjectLockEnabled,omitempty" json:"ObjectLockEnabled,omitempty"` // "Enabled"
	Rule              *ObjectLockRule `xml:"Rule,omitempty" json:"Rule,omitempty"`
}

// ObjectLockRule Object Lock 默认保留规则容器.
type ObjectLockRule struct {
	DefaultRetention DefaultRetention `xml:"DefaultRetention" json:"DefaultRetention"`
}

// DefaultRetention 默认保留设置.
type DefaultRetention struct {
	Mode  string `xml:"Mode" json:"Mode"` // GOVERNANCE / COMPLIANCE
	Days  int    `xml:"Days,omitempty" json:"Days,omitempty"`
	Years int    `xml:"Years,omitempty" json:"Years,omitempty"`
}

// ObjectLockRetention 对象级保留设置 (PUT/GET ?retention).
type ObjectLockRetention struct {
	XMLName         xml.Name `xml:"Retention" json:"-"`
	Mode            string   `xml:"Mode" json:"Mode"`                                           // GOVERNANCE / COMPLIANCE
	RetainUntilDate string   `xml:"RetainUntilDate,omitempty" json:"RetainUntilDate,omitempty"` // RFC3339
}

// ObjectLockLegalHold 对象级法律留存 (PUT/GET ?legal-hold).
type ObjectLockLegalHold struct {
	XMLName xml.Name `xml:"LegalHold" json:"-"`
	Status  string   `xml:"Status" json:"Status"` // ON / OFF
}

// ----------------------------------------------------------------------------
// 归档恢复 (RestoreObject)
// ----------------------------------------------------------------------------

// RestoreRequest POST ?restore 请求体.
type RestoreRequest struct {
	XMLName              xml.Name              `xml:"RestoreRequest" json:"-"`
	Days                 int                   `xml:"Days" json:"Days"`
	GlacierJobParameters *GlacierJobParameters `xml:"GlacierJobParameters,omitempty" json:"GlacierJobParameters,omitempty"`
}

// GlacierJobParameters 归档恢复层级.
type GlacierJobParameters struct {
	Tier string `xml:"Tier" json:"Tier"` // Expedited / Standard / Bulk
}

// ----------------------------------------------------------------------------
// 复制 (Replication)
// ----------------------------------------------------------------------------

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

// ----------------------------------------------------------------------------
// 访问控制列表 (ACL)
//
// S3 ACL 的 XML (含 xsi:type 的 Grantee) 较繁琐且易错; 此处 PUT 采用 canned ACL
// 头 (x-amz-acl) 与 grant 头 (x-amz-grant-*), 覆盖绝大多数场景且不依赖 XML;
// GET 返回原始 XML ([]byte) 供调用方自行解析或展示.
// ----------------------------------------------------------------------------

// ACLOptions 控制 ACL 设置 (canned ACL + grant 头).
type ACLOptions struct {
	ACL              string `json:"ACL,omitempty"` // x-amz-acl: private|public-read|public-read-write|authenticated-read|bucket-owner-read|bucket-owner-full-control|log-delivery-write
	GrantFullControl string `json:"GrantFullControl,omitempty"`
	GrantRead        string `json:"GrantRead,omitempty"`
	GrantReadACP     string `json:"GrantReadACP,omitempty"`
	GrantWrite       string `json:"GrantWrite,omitempty"`
	GrantWriteACP    string `json:"GrantWriteACP,omitempty"`
}
