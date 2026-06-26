// 封装一个多个统一接口, 实现api规范化

// type GenericAPI interface {} 完全封装 aws-sdk-go-v2 的 s3.Client
// 基本的都是兼容 aws 官方的 sdk

// 针对其他各自厂商，通过接口实现对业务的无感知兼容
// 方便后续对各厂商api变更后代码调整

// 实现 pkg/action 中的代码完全脱离对 sdk 的依赖
// 只需要关注处理返回和输出格式化

package api
