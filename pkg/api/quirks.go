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
// 通过 Options.Quirks 传入; 命令行封装层会从别名配置的对应键映射到这里。
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
	// 落点: bucket-cors.go marshalCorsXML, bucket-lifecycle.go marshalLifecycleXML。
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
