package config

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

// Config is the compose worker process configuration.
type Config struct {
	ListenAddr           string `json:"listen_addr"`
	WebhookSecret        string `json:"webhook_secret"`
	StemRoot             string `json:"stem_root"`
	OutRoot              string `json:"out_root"`
	IndexPath            string `json:"index_path"`
	RetentionHours       int    `json:"retention_hours"`
	JanusPPRecPath       string `json:"janus_pp_rec_path"`
	FFmpegPath           string `json:"ffmpeg_path"`
	InterfaceWebhookURL  string `json:"interface_webhook_url"`
	RetentionIntervalSec int    `json:"retention_interval_sec"`
}

// Load reads JSON config from path and applies defaults.
func Load(path string) (Config, error) {
	cfg := Config{
		ListenAddr:           "127.0.0.1:8094",
		StemRoot:             "/tmp/xylon-record",
		OutRoot:              "/tmp/xylon-compose",
		IndexPath:            "/tmp/xylon-compose/session_index.json",
		RetentionHours:       120,
		JanusPPRecPath:       "janus-pp-rec",
		FFmpegPath:           "ffmpeg",
		RetentionIntervalSec: 60,
	}
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return Config{}, fmt.Errorf("read config: %w", err)
		}
		if err := json.Unmarshal(data, &cfg); err != nil {
			return Config{}, fmt.Errorf("parse config: %w", err)
		}
	}
	if v := strings.TrimSpace(os.Getenv("RECORD_RETENTION_HOURS")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return Config{}, fmt.Errorf("RECORD_RETENTION_HOURS must be a positive int")
		}
		cfg.RetentionHours = n
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = "127.0.0.1:8094"
	}
	if cfg.RetentionHours < 1 {
		cfg.RetentionHours = 120
	}
	if cfg.RetentionIntervalSec < 1 {
		cfg.RetentionIntervalSec = 60
	}
	if cfg.JanusPPRecPath == "" {
		cfg.JanusPPRecPath = "janus-pp-rec"
	}
	if cfg.FFmpegPath == "" {
		cfg.FFmpegPath = "ffmpeg"
	}
	if cfg.OutRoot == "" {
		cfg.OutRoot = "/tmp/xylon-compose"
	}
	if cfg.IndexPath == "" {
		cfg.IndexPath = cfg.OutRoot + "/session_index.json"
	}
	if err := requireLoopbackListen(cfg.ListenAddr); err != nil {
		return Config{}, err
	}
	if cfg.WebhookSecret == "" {
		return Config{}, fmt.Errorf("webhook_secret required")
	}
	return cfg, nil
}

func requireLoopbackListen(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("listen_addr: %w", err)
	}
	host = strings.TrimSpace(host)
	switch strings.ToLower(host) {
	case "", "0.0.0.0", "::", "[::]":
		return fmt.Errorf("listen_addr must be loopback (got %q)", addr)
	case "127.0.0.1", "localhost", "::1":
		return nil
	}
	ip := net.ParseIP(host)
	if ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("listen_addr must be loopback (got %q)", addr)
}
