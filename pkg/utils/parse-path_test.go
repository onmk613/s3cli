package utils

import (
	"errors"
	"testing"
)

func TestParseS3Path(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    *S3Path
		wantErr error
	}{
		{
			name:    "alias only",
			input:   "my-s3",
			want:    &S3Path{Alias: "my-s3"},
			wantErr: ErrAliasOnly,
		},
		{
			name:  "alias and bucket",
			input: "my-s3:mybucket",
			want:  &S3Path{Alias: "my-s3", Bucket: "mybucket"},
		},
		{
			name:  "alias, bucket, and key",
			input: "my-s3:mybucket/a/b.txt",
			want:  &S3Path{Alias: "my-s3", Bucket: "mybucket", Key: "a/b.txt"},
		},
		{
			name:  "trailing slash directory",
			input: "my-s3:mybucket/dir/",
			want:  &S3Path{Alias: "my-s3", Bucket: "mybucket", Key: "dir/", TrailingSlash: true},
		},
		{
			name:  "key with multiple slashes",
			input: "my-s3:mybucket/a/b/c/d.txt",
			want:  &S3Path{Alias: "my-s3", Bucket: "mybucket", Key: "a/b/c/d.txt"},
		},
		{
			name:  "bucket with trailing slash, no key",
			input: "my-s3:mybucket/",
			want:  &S3Path{Alias: "my-s3", Bucket: "mybucket"},
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: errors.New("empty s3 path"),
		},
		{
			name:    "only colon",
			input:   ":",
			wantErr: errors.New(""),
		},
		{
			name:    "colon with no bucket",
			input:   "my-s3:",
			wantErr: errors.New(""),
		},
		{
			name:    "invalid alias starting with number but too short",
			input:   "a:mybucket",
			wantErr: errors.New(""),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseS3Path(tt.input)
			if tt.wantErr != nil {
				if err == nil {
					t.Errorf("ParseS3Path(%q) expected error, got nil", tt.input)
					return
				}
				return
			}
			if err != nil {
				t.Errorf("ParseS3Path(%q) unexpected error: %v", tt.input, err)
				return
			}
			if got.Alias != tt.want.Alias {
				t.Errorf("Alias: got %q, want %q", got.Alias, tt.want.Alias)
			}
			if got.Bucket != tt.want.Bucket {
				t.Errorf("Bucket: got %q, want %q", got.Bucket, tt.want.Bucket)
			}
			if got.Key != tt.want.Key {
				t.Errorf("Key: got %q, want %q", got.Key, tt.want.Key)
			}
			if got.TrailingSlash != tt.want.TrailingSlash {
				t.Errorf("TrailingSlash: got %v, want %v", got.TrailingSlash, tt.want.TrailingSlash)
			}
		})
	}
}

func TestErrAliasOnly(t *testing.T) {
	_, err := ParseS3Path("my-s3")
	if err == nil {
		t.Fatal("expected error for alias-only input")
	}
	if !errors.Is(err, ErrAliasOnly) {
		t.Errorf("expected ErrAliasOnly, got %v", err)
	}
}
