// env.go 提供基于环境变量的别名配置解析, 用于无 TOML 配置文件的 CI / 容器场景.
//
// 至少提供 host + access key + secret key 时, StaticFromEnv 返回一份可用的 Static,
// 供 client 包在别名未命中配置表时回退使用. 同时识别 s3cli 自有变量 (S3CLI_*) 与
// AWS 标准变量 (AWS_*), s3cli 自有变量优先级更高.

package config

import (
	"os"
	"strings"
)

// EnvStaticFromEnv 从环境变量构造一个别名静态配置.
// 至少提供 host + access key + secret key 时返回 (Static, true); 否则 (zero, false).
func StaticFromEnv() (Static, bool) {
	ak := firstNonEmpty(
		os.Getenv("S3CLI_ACCESS_KEY"),
		os.Getenv("AWS_ACCESS_KEY_ID"),
	)
	sk := firstNonEmpty(
		os.Getenv("S3CLI_SECRET_KEY"),
		os.Getenv("AWS_SECRET_ACCESS_KEY"),
	)
	host := firstNonEmpty(
		os.Getenv("S3CLI_HOST"),
		os.Getenv("AWS_ENDPOINT_URL_S3"),
		os.Getenv("AWS_ENDPOINT_URL"),
	)
	if ak == "" || sk == "" || host == "" {
		return Static{}, false
	}

	region := firstNonEmpty(
		os.Getenv("S3CLI_REGION"),
		os.Getenv("AWS_REGION"),
		os.Getenv("AWS_DEFAULT_REGION"),
	)
	if region == "" {
		region = "us-east-1"
	}

	bucketLookup := os.Getenv("S3CLI_BUCKET_LOOKUP")
	if bucketLookup == "" {
		bucketLookup = "path"
	}

	// 默认校验 SSL; 仅当显式启用跳过时关闭.
	verifySSL := !parseBoolEnv(
		firstNonEmpty(os.Getenv("S3CLI_NO_VERIFY_SSL"), os.Getenv("AWS_S3_DISABLE_SSL_VERIFY")),
		false,
	)

	return Static{
		AccessKey:    ak,
		SecretKey:    sk,
		HostBase:     host,
		SessionToken: firstNonEmpty(os.Getenv("S3CLI_SESSION_TOKEN"), os.Getenv("AWS_SESSION_TOKEN")),
		Region:       region,
		BucketLookup: bucketLookup,
		VerifySSL:    verifySSL,
	}, true
}

// firstNonEmpty 返回第一个非空字符串.
func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

// parseBoolEnv 解析布尔型环境变量 (1/true/yes/on 为真, 0/false/no/off 为假), 缺省返回 def.
func parseBoolEnv(s string, def bool) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}
