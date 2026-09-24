package index_test

import (
	"path/filepath"
	"testing"

	"github.com/Lazarus404/xylon-compose-worker/internal/index"
)

func TestPutGet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "idx.json")
	s, err := index.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	e := index.Entry{RecordingID: "r1", TenantID: "t", SessionID: "s", EndedAt: 100, SegmentMP4: "/a.mp4"}
	if err := s.Put(e); err != nil {
		t.Fatal(err)
	}
	got, ok := s.Get("r1")
	if !ok || got.EndedAt != 100 {
		t.Fatalf("got %+v ok=%v", got, ok)
	}
	s2, err := index.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok = s2.Get("r1")
	if !ok || got.SegmentMP4 != "/a.mp4" {
		t.Fatalf("reload %+v", got)
	}
}
