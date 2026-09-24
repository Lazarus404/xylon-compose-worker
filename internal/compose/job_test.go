package compose_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Lazarus404/xylon-compose-worker/internal/compose"
)

func TestGapMs(t *testing.T) {
	if compose.GapMs(200, 100) != 100 {
		t.Fatal("expected 100")
	}
	if compose.GapMs(100, 200) != 0 {
		t.Fatal("expected 0")
	}
}

func TestWriteFailedSidecarLeavesDir(t *testing.T) {
	dir := t.TempDir()
	stem := filepath.Join(dir, "a.mjr")
	if err := os.WriteFile(stem, []byte("mjr"), 0o644); err != nil {
		t.Fatal(err)
	}
	ev := compose.Event{RecordingID: "r1", Type: "recording.completed"}
	if err := compose.WriteFailedSidecar(dir, ev, os.ErrInvalid); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stem); err != nil {
		t.Fatal("stem must remain")
	}
	if _, err := os.Stat(filepath.Join(dir, "compose_failed.json")); err != nil {
		t.Fatal("sidecar missing")
	}
}

func TestParseEvent(t *testing.T) {
	body := []byte(`{"type":"recording.completed","recording_id":"r1","started_at":1,"ended_at":2,"artifacts":[{"path_or_key":"/a.mjr"}]}`)
	ev, err := compose.ParseEvent(body)
	if err != nil {
		t.Fatal(err)
	}
	if ev.RecordingID != "r1" || len(ev.Artifacts) != 1 {
		t.Fatalf("%+v", ev)
	}
}

func TestStemArtifactsSkipsLayout(t *testing.T) {
	arts := []compose.Artifact{
		{Kind: "audio", PathOrKey: "/tmp/a.mjr"},
		{Kind: "layout", PathOrKey: "/tmp/layout.mp4"},
		{Kind: "video", PathOrKey: "/tmp/v.rtp"},
		{Kind: "video", PathOrKey: "/tmp/v.mjr"},
	}
	stems := compose.StemArtifacts(arts)
	if len(stems) != 2 {
		t.Fatalf("want 2 stems, got %d %+v", len(stems), stems)
	}
	for _, s := range stems {
		if !compose.IsMJRStem(s) {
			t.Fatalf("not stem: %+v", s)
		}
	}
}

func TestCopyLayoutMP4(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src_layout.mp4")
	if err := os.WriteFile(src, []byte("mp4bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out")
	arts := []compose.Artifact{
		{Kind: "audio", PathOrKey: filepath.Join(dir, "a.mjr")},
		{Kind: "layout", PathOrKey: src},
	}
	dst, err := compose.CopyLayoutMP4(arts, out)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(out, "layout.mp4")
	if dst != want {
		t.Fatalf("dst=%s want=%s", dst, want)
	}
	got, err := os.ReadFile(want)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "mp4bytes" {
		t.Fatalf("content %q", got)
	}
}
