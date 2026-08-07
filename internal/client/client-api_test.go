// client-api_test.go 覆盖自建后端 (NewS3Client) 的构造行为.
// api 客户端的单元测试.

package client

import (
	"context"
	"testing"

	"s3cli/internal/config"
)

func TestNewS3Client(t *testing.T) {
	t.Run("valid path-style", func(t *testing.T) {
		cfg := config.Static{
			HostBase:  "https://s3.example.com",
			AccessKey: "AK",
			SecretKey: "SK",
		}
		c, err := NewS3Client(context.Background(), cfg, config.Flags{})
		if err != nil {
			t.Fatal(err)
		}
		if c == nil {
			t.Fatal("nil client")
		}
	})

	t.Run("debug mode", func(t *testing.T) {
		cfg := config.Static{HostBase: "https://s3.example.com", AccessKey: "a", SecretKey: "s"}
		if c, err := NewS3Client(context.Background(), cfg, config.Flags{Debug: true}); err != nil || c == nil {
			t.Fatalf("client=%v err=%v", c, err)
		}
	})

	t.Run("bad header -> error", func(t *testing.T) {
		cfg := config.Static{HostBase: "https://s3.example.com", AccessKey: "a", SecretKey: "s"}
		if _, err := NewS3Client(context.Background(), cfg, config.Flags{Headers: []string{"no-separator"}}); err == nil {
			t.Error("expected error for bad header")
		}
	})

	t.Run("bad bucket lookup -> error", func(t *testing.T) {
		cfg := config.Static{HostBase: "https://s3.example.com", AccessKey: "a", SecretKey: "s", BucketLookup: "garbage"}
		if _, err := NewS3Client(context.Background(), cfg, config.Flags{}); err == nil {
			t.Error("expected error for bad bucket_lookup")
		}
	})

	t.Run("custom lookup", func(t *testing.T) {
		cfg := config.Static{
			HostBase:  "https://s3.example.com",
			AccessKey: "a", SecretKey: "s",
			BucketLookup: "https://%(bucket).s3.example.com",
		}
		if c, err := NewS3Client(context.Background(), cfg, config.Flags{}); err != nil || c == nil {
			t.Fatalf("client=%v err=%v", c, err)
		}
	})

	t.Run("missing endpoint -> error", func(t *testing.T) {
		_, err := NewS3Client(context.Background(), config.Static{AccessKey: "a", SecretKey: "s"}, config.Flags{})
		if err == nil {
			t.Error("expected error for missing endpoint")
		}
	})
}
