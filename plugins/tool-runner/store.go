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

var ErrNotFound = errors.New("tool run not found")

type ToolRun struct {
	ID            string   `json:"id"`
	WorkContextID string   `json:"work_context_id"`
	WorkspacePath string   `json:"workspace_path"`
	Label         string   `json:"label"`
	Command       []string `json:"command"`
	Cwd           string   `json:"cwd"`
	EnvAllowlist  []string `json:"env_allowlist"`
	TimeoutMS     int      `json:"timeout_ms"`
	ExitCode      int      `json:"exit_code"`
	Outcome       string   `json:"outcome"`
	StdoutURI     string   `json:"stdout_uri"`
	StderrURI     string   `json:"stderr_uri"`
	Fingerprint   string   `json:"fingerprint"`
	StartedAt     string   `json:"started_at"`
	EndedAt       string   `json:"ended_at"`
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
	byID  map[string]*ToolRun
	byCtx map[string][]string
}

func Load(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{path: filepath.Join(dir, "tool-run-log.jsonl"), byID: map[string]*ToolRun{}, byCtx: map[string][]string{}}
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
			return nil, fmt.Errorf("tool run log line %d: %w", i+1, err)
		}
		if err := s.apply(rec); err != nil {
			return nil, fmt.Errorf("tool run log line %d: %w", i+1, err)
		}
		if rec.Seq > s.seq {
			s.seq = rec.Seq
		}
	}
	return s, nil
}

func (s *Store) apply(rec logRecord) error {
	switch rec.Op {
	case "tool.run.recorded":
		var tr ToolRun
		if err := json.Unmarshal(rec.Data, &tr); err != nil {
			return err
		}
		copyTR := cloneToolRun(tr)
		s.byID[tr.ID] = &copyTR
		s.byCtx[tr.WorkContextID] = append(s.byCtx[tr.WorkContextID], tr.ID)
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

func (s *Store) Record(tr ToolRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.append("tool.run.recorded", tr)
}

func (s *Store) GetByID(id string) (ToolRun, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tr, ok := s.byID[id]
	if !ok {
		return ToolRun{}, false
	}
	return cloneToolRun(*tr), true
}

func (s *Store) QueryByContext(wcID string) []ToolRun {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := s.byCtx[wcID]
	out := make([]ToolRun, 0, len(ids))
	for _, id := range ids {
		if tr, ok := s.byID[id]; ok {
			out = append(out, cloneToolRun(*tr))
		}
	}
	return out
}

func cloneToolRun(tr ToolRun) ToolRun {
	tr.Command = append([]string(nil), tr.Command...)
	tr.EnvAllowlist = append([]string(nil), tr.EnvAllowlist...)
	return tr
}
