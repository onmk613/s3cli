package action

import "testing"

func TestS3PathStatic(t *testing.T) {
	tests := []struct {
		name   string
		alias  string
		bucket string
		key    string
		want   string
	}{
		{"bucket only", "my-s3", "mybucket", "", "my-s3:mybucket"},
		{"bucket and key", "my-s3", "mybucket", "a/b.txt", "my-s3:mybucket/a/b.txt"},
		{"key with spaces", "prod", "data", "my file.txt", "prod:data/my file.txt"},
		{"empty alias", "", "bucket", "key", ":bucket/key"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := S3PathStatic(tt.alias, tt.bucket, tt.key)
			if got != tt.want {
				t.Errorf("S3PathStatic(%q, %q, %q) = %q, want %q",
					tt.alias, tt.bucket, tt.key, got, tt.want)
			}
		})
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{0, "0 B"},
		{512, "512.00 B"},
		{1024, "1.00 KB"},
		{1536, "1.50 KB"},
		{1048576, "1.00 MB"},
		{1073741824, "1.00 GB"},
		{-1, "0 B"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := FormatBytes(tt.bytes)
			if got != tt.want {
				t.Errorf("FormatBytes(%d) = %q, want %q", tt.bytes, got, tt.want)
			}
		})
	}
}
