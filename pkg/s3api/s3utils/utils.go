package s3utils

import (
	"errors"
	"regexp"
	"strings"
)

var (
	bucketNameRegex = regexp.MustCompile(`^[A-Za-z0-9][a-zA-Z0-9\_\-\.]{1,61}[A-Za-z0-9]$`)
	ipAddress       = regexp.MustCompile(`^(\d+\.){3}\d+$`)
)

func CheckValidBucketNameStrict(bucketName string) error {
	if strings.TrimSpace(bucketName) == "" {
		return errors.New("Bucket name cannot be empty")
	}
	if len(bucketName) < 3 {
		return errors.New("Bucket name cannot be shorter than 3 characters")
	}
	if len(bucketName) > 63 {
		return errors.New("Bucket name cannot be longer than 63 characters")
	}
	if ipAddress.MatchString(bucketName) {
		return errors.New("Bucket name cannot be an ip address")
	}
	if strings.Contains(bucketName, "..") || strings.Contains(bucketName, ".-") || strings.Contains(bucketName, "-.") {
		return errors.New("Bucket name contains invalid characters")
	}
	if !bucketNameRegex.MatchString(bucketName) {
		return errors.New("Bucket name contains invalid characters")
	}
	return nil
}
