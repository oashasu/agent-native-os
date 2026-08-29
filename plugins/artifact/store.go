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

var ErrNotFound = errors.New("artifact not found")

type Artifact struct {
	ID            string      `json:"id"`
	WorkContextID string      `json:"work_context_id"`
	Kind          string      `json:"kind"`
	BlobURI       string      `json:"blob_uri"`
	Summary       DiffSummary `json:"summary"`
	CreatedAt     string      `json:"created_at"`
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
	byID  map[string]*Artifact
	byCtx map[string][]string
}

func Load(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{path: filepath.Join(dir, "artifact-log.jsonl"), byID: map[string]*Artifact{}, byCtx: map[string][]string{}}
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
			return nil, fmt.Errorf("artifact log line %d: %w", i+1, err)
		}
		if err := s.apply(rec); err != nil {
			return nil, fmt.Errorf("artifact log line %d: %w", i+1, err)
		}
		if rec.Seq > s.seq {
			s.seq = rec.Seq
		}
	}
	return s, nil
}

func (s *Store) apply(rec logRecord) error {
	switch rec.Op {
	case "artifact.recorded":
		var a Artifact
		if err := json.Unmarshal(rec.Data, &a); err != nil {
			return err
		}
		copyA := cloneArtifact(a)
		s.byID[a.ID] = &copyA
		s.byCtx[a.WorkContextID] = append(s.byCtx[a.WorkContextID], a.ID)
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

func (s *Store) Record(a Artifact) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.append("artifact.recorded", a)
}

func (s *Store) GetByID(id string) (Artifact, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.byID[id]
	if !ok {
		return Artifact{}, false
	}
	return cloneArtifact(*a), true
}

func (s *Store) QueryByContext(wcID string) []Artifact {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := s.byCtx[wcID]
	out := make([]Artifact, 0, len(ids))
	for _, id := range ids {
		if a, ok := s.byID[id]; ok {
			out = append(out, cloneArtifact(*a))
		}
	}
	return out
}

func cloneArtifact(a Artifact) Artifact {
	a.Summary.Files = append([]string(nil), a.Summary.Files...)
	return a
}
