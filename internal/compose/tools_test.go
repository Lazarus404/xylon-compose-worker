package compose_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Lazarus404/xylon-compose-worker/internal/compose"
)

// Always in the default suite. Skips only when ffmpeg is not on PATH.
func TestSpliceClosedGapWithFFmpeg(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not on PATH")
	}

	dir := t.TempDir()
	prior := filepath.Join(dir, "prior.mp4")
	seg := filepath.Join(dir, "seg.mp4")
	out := filepath.Join(dir, "session.mp4")

	makeShortMP4(t, prior, "0.4")
	makeShortMP4(t, seg, "0.4")

	if err := compose.Splice("ffmpeg", prior, seg, out, 500); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() < 100 {
		t.Fatalf("session mp4 too small: %d", info.Size())
	}

	out2 := filepath.Join(dir, "session2.mp4")
	if err := compose.Splice("ffmpeg", prior, seg, out2, 0); err != nil {
		t.Fatal(err)
	}
}

func makeShortMP4(t *testing.T, path, sec string) {
	t.Helper()
	cmd := exec.Command("ffmpeg", "-y",
		"-f", "lavfi", "-i", "color=c=blue:s=320x240:d="+sec,
		"-f", "lavfi", "-i", "sine=frequency=440:duration="+sec,
		"-c:v", "libx264", "-c:a", "aac", "-shortest", path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("make mp4: %v (%s)", err, string(out))
	}
}

// Always in the default suite. Skips only when janus-pp-rec or ffmpeg missing.
func TestShellRemuxSkipOrRun(t *testing.T) {
	if _, err := exec.LookPath("janus-pp-rec"); err != nil {
		t.Skip("janus-pp-rec not on PATH")
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not on PATH")
	}
	// Without a real .mjr from XylonRecord, remux cannot succeed; assert the
	// tool path resolves and ShellRemuxer reports a clear error on empty stems.
	r := compose.ShellRemuxer{Tools: compose.Tools{JanusPPRec: "janus-pp-rec", FFmpeg: "ffmpeg"}}
	err := r.Remux(compose.Event{RecordingID: "r", Artifacts: nil}, filepath.Join(t.TempDir(), "o.mp4"))
	if err == nil {
		t.Fatal("expected error for empty artifacts")
	}
}
