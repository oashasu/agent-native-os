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

var (
	ErrNotFound       = errors.New("review not found")
	ErrAlreadyDecided = errors.New("review already decided")
)

type AcceptanceResult struct {
	CriterionID  string   `json:"criterion_id"`
	Satisfied    bool     `json:"satisfied"`
	EvidenceRefs []string `json:"evidence_refs"`
	Notes        string   `json:"notes"`
}
type EvidenceSnapshotItem struct {
	Kind          string `json:"kind"`
	Outcome       string `json:"outcome"`
	EvidenceRefID string `json:"evidence_ref_id"`
}
type Review struct {
	ID                string                 `json:"id"`
	WorkContextID     string                 `json:"work_context_id"`
	AgentRunID        string                 `json:"agent_run_id"`
	DiffArtifactID    string                 `json:"diff_artifact_id"`
	Status            string                 `json:"status"`
	Reviewer          string                 `json:"reviewer"`
	Notes             string                 `json:"notes"`
	AcceptanceResults []AcceptanceResult     `json:"acceptance_results"`
	EvidenceSnapshot  []EvidenceSnapshotItem `json:"evidence_snapshot"`
	RequestedAt       string                 `json:"requested_at"`
	DecidedAt         string                 `json:"decided_at"`
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
	byID  map[string]*Review
	byCtx map[string][]string
}

func Load(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{path: filepath.Join(dir, "review-log.jsonl"), byID: map[string]*Review{}, byCtx: map[string][]string{}}
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
			return nil, fmt.Errorf("review log line %d: %w", i+1, err)
		}
		if err := s.apply(rec); err != nil {
			return nil, fmt.Errorf("review log line %d: %w", i+1, err)
		}
		if rec.Seq > s.seq {
			s.seq = rec.Seq
		}
	}
	return s, nil
}
func (s *Store) apply(rec logRecord) error {
	var r Review
	if err := json.Unmarshal(rec.Data, &r); err != nil {
		return err
	}
	switch rec.Op {
	case "review.requested":
		c := cloneReview(r)
		s.byID[r.ID] = &c
		s.byCtx[r.WorkContextID] = append(s.byCtx[r.WorkContextID], r.ID)
	case "review.decided":
		c := cloneReview(r)
		s.byID[r.ID] = &c
	default:
		return fmt.Errorf("unknown op %q", rec.Op)
	}
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
func (s *Store) RecordRequested(r Review) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.append("review.requested", r)
}
func (s *Store) RecordDecided(id, decision, reviewer, notes string, results []AcceptanceResult) (Review, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.byID[id]
	if !ok {
		return Review{}, ErrNotFound
	}
	if p.Status != "PENDING" {
		return Review{}, ErrAlreadyDecided
	}
	r := cloneReview(*p)
	r.Status = decision
	r.Reviewer = reviewer
	r.Notes = notes
	r.AcceptanceResults = append([]AcceptanceResult(nil), results...)
	r.DecidedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := s.append("review.decided", r); err != nil {
		return Review{}, err
	}
	return cloneReview(r), nil
}
func (s *Store) GetByID(id string) (Review, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.byID[id]
	if !ok {
		return Review{}, false
	}
	return cloneReview(*r), true
}
func (s *Store) QueryByContext(w string) []Review {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := s.byCtx[w]
	out := make([]Review, 0, len(ids))
	for _, id := range ids {
		if r, ok := s.byID[id]; ok {
			out = append(out, cloneReview(*r))
		}
	}
	return out
}
func cloneReview(r Review) Review {
	r.EvidenceSnapshot = append([]EvidenceSnapshotItem(nil), r.EvidenceSnapshot...)
	r.AcceptanceResults = append([]AcceptanceResult(nil), r.AcceptanceResults...)
	for i := range r.AcceptanceResults {
		r.AcceptanceResults[i].EvidenceRefs = append([]string(nil), r.AcceptanceResults[i].EvidenceRefs...)
	}
	return r
}
