package main

import (
	"encoding/json"
	"testing"
)

func TestRecordStartedThenCompleted(t *testing.T) {
	dir := t.TempDir()
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	ar := AgentRun{ID: "run-1", WorkContextID: "wc-1", Prompt: "p", Provider: "mock", Status: StatusRunning, StartedAt: "t0"}
	if err := s.RecordStarted(ar); err != nil {
		t.Fatalf("started: %v", err)
	}
	if err := s.RecordCompleted("run-1", StatusCompleted, "blob://sha256/abc", 3, json.RawMessage(`{"k":1}`)); err != nil {
		t.Fatalf("completed: %v", err)
	}
	got, ok := s.GetByID("run-1")
	if !ok || got.Status != StatusCompleted || got.RawSessionRef != "blob://sha256/abc" || got.FrameCount != 3 {
		t.Fatalf("run: %+v ok=%v", got, ok)
	}
}

func TestSecondTerminalTransitionRejected(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	_ = s.RecordStarted(AgentRun{ID: "run-1", Status: StatusRunning})
	_ = s.RecordCompleted("run-1", StatusCompleted, "", 0, nil)
	if err := s.RecordCancelled("run-1"); err == nil {
		t.Fatal("cancelling an already-completed run must error")
	}
}

func TestProjectionRebuildsAndQueryByContext(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	_ = s.RecordStarted(AgentRun{ID: "r1", WorkContextID: "wc-1", Status: StatusRunning, StartedAt: "t1"})
	_ = s.RecordStarted(AgentRun{ID: "r2", WorkContextID: "wc-1", Status: StatusRunning, StartedAt: "t2"})
	_ = s.RecordCompleted("r1", StatusCompleted, "", 1, nil)
	reloaded, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	runs := reloaded.QueryByContext("wc-1")
	if len(runs) != 2 || runs[0].ID != "r1" || runs[0].Status != StatusCompleted || runs[1].ID != "r2" {
		t.Fatalf("query: %+v", runs)
	}
}
