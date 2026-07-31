//go:build aws

// awsclient_test.go 覆盖官方 SDK 后端 (NewAWSClient) 的构造行为.
// 本文件仅在 -tags aws 构建下编译; 自建后端的等价测试见 client-s3api_test.go.

package client

import (
	"context"
	"testing"

	"s3cli/internal/config"
)

func TestNewAWSClient(t *testing.T) {
	t.Run("valid path-style", func(t *testing.T) {
		cfg := config.Static{
			HostBase:  "https://s3.example.com",
			AccessKey: "AK",
			SecretKey: "SK",
		}
		c, err := NewAWSClient(context.Background(), cfg, config.Flags{})
		if err != nil {
			t.Fatal(err)
		}
		if c == nil {
			t.Fatal("nil client")
		}
	})

	t.Run("bad bucket lookup -> error", func(t *testing.T) {
		cfg := config.Static{HostBase: "https://s3.example.com", AccessKey: "a", SecretKey: "s", BucketLookup: "garbage"}
		if _, err := NewAWSClient(context.Background(), cfg, config.Flags{}); err == nil {
			t.Error("expected error for bad bucket_lookup")
		}
	})

	t.Run("custom lookup", func(t *testing.T) {
		cfg := config.Static{
			HostBase:  "https://s3.example.com",
			AccessKey: "a", SecretKey: "s",
			BucketLookup: "https://%(bucket).s3.example.com",
		}
		if c, err := NewAWSClient(context.Background(), cfg, config.Flags{}); err != nil || c == nil {
			t.Fatalf("client=%v err=%v", c, err)
		}
	})
}
