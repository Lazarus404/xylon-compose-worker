package retention_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Lazarus404/xylon-compose-worker/internal/retention"
)

func TestSweepDeletesOldFiles(t *testing.T) {
	root := t.TempDir()
	oldPath := filepath.Join(root, "old.mjr")
	newPath := filepath.Join(root, "new.mjr")
	if err := os.WriteFile(oldPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newPath, []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	n, err := retention.Sweep([]string{root}, 24*time.Hour, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("deleted %d", n)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatal("old should be gone")
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatal("new should remain")
	}
}
