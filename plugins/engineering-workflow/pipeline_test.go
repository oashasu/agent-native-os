package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakePipeline struct {
	mu           sync.Mutex
	calls        []string
	events       []string
	agentStatus  string
	buildOutcome string
	testOutcome  string
	reviews      []ReviewState
	reviewN      int
}

func (f *fakePipeline) add(s string) { f.mu.Lock(); defer f.mu.Unlock(); f.calls = append(f.calls, s) }
func (f *fakePipeline) has(s string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, x := range f.calls {
		if x == s {
			return true
		}
	}
	return false
}
func (f *fakePipeline) caps() caps {
	return caps{
		WorkGet: func(taskID string) (Task, string, int, error) { f.add("get"); return Task{ID: taskID}, "wc-1", 1, nil },
		WorkTransition: func(_, to string, _ int) (int, error) {
			f.add("transition:" + to)
			return map[string]int{"IN_PROGRESS": 2, "IN_REVIEW": 3, "DONE": 4}[to], nil
		},
		WorkspaceAlloc:   func(_, _ string) (string, string, error) { f.add("allocate"); return "ws-1", "/tmp/ws", nil },
		WorkspaceRelease: func(_, _ string) error { f.add("release"); return nil },
		AgentRun: func(_, _, _, _, _ string) (string, string, error) {
			f.add("agent")
			st := f.agentStatus
			if st == "" {
				st = "COMPLETED"
			}
			return "ar-1", st, nil
		},
		CollectDiff: func(_, _ string) (string, int, error) { f.add("diff"); return "art-1", 1, nil },
		ToolRun: func(_, _, label string, _ []string) (string, string, error) {
			f.add(label)
			if label == "build" {
				o := f.buildOutcome
				if o == "" {
					o = "PASS"
				}
				return "tr-b", o, nil
			}
			o := f.testOutcome
			if o == "" {
				o = "PASS"
			}
			return "tr-t", o, nil
		},
		AttachEvidence: func(_, kind, _, _, _ string) error { f.add("evidence:" + kind); return nil },
		ReviewRequest:  func(_, _, _ string, _ []EvItem) (string, error) { f.add("review.request"); return "rev-1", nil },
		ReviewGet: func(_ string) (ReviewState, error) {
			f.add("review.get")
			if len(f.reviews) == 0 {
				return approved("art-1", true), nil
			}
			i := f.reviewN
			if i >= len(f.reviews) {
				i = len(f.reviews) - 1
			}
			f.reviewN++
			return f.reviews[i], nil
		},
		SessionSeal: func(_, _, _ string) (string, error) { f.add("seal"); return "sess-1", nil },
		AppendEvent: func(typ string, _ map[string]any) (string, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.events = append(f.events, typ)
			return "ev-" + typ, nil
		},
		Sleep: func(d time.Duration) { time.Sleep(d) },
		Now:   func() string { return "now" },
	}
}

func baseRun() RunRequest {
	return RunRequest{TaskID: "task-1", Prompt: "go", BuildCommand: []string{"true"}, TestCommand: []string{"true"}, ReviewPollMS: 1}
}

func TestRunPipelineHappyPath(t *testing.T) {
	f := &fakePipeline{reviews: []ReviewState{{Status: "PENDING", DiffArtifactID: "art-1"}, {Status: "PENDING", DiffArtifactID: "art-1"}, approved("art-1", true)}}
	r := runPipeline(context.Background(), f.caps(), baseRun())
	if r.Outcome != "DONE" {
		t.Fatalf("outcome=%s reason=%s calls=%v", r.Outcome, r.Reason, f.calls)
	}
	want := []string{"allocate", "transition:IN_PROGRESS", "agent", "diff", "build", "test", "transition:IN_REVIEW", "review.request", "review.get", "transition:DONE", "seal", "release"}
	pos := 0
	for _, c := range f.calls {
		if pos < len(want) && c == want[pos] {
			pos++
		}
	}
	if pos != len(want) {
		t.Fatalf("order calls=%v want subsequence=%v", f.calls, want)
	}
	if len(r.EventIDs) < 12 {
		t.Fatalf("events=%d", len(r.EventIDs))
	}
	found := false
	for _, e := range f.events {
		if e == "workflow.waiting_review" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing waiting event: %v", f.events)
	}
}

func TestRunPipelineGateFailOnTest(t *testing.T) {
	f := &fakePipeline{testOutcome: "FAIL"}
	r := runPipeline(context.Background(), f.caps(), baseRun())
	if r.Outcome != "GATE_FAILED" || !strings.Contains(r.Reason, "test") {
		t.Fatalf("%+v", r)
	}
	if f.has("transition:DONE") {
		t.Fatal("DONE transition happened")
	}
	if !f.has("seal") {
		t.Fatal("session not sealed")
	}
}
func TestRunPipelineGateFailOnStaleDiff(t *testing.T) {
	// CollectDiff (fake) returns "art-1"; the only review is APPROVED but bound
	// to a different diff artifact. runPipeline must refuse DONE.
	f := &fakePipeline{reviews: []ReviewState{approved("art-STALE", true)}}
	r := runPipeline(context.Background(), f.caps(), baseRun())
	if r.Outcome != "GATE_FAILED" || !strings.Contains(r.Reason, "diff") {
		t.Fatalf("outcome=%s reason=%s", r.Outcome, r.Reason)
	}
	if f.has("transition:DONE") {
		t.Fatal("DONE transition happened despite stale-diff review")
	}
	if !f.has("seal") || !f.has("release") {
		t.Fatal("cleanup missing")
	}
}

func TestRunPipelineAgentFailed(t *testing.T) {
	f := &fakePipeline{agentStatus: "FAILED"}
	r := runPipeline(context.Background(), f.caps(), baseRun())
	if r.Outcome != "AGENT_FAILED" {
		t.Fatalf("%+v", r)
	}
	if f.has("build") || f.has("test") {
		t.Fatal("tools ran")
	}
	if !f.has("seal") || !f.has("release") {
		t.Fatal("cleanup missing")
	}
}
func TestRunPipelineReviewTimeout(t *testing.T) {
	f := &fakePipeline{reviews: []ReviewState{{Status: "PENDING", DiffArtifactID: "art-1"}}}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	req := baseRun()
	req.ReviewPollMS = 2
	r := runPipeline(ctx, f.caps(), req)
	if r.Outcome != "TIMEOUT" {
		t.Fatalf("%+v", r)
	}
}
func TestRunPipelineTaskNotFound(t *testing.T) {
	c := (&fakePipeline{}).caps()
	c.WorkGet = func(string) (Task, string, int, error) { return Task{}, "", 0, errors.New("no") }
	r := runPipeline(context.Background(), c, baseRun())
	if r.Outcome != "GATE_FAILED" || r.Reason != "task not found" {
		t.Fatalf("%+v", r)
	}
}
