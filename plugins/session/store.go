package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var ErrNotFound = errors.New("session record not found")

type SessionRecord struct {
	ID                 string                `json:"id"`
	WorkContextID      string                `json:"work_context_id"`
	AgentRunID         string                `json:"agent_run_id"`
	ArchiveRef         string                `json:"archive_ref"`
	ArchiveHash        string                `json:"archive_hash"`
	EventSelection     SessionEventSelection `json:"event_selection"`
	RecoveryCheckpoint RecoveryCheckpoint    `json:"recovery_checkpoint"`
	SealedAt           string                `json:"sealed_at"`
}
type logRecord struct {
	Seq  int64           `json:"seq"`
	TS   string          `json:"ts"`
	Op   string          `json:"op"`
	Data json.RawMessage `json:"data"`
}
type Store struct {
	mu    sync.Mutex
	path  string
	seq   int64
	byID  map[string]*SessionRecord
	byCtx map[string][]string
}

func Load(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{path: filepath.Join(dir, "session-log.jsonl"), byID: map[string]*SessionRecord{}, byCtx: map[string][]string{}}
	b, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	lines := bytes.Split(b, []byte{'\n'})
	last := -1
	for i, l := range lines {
		if len(bytes.TrimSpace(l)) > 0 {
			last = i
		}
	}
	for i, l := range lines {
		if len(bytes.TrimSpace(l)) == 0 {
			continue
		}
		var rec logRecord
		if err := json.Unmarshal(l, &rec); err != nil {
			if i == last && !bytes.HasSuffix(b, []byte{'\n'}) {
				break
			}
			return nil, fmt.Errorf("session log line %d: %w", i+1, err)
		}
		if err := s.apply(rec); err != nil {
			return nil, fmt.Errorf("session log line %d: %w", i+1, err)
		}
		if rec.Seq > s.seq {
			s.seq = rec.Seq
		}
	}
	return s, nil
}
func (s *Store) apply(rec logRecord) error {
	if rec.Op != "session.sealed" {
		return fmt.Errorf("unknown op %q", rec.Op)
	}
	var sr SessionRecord
	if err := json.Unmarshal(rec.Data, &sr); err != nil {
		return err
	}
	c := cloneSession(sr)
	s.byID[sr.ID] = &c
	s.byCtx[sr.WorkContextID] = append(s.byCtx[sr.WorkContextID], sr.ID)
	return nil
}
func (s *Store) append(op string, data any) error {
	p, err := json.Marshal(data)
	if err != nil {
		return err
	}
	rec := logRecord{Seq: s.seq + 1, TS: time.Now().UTC().Format(time.RFC3339Nano), Op: op, Data: p}
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, werr := f.Write(append(line, '\n'))
	if werr == nil {
		werr = f.Sync()
	}
	cerr := f.Close()
	if werr != nil {
		return werr
	}
	if cerr != nil {
		return cerr
	}
	if err := s.apply(rec); err != nil {
		return err
	}
	s.seq = rec.Seq
	return nil
}
func (s *Store) Record(sr SessionRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.append("session.sealed", sr)
}
func (s *Store) GetByID(id string) (SessionRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sr, ok := s.byID[id]
	if !ok {
		return SessionRecord{}, false
	}
	return cloneSession(*sr), true
}
func (s *Store) QueryByContext(w string) []SessionRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]SessionRecord, 0, len(s.byCtx[w]))
	for _, id := range s.byCtx[w] {
		if sr, ok := s.byID[id]; ok {
			out = append(out, cloneSession(*sr))
		}
	}
	return out
}
func cloneSelection(s SessionEventSelection) SessionEventSelection {
	s.EventIDs = append([]string(nil), s.EventIDs...)
	s.EventSHA256s = append([]string(nil), s.EventSHA256s...)
	return s
}
func cloneCheckpoint(c RecoveryCheckpoint) RecoveryCheckpoint {
	c.UntrackedManifest = append([]string(nil), c.UntrackedManifest...)
	c.CanonicalEventSelection = cloneSelection(c.CanonicalEventSelection)
	return c
}
func cloneSession(s SessionRecord) SessionRecord {
	s.EventSelection = cloneSelection(s.EventSelection)
	s.RecoveryCheckpoint = cloneCheckpoint(s.RecoveryCheckpoint)
	return s
}
