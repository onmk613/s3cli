//go:build aws

// aws-config.go 实现 AWS 上的桶子资源配置管理:
// CORS / 加密 / 生命周期 / 事件通知 / 版本控制 / 桶策略.
//
// s3iface 配置类型 (CorsConfig / LifecycleConfig 等) 在此转换为 SDK 类型后调用对应 API.

package awss3

import (
	"context"
	"encoding/xml"

	"s3cli/pkg/s3iface"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// ---- CORS ----

func toSDKCorsConfig(c *s3iface.CorsConfig) *types.CORSConfiguration {
	out := &types.CORSConfiguration{}
	for _, r := range c.CORSRules {
		rule := types.CORSRule{
			AllowedHeaders: r.AllowedHeader,
			AllowedMethods: r.AllowedMethod,
			AllowedOrigins: r.AllowedOrigin,
			ExposeHeaders:  r.ExposeHeader,
		}
		if r.MaxAgeSeconds > 0 {
			rule.MaxAgeSeconds = aws.Int32(int32(r.MaxAgeSeconds))
		}
		if r.ID != "" {
			rule.ID = aws.String(r.ID)
		}
		out.CORSRules = append(out.CORSRules, rule)
	}
	return out
}

func fromSDKCorsRules(rules []types.CORSRule) *s3iface.CorsConfig {
	out := &s3iface.CorsConfig{}
	for _, r := range rules {
		out.CORSRules = append(out.CORSRules, s3iface.CorsRule{
			AllowedHeader: r.AllowedHeaders,
			AllowedMethod: r.AllowedMethods,
			AllowedOrigin: r.AllowedOrigins,
			ExposeHeader:  r.ExposeHeaders,
			ID:            aws.ToString(r.ID),
			MaxAgeSeconds: int(aws.ToInt32(r.MaxAgeSeconds)),
		})
	}
	return out
}

func (a *AWS) SetBucketCors(ctx context.Context, bucket string, config *s3iface.CorsConfig) error {
	_, err := a.client.PutBucketCors(ctx, &s3.PutBucketCorsInput{
		Bucket:            aws.String(bucket),
		CORSConfiguration: toSDKCorsConfig(config),
	})
	return sdkErr(err)
}

func (a *AWS) GetBucketCors(ctx context.Context, bucket string) (*s3iface.CorsConfig, error) {
	out, err := a.client.GetBucketCors(ctx, &s3.GetBucketCorsInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		return nil, sdkErr(err)
	}
	return fromSDKCorsRules(out.CORSRules), nil
}

func (a *AWS) DeleteBucketCors(ctx context.Context, bucket string) error {
	_, err := a.client.DeleteBucketCors(ctx, &s3.DeleteBucketCorsInput{
		Bucket: aws.String(bucket),
	})
	return sdkErr(err)
}

// ---- 加密 ----

func (a *AWS) SetBucketEncryption(ctx context.Context, bucket string, config *s3iface.ServerSideEncryptionConfiguration) error {
	var sdkRules []types.ServerSideEncryptionRule
	for _, r := range config.Rules {
		rule := types.ServerSideEncryptionRule{
			ApplyServerSideEncryptionByDefault: &types.ServerSideEncryptionByDefault{
				SSEAlgorithm:   types.ServerSideEncryption(r.ApplyServerSideEncryptionByDefault.SSEAlgorithm),
				KMSMasterKeyID: aws.String(r.ApplyServerSideEncryptionByDefault.KMSMasterKeyID),
			},
		}
		if r.BucketKeyEnabled != nil {
			rule.BucketKeyEnabled = aws.Bool(*r.BucketKeyEnabled)
		}
		sdkRules = append(sdkRules, rule)
	}
	_, err := a.client.PutBucketEncryption(ctx, &s3.PutBucketEncryptionInput{
		Bucket: aws.String(bucket),
		ServerSideEncryptionConfiguration: &types.ServerSideEncryptionConfiguration{
			Rules: sdkRules,
		},
	})
	return sdkErr(err)
}

func (a *AWS) GetBucketEncryption(ctx context.Context, bucket string) (*s3iface.ServerSideEncryptionConfiguration, error) {
	out, err := a.client.GetBucketEncryption(ctx, &s3.GetBucketEncryptionInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		return nil, sdkErr(err)
	}
	cfg := &s3iface.ServerSideEncryptionConfiguration{}
	if out.ServerSideEncryptionConfiguration != nil {
		for _, r := range out.ServerSideEncryptionConfiguration.Rules {
			rule := s3iface.ServerSideEncryptionRule{}
			if r.ApplyServerSideEncryptionByDefault != nil {
				rule.ApplyServerSideEncryptionByDefault = s3iface.ServerSideEncryptionByDefault{
					SSEAlgorithm:   string(r.ApplyServerSideEncryptionByDefault.SSEAlgorithm),
					KMSMasterKeyID: aws.ToString(r.ApplyServerSideEncryptionByDefault.KMSMasterKeyID),
				}
			}
			if r.BucketKeyEnabled != nil {
				bk := aws.ToBool(r.BucketKeyEnabled)
				rule.BucketKeyEnabled = &bk
			}
			cfg.Rules = append(cfg.Rules, rule)
		}
	}
	return cfg, nil
}

func (a *AWS) DeleteBucketEncryption(ctx context.Context, bucket string) error {
	_, err := a.client.DeleteBucketEncryption(ctx, &s3.DeleteBucketEncryptionInput{
		Bucket: aws.String(bucket),
	})
	return sdkErr(err)
}

// ---- 生命周期 ----

func (a *AWS) SetBucketLifecycle(ctx context.Context, bucket string, config *s3iface.LifecycleConfig) error {
	// 将 s3iface 配置序列化为 XML, 再反序列化为 SDK 类型, 避免逐字段映射的遗漏.
	data, err := config.ToXML()
	if err != nil {
		return err
	}
	var sdkCfg types.BucketLifecycleConfiguration
	if err := xml.Unmarshal(data, &sdkCfg); err != nil {
		return err
	}
	_, err = a.client.PutBucketLifecycleConfiguration(ctx, &s3.PutBucketLifecycleConfigurationInput{
		Bucket:                 aws.String(bucket),
		LifecycleConfiguration: &sdkCfg,
	})
	return sdkErr(err)
}

func (a *AWS) GetBucketLifecycle(ctx context.Context, bucket string) (*s3iface.LifecycleConfig, error) {
	out, err := a.client.GetBucketLifecycleConfiguration(ctx, &s3.GetBucketLifecycleConfigurationInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		return nil, sdkErr(err)
	}
	// SDK 类型 → XML → s3iface 类型
	sdkCfg := types.BucketLifecycleConfiguration{Rules: out.Rules}
	data, err := xml.Marshal(sdkCfg)
	if err != nil {
		return nil, err
	}
	cfg := &s3iface.LifecycleConfig{}
	if err := xml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (a *AWS) DeleteBucketLifecycle(ctx context.Context, bucket string) error {
	_, err := a.client.DeleteBucketLifecycle(ctx, &s3.DeleteBucketLifecycleInput{
		Bucket: aws.String(bucket),
	})
	return sdkErr(err)
}

// ---- 事件通知 ----

func (a *AWS) SetBucketNotification(ctx context.Context, bucket string, config *s3iface.NotificationConfiguration) error {
	data, err := marshalXMLWithHeaderIface(config)
	if err != nil {
		return err
	}
	var sdkCfg types.NotificationConfiguration
	if err := xml.Unmarshal(data, &sdkCfg); err != nil {
		return err
	}
	_, err = a.client.PutBucketNotificationConfiguration(ctx, &s3.PutBucketNotificationConfigurationInput{
		Bucket:                    aws.String(bucket),
		NotificationConfiguration: &sdkCfg,
	})
	return sdkErr(err)
}

func (a *AWS) GetBucketNotification(ctx context.Context, bucket string) (*s3iface.NotificationConfiguration, error) {
	out, err := a.client.GetBucketNotificationConfiguration(ctx, &s3.GetBucketNotificationConfigurationInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		return nil, sdkErr(err)
	}
	cfg := &s3iface.NotificationConfiguration{}
	for _, t := range out.TopicConfigurations {
		cfg.TopicConfigurations = append(cfg.TopicConfigurations, s3iface.TopicConfiguration{
			ID: aws.ToString(t.Id), TopicARN: aws.ToString(t.TopicArn), Events: eventStrings(t.Events),
		})
	}
	for _, q := range out.QueueConfigurations {
		cfg.QueueConfigurations = append(cfg.QueueConfigurations, s3iface.QueueConfiguration{
			ID: aws.ToString(q.Id), QueueARN: aws.ToString(q.QueueArn), Events: eventStrings(q.Events),
		})
	}
	for _, l := range out.LambdaFunctionConfigurations {
		cfg.LambdaFunctionConfigurations = append(cfg.LambdaFunctionConfigurations, s3iface.LambdaFunctionConfiguration{
			ID: aws.ToString(l.Id), LambdaARN: aws.ToString(l.LambdaFunctionArn), Events: eventStrings(l.Events),
		})
	}
	return cfg, nil
}

func (a *AWS) DeleteBucketNotification(ctx context.Context, bucket string) error {
	_, err := a.client.PutBucketNotificationConfiguration(ctx, &s3.PutBucketNotificationConfigurationInput{
		Bucket:                    aws.String(bucket),
		NotificationConfiguration: &types.NotificationConfiguration{},
	})
	return sdkErr(err)
}

func eventStrings(events []types.Event) []string {
	result := make([]string, 0, len(events))
	for _, e := range events {
		result = append(result, string(e))
	}
	return result
}

// ---- 版本控制 ----

func (a *AWS) SetBucketVersioning(ctx context.Context, bucket string, status s3iface.BucketVersioningStatus) error {
	_, err := a.client.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{
		Bucket: aws.String(bucket),
		VersioningConfiguration: &types.VersioningConfiguration{
			Status: types.BucketVersioningStatus(status),
		},
	})
	return sdkErr(err)
}

func (a *AWS) GetBucketVersioning(ctx context.Context, bucket string) (s3iface.BucketVersioningStatus, error) {
	out, err := a.client.GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		return "", sdkErr(err)
	}
	return s3iface.BucketVersioningStatus(out.Status), nil
}

// ---- 桶策略 ----

func (a *AWS) SetBucketPolicy(ctx context.Context, bucket string, data []byte) error {
	_, err := a.client.PutBucketPolicy(ctx, &s3.PutBucketPolicyInput{
		Bucket: aws.String(bucket),
		Policy: aws.String(string(data)),
	})
	return sdkErr(err)
}

func (a *AWS) GetBucketPolicy(ctx context.Context, bucket string) ([]byte, error) {
	out, err := a.client.GetBucketPolicy(ctx, &s3.GetBucketPolicyInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		return nil, sdkErr(err)
	}
	return []byte(aws.ToString(out.Policy)), nil
}

func (a *AWS) DeleteBucketPolicy(ctx context.Context, bucket string) error {
	_, err := a.client.DeleteBucketPolicy(ctx, &s3.DeleteBucketPolicyInput{
		Bucket: aws.String(bucket),
	})
	return sdkErr(err)
}

// marshalXMLWithHeaderIface 序列化 s3iface 配置类型为带声明的 XML.
func marshalXMLWithHeaderIface(v any) ([]byte, error) {
	body, err := xml.Marshal(v)
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), body...), nil
}
