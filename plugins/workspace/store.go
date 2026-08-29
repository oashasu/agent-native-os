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

type Status string

const (
	StatusAllocated Status = "ALLOCATED"
	StatusReleased  Status = "RELEASED"
)

var ErrNotFound = errors.New("workspace not found")

type WorkspaceRef struct {
	ID            string `json:"id"`
	WorkContextID string `json:"work_context_id"`
	Repo          string `json:"repo"`
	Path          string `json:"path"`
	Branch        string `json:"branch"`
	BaseCommit    string `json:"base_commit"`
	Status        Status `json:"status"`
	ReleasePolicy string `json:"release_policy"`
	AllocatedAt   string `json:"allocated_at"`
	ReleasedAt    string `json:"released_at"`
}

type logRecord struct {
	Seq  int64           `json:"seq"`
	TS   string          `json:"ts"`
	Op   string          `json:"op"`
	Data json.RawMessage `json:"data"`
}

type Store struct {
	mu   sync.Mutex
	path string
	seq  int64
	byID map[string]*WorkspaceRef
}

type releasedData struct {
	WorkspaceID string `json:"workspace_id"`
	Policy      string `json:"policy"`
	ReleasedAt  string `json:"released_at"`
}

func Load(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{path: filepath.Join(dir, "workspace-log.jsonl"), byID: map[string]*WorkspaceRef{}}
	b, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	lines := bytes.Split(b, []byte{'\n'})
	lastNonEmpty := -1
	for i, line := range lines {
		if len(bytes.TrimSpace(line)) != 0 {
			lastNonEmpty = i
		}
	}
	for i, line := range lines {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var rec logRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			if i == lastNonEmpty && !bytes.HasSuffix(b, []byte{'\n'}) {
				break
			}
			return nil, fmt.Errorf("workspace log line %d: %w", i+1, err)
		}
		if err := s.apply(rec); err != nil {
			return nil, fmt.Errorf("workspace log line %d: %w", i+1, err)
		}
		if rec.Seq > s.seq {
			s.seq = rec.Seq
		}
	}
	return s, nil
}

func (s *Store) apply(rec logRecord) error {
	switch rec.Op {
	case "workspace.allocated":
		var ref WorkspaceRef
		if err := json.Unmarshal(rec.Data, &ref); err != nil {
			return err
		}
		copyRef := ref
		s.byID[ref.ID] = &copyRef
	case "workspace.released":
		var d releasedData
		if err := json.Unmarshal(rec.Data, &d); err != nil {
			return err
		}
		ref, ok := s.byID[d.WorkspaceID]
		if !ok {
			return ErrNotFound
		}
		ref.Status = StatusReleased
		ref.ReleasePolicy = d.Policy
		ref.ReleasedAt = d.ReleasedAt
	default:
		return fmt.Errorf("unknown op %q", rec.Op)
	}
	return nil
}

func (s *Store) append(op string, data any) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	rec := logRecord{Seq: s.seq + 1, TS: time.Now().UTC().Format(time.RFC3339Nano), Op: op, Data: payload}
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

func (s *Store) RecordAllocated(ref WorkspaceRef) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.append("workspace.allocated", ref)
}

func (s *Store) RecordReleased(id, policy string) (WorkspaceRef, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byID[id]; !ok {
		return WorkspaceRef{}, ErrNotFound
	}
	if err := s.append("workspace.released", releasedData{WorkspaceID: id, Policy: policy, ReleasedAt: time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
		return WorkspaceRef{}, err
	}
	return cloneRef(s.byID[id]), nil
}

func (s *Store) GetByID(id string) (WorkspaceRef, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ref, ok := s.byID[id]
	if !ok {
		return WorkspaceRef{}, false
	}
	return cloneRef(ref), true
}

func (s *Store) GetActiveByContext(wcID string) (WorkspaceRef, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var best *WorkspaceRef
	for _, ref := range s.byID {
		if ref.WorkContextID != wcID || ref.Status != StatusAllocated {
			continue
		}
		if best == nil || ref.AllocatedAt > best.AllocatedAt {
			best = ref
		}
	}
	if best == nil {
		return WorkspaceRef{}, false
	}
	return cloneRef(best), true
}

func cloneRef(ref *WorkspaceRef) WorkspaceRef {
	if ref == nil {
		return WorkspaceRef{}
	}
	return *ref
}
