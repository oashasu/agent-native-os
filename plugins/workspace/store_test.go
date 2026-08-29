package main

import "testing"

func TestRecordAllocatedThenGet(t *testing.T) {
	dir := t.TempDir()
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	ref := WorkspaceRef{ID: "ws-1", WorkContextID: "wc-1", Repo: "/tmp/r", Path: "/tmp/wt", Branch: "aeos/ws-1", BaseCommit: "abc", Status: StatusAllocated, AllocatedAt: "t0"}
	if err := s.RecordAllocated(ref); err != nil {
		t.Fatalf("record: %v", err)
	}
	got, ok := s.GetByID("ws-1")
	if !ok || got.Status != StatusAllocated || got.Branch != "aeos/ws-1" {
		t.Fatalf("get: %+v ok=%v", got, ok)
	}
	active, ok := s.GetActiveByContext("wc-1")
	if !ok || active.ID != "ws-1" {
		t.Fatalf("active by context: %+v ok=%v", active, ok)
	}
}

func TestReleaseUpdatesStatusAndSurvivesReload(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	_ = s.RecordAllocated(WorkspaceRef{ID: "ws-1", WorkContextID: "wc-1", Status: StatusAllocated})
	out, err := s.RecordReleased("ws-1", "preserve")
	if err != nil || out.Status != StatusReleased || out.ReleasePolicy != "preserve" {
		t.Fatalf("release: %+v err=%v", out, err)
	}
	if _, ok := s.GetActiveByContext("wc-1"); ok {
		t.Fatalf("released workspace must not be 'active' for its context")
	}
	reloaded, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := reloaded.GetByID("ws-1")
	if got.Status != StatusReleased || got.ReleasePolicy != "preserve" {
		t.Fatalf("release did not survive reload: %+v", got)
	}
}

func TestReleaseUnknownIsNotFound(t *testing.T) {
	s, _ := Load(t.TempDir())
	if _, err := s.RecordReleased("nope", "preserve"); err == nil {
		t.Fatal("releasing an unknown workspace must error")
	}
}
