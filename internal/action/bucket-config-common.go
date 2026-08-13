// bucket-config-common.go 提供桶子资源 (CORS/Lifecycle/Encryption/Notification 等)
// 配置命令的通用辅助: pretty-JSON 打印、删除样板、AWS CLI 兼容 JSON 加载.

package action

import (
	"encoding/json"
	"fmt"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/i18n"
)

// printBucketConfigJSON 把桶级配置以 pretty JSON 打印, 统一了各 GetXxx 的输出样板。
// header 为打印的标题 (如 "encryption" / "lifecycle"), fetchErrLabel 用于错误信息。
func (c *Action) printBucketConfigJSON(bucket, header string, cfg any) error {
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", header, err)
	}
	myprint.PrintfBoldBlue("# %s %s %s\n", c.Alias, bucket, header)
	myprint.PrintlnGreen(string(b))
	return nil
}

// deleteBucketConfig 执行桶级配置删除并打印成功信息, 统一了各 DelXxx 的样板。
// label 用于错误信息 (如 "cors"), doneMsg 为成功提示模板 (含两个 %s: alias、bucket)。
func (c *Action) deleteBucketConfig(bucket, label, doneMsg string, del func() error) error {
	if err := del(); err != nil {
		return fmt.Errorf("delete %s %s: %s", label, bucket, FormatAPIError(err))
	}
	myprint.PrintfBoldGreen(doneMsg, c.Alias, bucket)
	return nil
}

// loadJSONConfig 从本地文件加载 AWS CLI 兼容的 JSON 配置并解码到 *T。
// label 用于错误信息 (如 "encryption")。仅接受 JSON 格式。
func loadJSONConfig[T any](file, label string) (*T, error) {
	data, format, err := loadAWSConfigFile(file)
	if err != nil {
		return nil, err
	}
	if format != "json" {
		return nil, fmt.Errorf(i18n.T("%s only supports JSON format (AWS CLI compatible)", "%s 仅支持 JSON 格式（兼容 AWS CLI）"), label)
	}
	var cfg T
	if err := unmarshalAWS(data, "json", &cfg); err != nil {
		return nil, fmt.Errorf("parse %s file %s: %w", label, file, err)
	}
	return &cfg, nil
}
