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

	"github.com/example/agent-native-microkernel/sdk/go/protocol"
)

type Status string

const (
	StatusPlanned    Status = "PLANNED"
	StatusInProgress Status = "IN_PROGRESS"
	StatusInReview   Status = "IN_REVIEW"
	StatusDone       Status = "DONE"
	StatusFailed     Status = "FAILED"
)

var (
	ErrNotFound          = errors.New("not found")
	ErrConflict          = errors.New("version conflict")
	ErrIllegalTransition = errors.New("illegal transition")
)

type AcceptanceCriterion struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

type EvidenceRef struct {
	ID               string  `json:"id"`
	Kind             string  `json:"kind"`
	SourceCapability string  `json:"source_capability"`
	SourceID         string  `json:"source_id"`
	Outcome          string  `json:"outcome"`
	ObservedAt       string  `json:"observed_at"`
	ContentHash      string  `json:"content_hash"`
	InvalidatedAt    *string `json:"invalidated_at"`
}

type Task struct {
	ID                 string                `json:"id"`
	Title              string                `json:"title"`
	Goal               string                `json:"goal"`
	Scope              string                `json:"scope"`
	AcceptanceCriteria []AcceptanceCriterion `json:"acceptance_criteria"`
	Status             Status                `json:"status"`
	Version            int                   `json:"version"`
	WorkContextID      string                `json:"work_context_id"`
}

type WorkContext struct {
	ID                 string        `json:"id"`
	TaskID             string        `json:"task_id"`
	Repo               string        `json:"repo"`
	ActiveWorkspaceRef any           `json:"active_workspace_ref"`
	EvidenceRefs       []EvidenceRef `json:"evidence_refs"`
	Version            int           `json:"version"`
}

type CreateInput struct {
	Title          string
	Goal           string
	Scope          string
	Repo           string
	Acceptance     []AcceptanceCriterion
	IdempotencyKey string
}

type logRecord struct {
	Seq  int64           `json:"seq"`
	TS   string          `json:"ts"`
	Op   string          `json:"op"`
	Data json.RawMessage `json:"data"`
}

type Store struct {
	mu       sync.Mutex
	path     string
	seq      int64
	tasks    map[string]*Task
	wcs      map[string]*WorkContext
	taskByWC map[string]string
	idem     map[string]string
}

type createdData struct {
	Task           Task        `json:"task"`
	WorkContext    WorkContext `json:"work_context"`
	IdempotencyKey string      `json:"idempotency_key,omitempty"`
}

type transitionedData struct {
	WorkContextID    string `json:"work_context_id"`
	From             Status `json:"from"`
	To               Status `json:"to"`
	TaskVersionAfter int    `json:"task_version_after"`
}

type evidenceData struct {
	WorkContextID string      `json:"work_context_id"`
	EvidenceRef   EvidenceRef `json:"evidence_ref"`
}

func Load(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{
		path:     filepath.Join(dir, "work-log.jsonl"),
		tasks:    map[string]*Task{},
		wcs:      map[string]*WorkContext{},
		taskByWC: map[string]string{},
		idem:     map[string]string{},
	}
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
			return nil, fmt.Errorf("work log line %d: %w", i+1, err)
		}
		if err := s.apply(rec); err != nil {
			return nil, fmt.Errorf("work log line %d: %w", i+1, err)
		}
		if rec.Seq > s.seq {
			s.seq = rec.Seq
		}
	}
	return s, nil
}

