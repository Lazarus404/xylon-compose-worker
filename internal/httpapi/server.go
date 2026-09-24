package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/Lazarus404/xylon-compose-worker/internal/compose"
	"github.com/Lazarus404/xylon-compose-worker/internal/config"
	"github.com/Lazarus404/xylon-compose-worker/internal/index"
	"github.com/Lazarus404/xylon-compose-worker/internal/retention"
	"github.com/Lazarus404/xylon-compose-worker/internal/sign"
)

// Server is the compose webhook HTTP API.
type Server struct {
	Cfg     config.Config
	Index   *index.Store
	Remuxer compose.Remuxer
	Client  *http.Client
}

// Handler returns the root mux.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
	mux.HandleFunc("POST /v1/webhooks/xylon", s.handleWebhook)
	return mux
}

// StartRetentionLoop sweeps stem and out roots on an interval.
func (s *Server) StartRetentionLoop(stop <-chan struct{}) {
	interval := time.Duration(s.Cfg.RetentionIntervalSec) * time.Second
	maxAge := time.Duration(s.Cfg.RetentionHours) * time.Hour
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				_, _ = retention.Sweep([]string{s.Cfg.StemRoot, s.Cfg.OutRoot}, maxAge, time.Now())
			}
		}
	}()
}

func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "read_body"})
		return
	}
	sig := r.Header.Get(sign.Header)
	if !sign.Valid(body, s.Cfg.WebhookSecret, sig) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "bad_signature"})
		return
	}

	// Accept first so Xylon delivery succeeds; work is async.
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})

	bodyCopy := append([]byte(nil), body...)
	sigCopy := sig
	go s.process(bodyCopy, sigCopy)
}

func (s *Server) process(body []byte, sig string) {
	if s.Cfg.InterfaceWebhookURL != "" {
		s.forward(body, sig)
	}

	ev, err := compose.ParseEvent(body)
	if err != nil {
		log.Printf("compose parse: %v", err)
		return
	}
	if ev.Type != "recording.completed" {
		log.Printf("compose skip type=%s", ev.Type)
		return
	}
	if len(ev.Artifacts) == 0 {
		log.Printf("compose no artifacts recording_id=%s", ev.RecordingID)
		_ = s.Index.Put(index.Entry{
			RecordingID: ev.RecordingID,
			TenantID:    ev.TenantID,
			SessionID:   ev.SessionID,
			EndedAt:     ev.EndedAt,
		})
		return
	}

	segDir := filepath.Join(s.Cfg.OutRoot, ev.TenantID, ev.SessionID, ev.RecordingID)
	_ = os.MkdirAll(segDir, 0o755)
	segMP4 := filepath.Join(segDir, "segment.mp4")
	sessionMP4 := filepath.Join(s.Cfg.OutRoot, ev.TenantID, ev.SessionID, "session.mp4")

	remuxer := s.Remuxer
	if remuxer == nil {
		remuxer = compose.ShellRemuxer{Tools: compose.Tools{JanusPPRec: s.Cfg.JanusPPRecPath, FFmpeg: s.Cfg.FFmpegPath}}
	}

	if err := remuxer.Remux(ev, segMP4); err != nil {
		log.Printf("compose remux failed recording_id=%s: %v", ev.RecordingID, err)
		_ = compose.WriteFailedSidecar(segDir, ev, err)
		// Still copy live layout MP4 when present.
		if _, copyErr := compose.CopyLayoutMP4(ev.Artifacts, segDir); copyErr != nil {
			log.Printf("compose layout copy: %v", copyErr)
		}
		_ = s.Index.Put(index.Entry{
			RecordingID: ev.RecordingID,
			TenantID:    ev.TenantID,
			SessionID:   ev.SessionID,
			EndedAt:     ev.EndedAt,
		})
		return
	}

	if _, copyErr := compose.CopyLayoutMP4(ev.Artifacts, segDir); copyErr != nil {
		log.Printf("compose layout copy: %v", copyErr)
	}

	var gap int64
	var priorSession string
	if ev.PrecededBy != nil && *ev.PrecededBy != "" {
		if prev, ok := s.Index.Get(*ev.PrecededBy); ok {
			gap = compose.GapMs(ev.StartedAt, prev.EndedAt)
			priorSession = prev.SessionMP4
			if priorSession == "" {
				priorSession = prev.SegmentMP4
			}
		}
	}

	if err := compose.Splice(s.Cfg.FFmpegPath, priorSession, segMP4, sessionMP4, gap); err != nil {
		log.Printf("compose splice failed recording_id=%s: %v (keeping segment)", ev.RecordingID, err)
		// keep segment; copy as session if no prior
		if priorSession == "" {
			_ = copyFile(segMP4, sessionMP4)
		}
	}

	_ = s.Index.Put(index.Entry{
		RecordingID: ev.RecordingID,
		TenantID:    ev.TenantID,
		SessionID:   ev.SessionID,
		EndedAt:     ev.EndedAt,
		SegmentMP4:  segMP4,
		SessionMP4:  sessionMP4,
	})
}

func (s *Server) forward(body []byte, sig string) {
	client := s.Client
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequest(http.MethodPost, s.Cfg.InterfaceWebhookURL, bytes.NewReader(body))
	if err != nil {
		log.Printf("compose forward build: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(sign.Header, sig)
	res, err := client.Do(req)
	if err != nil {
		log.Printf("compose forward: %v", err)
		return
	}
	_ = res.Body.Close()
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	_ = os.MkdirAll(filepath.Dir(dst), 0o755)
	return os.WriteFile(dst, data, 0o644)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
