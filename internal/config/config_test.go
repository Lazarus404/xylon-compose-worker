package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Lazarus404/xylon-compose-worker/internal/config"
)

func TestRecordRetentionHoursEnvOverridesConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cfg.json")
	if err := os.WriteFile(path, []byte(`{
		"listen_addr":"127.0.0.1:8094",
		"webhook_secret":"s",
		"retention_hours": 48
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RECORD_RETENTION_HOURS", "24")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RetentionHours != 24 {
		t.Fatalf("got %d", cfg.RetentionHours)
	}
}

func TestDefaultRetention120(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cfg.json")
	if err := os.WriteFile(path, []byte(`{"listen_addr":"127.0.0.1:8094","webhook_secret":"s"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RECORD_RETENTION_HOURS", "")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RetentionHours != 120 {
		t.Fatalf("got %d", cfg.RetentionHours)
	}
}
