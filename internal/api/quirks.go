// quirks.go 定义厂商差异开关 Quirks: 不同 S3 兼容实现在线协议细节上的分歧,
// 集中以开关形式暴露, 使适配新厂商只需改配置而非改代码.
//
// 设计约定 (新增厂商差异时请沿用):
//   - 零值 = 严格 AWS 语义, 也就是本项目既有的默认行为; 任何字段不设置都不会
//     改变发出的请求。
//   - 一个开关只有一个落点 (见各字段注释末尾的落点标注); 请求构建的收口点是
//     newRequest / Do / resolveURL, 线协议收口点是各操作文件里的 marshal*XML helper。
//   - 读取方向尽量无条件放宽 (如通知配置同时接受两种 Lambda 元素命名), 因为多接受
//     一种输入不改变对外行为; 只有"发出的请求长什么样"才需要开关。
//
// 已有的逃生口不需要新开关:
//   - 寻址方式:      Options.BucketLookup / Options.BucketLookupViaURL (自定义模板)
//   - TLS 版本/校验:  Options.Transport (internal/client 依别名配置构造)
//   - 重试次数:      Options.MaxRetries
//   - 预签名版本:    PresignedURL (SigV4) / PresignV2 (兼容旧式服务)
//   - 自定义 header:  Transport 包装 (internal/client 的 header / user-agent 装饰器)

package api

// Lambda 通知元素命名取值, 供 Quirks.LambdaNotificationElement 使用.
const (
	// LambdaNotificationElementCloud 使用 <CloudFunctionConfiguration>/<CloudFunction>,
	// 与 AWS S3 实际返回的线协议一致 (也是本项目的默认值)。
	LambdaNotificationElementCloud = "cloud"
	// LambdaNotificationElementLambda 使用 <LambdaFunctionConfiguration>/<LambdaFunction>,
	// 部分厂商按 API 名称而非历史线协议命名。
	LambdaNotificationElementLambda = "lambda"
)

// Quirks 描述非 AWS 实现的兼容差异。零值表示严格 AWS 语义。
//
// 通过 Options.Quirks 传入; 命令行侧由别名配置 (internal/config) 的对应键构造。
type Quirks struct {
	// LifecycleRootElement 覆盖 PUT ?lifecycle 的根元素名, 空 = "LifecycleConfiguration"。
	// 少数厂商要求与 GET 返回一致的 <BucketLifecycleConfiguration>。
	// 落点: bucket-lifecycle.go SetBucketLifecycle -> marshalLifecycleXML。
	LifecycleRootElement string

	// LambdaNotificationElement 选择 Lambda 通知的元素命名, 取值见
	// LambdaNotificationElement* 常量, 空 = cloud。
	// 落点: bucket-notification.go SetBucketNotification -> marshalNotificationXML。
	// 读取方向无需开关: NotificationConfiguration.UnmarshalXML 始终接受两种命名。
	LambdaNotificationElement string

	// DisableRegionRedirect 关闭 301/307/400 + X-Amz-Bucket-Region 的重签重发。
	// 用于返回错误 region 头、或不允许跨 region 重签的端点。
	// 落点: api.go Do 的 region 重定向分支。
	DisableRegionRedirect bool

	// DisableRegionProbe 关闭自定义寻址模板 %(region) 的 GetBucketLocation 探测,
	// 直接用配置 region。用于不实现 ?location 或对其报错的端点。
	// 落点: bucket-lookup.go resolveBucketRegion。
	DisableRegionProbe bool

	// ForceUnsignedPayload 强制 x-amz-content-sha256: UNSIGNED-PAYLOAD,
	// 覆盖调用方预计算值。用于不接受请求体哈希、或上传经由会改写 body 的代理的场景。
	// 落点: api.go newRequest 的 x-amz-content-sha256 计算。
	ForceUnsignedPayload bool

	// XMLNS 覆盖 CORS / 生命周期 XML 的命名空间, 空 = DefaultXMLNS。
	// 落点: bucket-cors.go marshalCorsXML, bucket-types.go marshalLifecycleXML。
	XMLNS string
}

// normalize 返回补齐默认值后的副本: nil 与零值等价, 便于上层直接直通配置.
func (q Quirks) normalize() Quirks {
	if q.LambdaNotificationElement == "" {
		q.LambdaNotificationElement = LambdaNotificationElementCloud
	}
	if q.XMLNS == "" {
		q.XMLNS = DefaultXMLNS
	}
	return q
}
