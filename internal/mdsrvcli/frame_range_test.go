package mdsrvcli

import (
	"strings"
	"testing"

	"github.com/rafflesia-ai/hlmdsrv/internal/mdsrv"
)

// TestParseFrameRangeErrorsAreCoded locks in that a malformed --frames value
// yields a clean validation_failed error (not a leaked strconv error tagged
// internal_error), and that valid ranges still parse.
func TestParseFrameRangeErrorsAreCoded(t *testing.T) {
	ref := mdsrv.FileRef{FrameCount: 5, TimeStep: 1, TimeStart: 0}
	bad := []struct {
		value   string
		wantSub string
	}{
		{"0-2", "expected START:STOP:STRIDE"},     // dash instead of colon (the leaky case)
		{"a:b", "integer frame indexes"},          // non-integer
		{"0:99", "out of range"},                  // stop beyond frame count
		{"0:2:0", "stride must be positive"},      // zero stride
		{"3:1", "greater than or equal"},          // stop < start
		{"0:1:2:3", "expected START:STOP:STRIDE"}, // too many parts
	}
	for _, tc := range bad {
		_, _, _, err := parseFrameRange(tc.value, ref)
		if err == nil {
			t.Errorf("parseFrameRange(%q) = nil, want error", tc.value)
			continue
		}
		if code := ErrorCode(err); code != "validation_failed" {
			t.Errorf("parseFrameRange(%q) code = %q, want validation_failed", tc.value, code)
		}
		if !strings.Contains(err.Error(), tc.wantSub) {
			t.Errorf("parseFrameRange(%q) msg = %q, want contains %q", tc.value, err.Error(), tc.wantSub)
		}
	}

	if _, _, stride, err := parseFrameRange("0:2", ref); err != nil || stride != 1 {
		t.Fatalf("valid range failed: stride=%d err=%v", stride, err)
	}
	// Empty range is allowed (means "whole trajectory").
	if _, _, _, err := parseFrameRange("", ref); err != nil {
		t.Fatalf("empty range should be allowed: %v", err)
	}
}
