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

func TestRefLessOrdering(t *testing.T) {
	cases := []struct {
		name        string
		a, b        WorkspaceRef
		wantBBetter bool
	}{
		{"later AllocatedAt wins", WorkspaceRef{ID: "a", AllocatedAt: "t0"}, WorkspaceRef{ID: "b", AllocatedAt: "t1"}, true},
		{"earlier AllocatedAt loses", WorkspaceRef{ID: "a", AllocatedAt: "t1"}, WorkspaceRef{ID: "b", AllocatedAt: "t0"}, false},
		{"equal AllocatedAt, later ReleasedAt wins", WorkspaceRef{ID: "a", AllocatedAt: "t0", ReleasedAt: "r0"}, WorkspaceRef{ID: "b", AllocatedAt: "t0", ReleasedAt: "r1"}, true},
		{"equal AllocatedAt and ReleasedAt, smaller ID wins", WorkspaceRef{ID: "ws-b", AllocatedAt: "t0", ReleasedAt: "r0"}, WorkspaceRef{ID: "ws-a", AllocatedAt: "t0", ReleasedAt: "r0"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, b := c.a, c.b
			if got := refLess(&a, &b); got != c.wantBBetter {
				t.Fatalf("refLess(a=%+v, b=%+v) = %v, want %v", a, b, got, c.wantBBetter)
			}
		})
	}
}

func TestGetByContextPrefersLatestAllocated(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	_ = s.RecordAllocated(WorkspaceRef{ID: "ws-1", WorkContextID: "wc-1", Status: StatusAllocated, AllocatedAt: "t0"})
	_ = s.RecordAllocated(WorkspaceRef{ID: "ws-2", WorkContextID: "wc-1", Status: StatusAllocated, AllocatedAt: "t1"})
	got, ok := s.GetByContext("wc-1")
	if !ok || got.ID != "ws-2" {
		t.Fatalf("want ws-2 (latest allocated), got %+v ok=%v", got, ok)
	}
}

func TestGetByContextPrefersAllocatedOverNewerPreserveReleased(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	_ = s.RecordAllocated(WorkspaceRef{ID: "ws-1", WorkContextID: "wc-1", Status: StatusAllocated, AllocatedAt: "t0"})
	_ = s.RecordAllocated(WorkspaceRef{ID: "ws-2", WorkContextID: "wc-1", Status: StatusReleased, ReleasePolicy: "preserve", AllocatedAt: "t1"})
	got, ok := s.GetByContext("wc-1")
	if !ok || got.ID != "ws-1" {
		t.Fatalf("want ws-1 (ALLOCATED beats a RELEASED+preserve with a *later* AllocatedAt — priority is by status first, not by timestamp), got %+v ok=%v", got, ok)
	}
}

func TestGetByContextFallsBackToLatestPreserveReleased(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	_ = s.RecordAllocated(WorkspaceRef{ID: "ws-1", WorkContextID: "wc-1", Status: StatusAllocated, AllocatedAt: "t0"})
	_ = s.RecordAllocated(WorkspaceRef{ID: "ws-2", WorkContextID: "wc-1", Status: StatusAllocated, AllocatedAt: "t1"})
	if _, err := s.RecordReleased("ws-1", "preserve"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordReleased("ws-2", "preserve"); err != nil {
		t.Fatal(err)
	}
	got, ok := s.GetByContext("wc-1")
	if !ok || got.ID != "ws-2" {
		t.Fatalf("want ws-2 (latest allocated among the preserved), got %+v ok=%v", got, ok)
	}
}

func TestGetByContextSkipsDeletedReleased(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	_ = s.RecordAllocated(WorkspaceRef{ID: "ws-1", WorkContextID: "wc-1", Status: StatusAllocated, AllocatedAt: "t0"})
	_ = s.RecordAllocated(WorkspaceRef{ID: "ws-2", WorkContextID: "wc-1", Status: StatusAllocated, AllocatedAt: "t1"})
	if _, err := s.RecordReleased("ws-1", "preserve"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordReleased("ws-2", "delete"); err != nil {
		t.Fatal(err)
	}
	got, ok := s.GetByContext("wc-1")
	if !ok || got.ID != "ws-1" {
		t.Fatalf("want ws-1 (only preserve candidate; ws-2 is delete-policy), got %+v ok=%v", got, ok)
	}
}

func TestGetByContextTiebreaksOnReleasedAtWhenAllocatedAtEqual(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	_ = s.RecordAllocated(WorkspaceRef{ID: "ws-1", WorkContextID: "wc-1", Status: StatusReleased, ReleasePolicy: "preserve", AllocatedAt: "t0", ReleasedAt: "r0"})
	_ = s.RecordAllocated(WorkspaceRef{ID: "ws-2", WorkContextID: "wc-1", Status: StatusReleased, ReleasePolicy: "preserve", AllocatedAt: "t0", ReleasedAt: "r1"})
	got, ok := s.GetByContext("wc-1")
	if !ok || got.ID != "ws-2" {
		t.Fatalf("want ws-2 (later ReleasedAt breaks the AllocatedAt tie), got %+v ok=%v", got, ok)
	}
}

func TestGetByContextTiebreaksOnIDWhenAllTimestampsEqual(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	_ = s.RecordAllocated(WorkspaceRef{ID: "ws-b", WorkContextID: "wc-1", Status: StatusReleased, ReleasePolicy: "preserve", AllocatedAt: "t0", ReleasedAt: "r0"})
	_ = s.RecordAllocated(WorkspaceRef{ID: "ws-a", WorkContextID: "wc-1", Status: StatusReleased, ReleasePolicy: "preserve", AllocatedAt: "t0", ReleasedAt: "r0"})
	got, ok := s.GetByContext("wc-1")
	if !ok || got.ID != "ws-a" {
		t.Fatalf("want ws-a (smaller ID wins a full tie), got %+v ok=%v", got, ok)
	}
}

func TestGetByContextNoCandidatesNotFound(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	_ = s.RecordAllocated(WorkspaceRef{ID: "ws-1", WorkContextID: "wc-1", Status: StatusReleased, ReleasePolicy: "delete", AllocatedAt: "t0"})
	if _, ok := s.GetByContext("wc-1"); ok {
		t.Fatalf("delete-policy-only context must yield no candidate")
	}
	if _, ok := s.GetByContext("wc-does-not-exist"); ok {
		t.Fatalf("unknown context must yield no candidate")
	}
}

func TestGetByContextSurvivesReload(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	_ = s.RecordAllocated(WorkspaceRef{ID: "ws-1", WorkContextID: "wc-1", Status: StatusAllocated, AllocatedAt: "t0"})
	if _, err := s.RecordReleased("ws-1", "preserve"); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reloaded.GetByContext("wc-1")
	if !ok || got.ID != "ws-1" || got.Status != StatusReleased {
		t.Fatalf("GetByContext after reload: %+v ok=%v", got, ok)
	}
}
