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

const (
	StatusRunning   = "RUNNING"
	StatusCompleted = "COMPLETED"
	StatusFailed    = "FAILED"
	StatusCancelled = "CANCELLED"
	StatusTimeout   = "TIMEOUT"
)

var (
	ErrNotFound        = errors.New("not found")
	ErrAlreadyTerminal = errors.New("agent run already terminal")
)

type AgentRun struct {
	ID               string          `json:"id"`
	WorkContextID    string          `json:"work_context_id"`
	WorkspacePath    string          `json:"workspace_path"`
	Prompt           string          `json:"prompt"`
	Provider         string          `json:"provider"`
	HarnessNativeID  string          `json:"harness_native_id"`
	Status           string          `json:"status"`
	RawSessionRef    string          `json:"raw_session_ref"`
	ProviderMetadata json.RawMessage `json:"provider_metadata"`
	FrameCount       int             `json:"frame_count"`
	StartedAt        string          `json:"started_at"`
	EndedAt          string          `json:"ended_at"`
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
	byID  map[string]*AgentRun
	byCtx map[string][]string
}

type completedData struct {
	ID               string          `json:"id"`
	Status           string          `json:"status"`
	RawSessionRef    string          `json:"raw_session_ref"`
	FrameCount       int             `json:"frame_count"`
	ProviderMetadata json.RawMessage `json:"provider_metadata"`
	EndedAt          string          `json:"ended_at"`
}

type cancelledData struct {
	ID      string `json:"id"`
	EndedAt string `json:"ended_at"`
}

func Load(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{path: filepath.Join(dir, "agent-log.jsonl"), byID: map[string]*AgentRun{}, byCtx: map[string][]string{}}
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
			return nil, fmt.Errorf("agent log line %d: %w", i+1, err)
		}
		if err := s.apply(rec); err != nil {
			return nil, fmt.Errorf("agent log line %d: %w", i+1, err)
		}
		if rec.Seq > s.seq {
			s.seq = rec.Seq
		}
	}
	return s, nil
}

func (s *Store) apply(rec logRecord) error {
	switch rec.Op {
	case "agent.run.started":
		var ar AgentRun
		if err := json.Unmarshal(rec.Data, &ar); err != nil {
			return err
		}
		cp := cloneRun(ar)
		s.byID[ar.ID] = &cp
		s.byCtx[ar.WorkContextID] = append(s.byCtx[ar.WorkContextID], ar.ID)
	case "agent.run.completed":
		var d completedData
		if err := json.Unmarshal(rec.Data, &d); err != nil {
			return err
		}
		ar, ok := s.byID[d.ID]
		if !ok {
			return ErrNotFound
		}
		ar.Status = d.Status
		ar.RawSessionRef = d.RawSessionRef
		ar.FrameCount = d.FrameCount
		ar.ProviderMetadata = append(json.RawMessage(nil), d.ProviderMetadata...)
		ar.EndedAt = d.EndedAt
	case "agent.run.cancelled":
		var d cancelledData
		if err := json.Unmarshal(rec.Data, &d); err != nil {
			return err
		}
		ar, ok := s.byID[d.ID]
		if !ok {
			return ErrNotFound
		}
		ar.Status = StatusCancelled
		ar.EndedAt = d.EndedAt
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

func (s *Store) RecordStarted(ar AgentRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.append("agent.run.started", ar)
}

func (s *Store) RecordCompleted(id, status, rawRef string, frameCount int, meta json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ar, ok := s.byID[id]
	if !ok {
		return ErrNotFound
	}
	if ar.Status != StatusRunning {
		return ErrAlreadyTerminal
	}
	return s.append("agent.run.completed", completedData{ID: id, Status: status, RawSessionRef: rawRef, FrameCount: frameCount, ProviderMetadata: meta, EndedAt: time.Now().UTC().Format(time.RFC3339Nano)})
}

func (s *Store) RecordCancelled(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ar, ok := s.byID[id]
	if !ok {
		return ErrNotFound
	}
	if ar.Status != StatusRunning {
		return ErrAlreadyTerminal
	}
	return s.append("agent.run.cancelled", cancelledData{ID: id, EndedAt: time.Now().UTC().Format(time.RFC3339Nano)})
}

func (s *Store) GetByID(id string) (AgentRun, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ar, ok := s.byID[id]
	if !ok {
		return AgentRun{}, false
	}
	return cloneRun(*ar), true
}

func (s *Store) QueryByContext(wcID string) []AgentRun {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := s.byCtx[wcID]
	out := make([]AgentRun, 0, len(ids))
	for _, id := range ids {
		if ar, ok := s.byID[id]; ok {
			out = append(out, cloneRun(*ar))
		}
	}
	return out
}

func cloneRun(ar AgentRun) AgentRun {
	ar.ProviderMetadata = append(json.RawMessage(nil), ar.ProviderMetadata...)
	return ar
}
