package httpapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Lazarus404/xylon-compose-worker/internal/compose"
	"github.com/Lazarus404/xylon-compose-worker/internal/config"
	"github.com/Lazarus404/xylon-compose-worker/internal/httpapi"
	"github.com/Lazarus404/xylon-compose-worker/internal/index"
	"github.com/Lazarus404/xylon-compose-worker/internal/sign"
)

type fakeRemux struct {
	mu   sync.Mutex
	path string
	err  error
}

func (f *fakeRemux) Remux(ev compose.Event, outMP4 string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	_ = os.MkdirAll(filepath.Dir(outMP4), 0o755)
	return os.WriteFile(outMP4, []byte("mp4-"+ev.RecordingID), 0o644)
}

func TestWebhookRejectsBadSignature(t *testing.T) {
	out := t.TempDir()
	idx, err := index.Open(filepath.Join(out, "idx.json"))
	if err != nil {
		t.Fatal(err)
	}
	srv := &httpapi.Server{
		Cfg: config.Config{
			WebhookSecret: "secret",
			OutRoot:       out,
			StemRoot:      out,
			FFmpegPath:    "ffmpeg",
		},
		Index:   idx,
		Remuxer: &fakeRemux{},
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/webhooks/xylon", strings.NewReader(`{}`))
	req.Header.Set(sign.Header, "sha256=dead")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d", res.StatusCode)
	}
}

func TestWebhookRemuxSuccessAndFailureKeepsStems(t *testing.T) {
	out := t.TempDir()
	stemDir := t.TempDir()
	stem := filepath.Join(stemDir, "a.mjr")
	if err := os.WriteFile(stem, []byte("mjr"), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := index.Open(filepath.Join(out, "idx.json"))
	if err != nil {
		t.Fatal(err)
	}

	var forwarded []byte
	iface := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer iface.Close()

	remux := &fakeRemux{}
	srv := &httpapi.Server{
		Cfg: config.Config{
			WebhookSecret:       "secret",
			OutRoot:             out,
			StemRoot:            stemDir,
			FFmpegPath:          "ffmpeg",
			InterfaceWebhookURL: iface.URL,
		},
		Index:   idx,
		Remuxer: remux,
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	body := map[string]any{
		"type":          "recording.completed",
		"tenant_id":     "t1",
		"session_id":    "s1",
		"recording_id":  "r1",
		"sequence":      1,
		"started_at":    1000,
		"ended_at":      2000,
		"artifacts":     []map[string]any{{"path_or_key": stem, "kind": "audio"}},
	}
	raw, _ := json.Marshal(body)
	sig := sign.Sign(raw, "secret")
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/webhooks/xylon", strings.NewReader(string(raw)))
	req.Header.Set(sign.Header, sig)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("status %d", res.StatusCode)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(out, "t1", "s1", "r1", "segment.mp4")); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := os.Stat(filepath.Join(out, "t1", "s1", "r1", "segment.mp4")); err != nil {
		t.Fatal("segment missing")
	}
	if string(forwarded) != string(raw) {
		t.Fatalf("forward body: %s", forwarded)
	}

	// failure path
	remux.err = os.ErrInvalid
	body["recording_id"] = "r2"
	raw, _ = json.Marshal(body)
	sig = sign.Sign(raw, "secret")
	req, _ = http.NewRequest(http.MethodPost, ts.URL+"/v1/webhooks/xylon", strings.NewReader(string(raw)))
	req.Header.Set(sign.Header, sig)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(out, "t1", "s1", "r2", "compose_failed.json")); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := os.Stat(stem); err != nil {
		t.Fatal("stem must remain after remux failure")
	}
	if _, err := os.Stat(filepath.Join(out, "t1", "s1", "r2", "compose_failed.json")); err != nil {
		t.Fatal("sidecar missing")
	}
}

func TestGapDurationViaIndex(t *testing.T) {
	if compose.GapMs(5000, 2000) != 3000 {
		t.Fatal("gap")
	}
}
