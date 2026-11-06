package qoder

import (
	"testing"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

func TestIsImageConversionTimeoutMessage(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"Image conversion timeout", true},
		{"image_conversion_timeout", true},
		{`{"code":"provider_error","message":"Image conversion timeout","details":"image conversion timeout after 30 seconds"}`, true},
		{"image conversion timed out", true},
		{"First Token Timeout", false},
		{"upstream timeout", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isImageConversionTimeoutMessage(tc.in); got != tc.want {
			t.Fatalf("%q: got %v want %v", tc.in, got, tc.want)
		}
	}
}

func TestImageConversionTimeoutErrorCode(t *testing.T) {
	err := imageConversionTimeoutError("Image conversion timeout")
	if !isImageConversionTimeoutErr(err) {
		t.Fatal(err)
	}
	pe, ok := err.(*providers.Error)
	if !ok || pe.Code != providers.CodeUpstreamImageConversionTimeout {
		t.Fatalf("%#v", err)
	}
}
