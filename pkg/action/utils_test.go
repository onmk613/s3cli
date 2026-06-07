package action

import (
	"errors"
	"fmt"
	"testing"

	"github.com/aws/smithy-go"
)

func TestFormatAPIError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"nil error", nil, ""},
		{"generic error", errors.New("something broke"), "something broke"},
		{"wrapped generic error", fmt.Errorf("wrap: %w", errors.New("inner")), "wrap: inner"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatAPIError(tt.err)
			if got != tt.want {
				t.Errorf("FormatAPIError(%v) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}

func TestFormatAPIError_Smithy(t *testing.T) {
	// smithy API error
	apiErr := &smithy.GenericAPIError{
		Code:    "NoSuchBucket",
		Message: "The specified bucket does not exist",
	}
	got := FormatAPIError(apiErr)
	want := "NoSuchBucket: The specified bucket does not exist"
	if got != want {
		t.Errorf("FormatAPIError = %q, want %q", got, want)
	}
}

func TestAddMime_Idempotent(t *testing.T) {
	// 调用两次不应 panic
	AddMime()
	AddMime()
}
