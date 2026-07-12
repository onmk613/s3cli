package s3utils

import "testing"

func TestCheckValidBucketNameStrict(t *testing.T) {
	valid := []string{"abc", "bucket-name", "bucket.name"}
	for _, name := range valid {
		if err := CheckValidBucketNameStrict(name); err != nil {
			t.Fatalf("%q rejected: %v", name, err)
		}
	}
	invalid := []string{"ab", "192.168.1.1", "bad..name", "bad/name", "-start"}
	for _, name := range invalid {
		if err := CheckValidBucketNameStrict(name); err == nil {
			t.Fatalf("%q accepted", name)
		}
	}
}