func (s *Store) apply(rec logRecord) error {
	switch rec.Op {
	case "task.created":
		var d createdData
		if err := json.Unmarshal(rec.Data, &d); err != nil {
			return err
		}
		task := d.Task
		wc := d.WorkContext
		s.tasks[task.ID] = &task
		s.wcs[wc.ID] = &wc
		s.taskByWC[wc.ID] = task.ID
		if d.IdempotencyKey != "" {
			s.idem[d.IdempotencyKey] = task.ID
		}
	case "work.transitioned":
		var d transitionedData
		if err := json.Unmarshal(rec.Data, &d); err != nil {
			return err
		}
		taskID, ok := s.taskByWC[d.WorkContextID]
		if !ok {
			return ErrNotFound
		}
		task := s.tasks[taskID]
		task.Status = d.To
		task.Version = d.TaskVersionAfter
	case "evidence.attached":
		var d evidenceData
		if err := json.Unmarshal(rec.Data, &d); err != nil {
			return err
		}
		wc, ok := s.wcs[d.WorkContextID]
		if !ok {
			return ErrNotFound
		}
		wc.EvidenceRefs = append(wc.EvidenceRefs, d.EvidenceRef)
		wc.Version++
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

func (s *Store) CreateTask(in CreateInput) (*Task, *WorkContext, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if in.IdempotencyKey != "" {
		if taskID, ok := s.idem[in.IdempotencyKey]; ok {
			t := s.tasks[taskID]
			wc := s.wcs[t.WorkContextID]
			return cloneTask(t), cloneWC(wc), true, nil
		}
	}
	taskID := protocol.NewID("task")
	wcID := protocol.NewID("wc")
	task := Task{ID: taskID, Title: in.Title, Goal: in.Goal, Scope: in.Scope, AcceptanceCriteria: append([]AcceptanceCriterion(nil), in.Acceptance...), Status: StatusPlanned, Version: 1, WorkContextID: wcID}
	wc := WorkContext{ID: wcID, TaskID: taskID, Repo: in.Repo, ActiveWorkspaceRef: nil, EvidenceRefs: []EvidenceRef{}, Version: 1}
	if err := s.append("task.created", createdData{Task: task, WorkContext: wc, IdempotencyKey: in.IdempotencyKey}); err != nil {
		return nil, nil, false, err
	}
	return cloneTask(s.tasks[taskID]), cloneWC(s.wcs[wcID]), false, nil
}

func legalTransition(from, to Status) bool {
	if to == StatusFailed && from != StatusDone && from != StatusFailed {
		return true
	}
	return (from == StatusPlanned && to == StatusInProgress) ||
		(from == StatusInProgress && to == StatusInReview) ||
		(from == StatusInReview && to == StatusDone)
}

func (s *Store) Transition(wcID string, to Status, expectedVersion int) (*Task, *WorkContext, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	taskID, ok := s.taskByWC[wcID]
	if !ok {
		return nil, nil, ErrNotFound
	}
	task := s.tasks[taskID]
	if task.Version != expectedVersion {
		return nil, nil, ErrConflict
	}
	if !legalTransition(task.Status, to) {
		return nil, nil, ErrIllegalTransition
	}
	if err := s.append("work.transitioned", transitionedData{WorkContextID: wcID, From: task.Status, To: to, TaskVersionAfter: task.Version + 1}); err != nil {
		return nil, nil, err
	}
	return cloneTask(s.tasks[taskID]), cloneWC(s.wcs[wcID]), nil
}

func (s *Store) AttachEvidence(wcID string, ev EvidenceRef, expectedWCVersion int) (*EvidenceRef, *WorkContext, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	wc, ok := s.wcs[wcID]
	if !ok {
		return nil, nil, ErrNotFound
	}
	if wc.Version != expectedWCVersion {
		return nil, nil, ErrConflict
	}
	ev.ID = protocol.NewID("evidence")
	ev.ObservedAt = time.Now().UTC().Format(time.RFC3339Nano)
	ev.InvalidatedAt = nil
	if err := s.append("evidence.attached", evidenceData{WorkContextID: wcID, EvidenceRef: ev}); err != nil {
		return nil, nil, err
	}
	out := ev
	return &out, cloneWC(s.wcs[wcID]), nil
}

func (s *Store) GetByTask(taskID string) (Task, WorkContext, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[taskID]
	if !ok {
		return Task{}, WorkContext{}, false
	}
	wc := s.wcs[task.WorkContextID]
	return *cloneTask(task), *cloneWC(wc), true
}

func (s *Store) GetByContext(wcID string) (Task, WorkContext, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	taskID, ok := s.taskByWC[wcID]
	if !ok {
		return Task{}, WorkContext{}, false
	}
	task := s.tasks[taskID]
	wc := s.wcs[wcID]
	return *cloneTask(task), *cloneWC(wc), true
}

func cloneTask(in *Task) *Task {
	if in == nil {
		return nil
	}
	out := *in
	out.AcceptanceCriteria = append([]AcceptanceCriterion(nil), in.AcceptanceCriteria...)
	return &out
}

func cloneWC(in *WorkContext) *WorkContext {
	if in == nil {
		return nil
	}
	out := *in
	out.EvidenceRefs = append([]EvidenceRef(nil), in.EvidenceRefs...)
	return &out
}
