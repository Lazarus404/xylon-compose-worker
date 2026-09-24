package index

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Entry records one finished segment for splice lookups.
type Entry struct {
	RecordingID string `json:"recording_id"`
	TenantID    string `json:"tenant_id"`
	SessionID   string `json:"session_id"`
	EndedAt     int64  `json:"ended_at"`
	SegmentMP4  string `json:"segment_mp4"`
	SessionMP4  string `json:"session_mp4"`
}

// Store is a small JSON file index keyed by recording_id.
type Store struct {
	path string
	mu   sync.Mutex
	byID map[string]Entry
}

// Open loads or creates an index at path.
func Open(path string) (*Store, error) {
	s := &Store{path: path, byID: map[string]Entry{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return nil, err
			}
			return s, s.flushLocked()
		}
		return nil, err
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &s.byID); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// Put upserts an entry and persists.
func (s *Store) Put(e Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byID[e.RecordingID] = e
	return s.flushLocked()
}

// Get returns an entry by recording id.
func (s *Store) Get(recordingID string) (Entry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.byID[recordingID]
	return e, ok
}

func (s *Store) flushLocked() error {
	data, err := json.MarshalIndent(s.byID, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
