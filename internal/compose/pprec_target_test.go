package compose_test

import (
	"testing"

	"github.com/Lazarus404/xylon-compose-worker/internal/compose"
)

func TestPPRecTargetExtensions(t *testing.T) {
	cases := []struct {
		codec, kind, wantSuffix string
	}{
		{"vp8", "video", ".webm"},
		{"VP9", "video", ".webm"},
		{"opus", "audio", ".opus"},
		{"h264", "video", ".h264"},
		{"", "audio", ".opus"},
		{"", "video", ".webm"},
	}
	for _, tc := range cases {
		got := compose.PPRecTargetForTest("stem", compose.Artifact{Codec: tc.codec, Kind: tc.kind})
		if !hasSuffix(got, tc.wantSuffix) {
			t.Fatalf("codec=%q kind=%q got %q want suffix %q", tc.codec, tc.kind, got, tc.wantSuffix)
		}
	}
}

func hasSuffix(s, suf string) bool {
	return len(s) >= len(suf) && s[len(s)-len(suf):] == suf
}
