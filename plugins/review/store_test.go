package main

import "testing"

func TestRequestThenDecide(t *testing.T) {
	dir := t.TempDir()
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	r := Review{ID: "r1", WorkContextID: "wc-1", DiffArtifactID: "art-1", Status: "PENDING", RequestedAt: "t0"}
	if err := s.RecordRequested(r); err != nil {
		t.Fatal(err)
	}
	out, err := s.RecordDecided("r1", "APPROVED", "alice", "lgtm", []AcceptanceResult{{CriterionID: "AC1", Satisfied: true}})
	if err != nil || out.Status != "APPROVED" || out.Reviewer != "alice" || len(out.AcceptanceResults) != 1 {
		t.Fatalf("decide: %+v err=%v", out, err)
	}
	if _, err := s.RecordDecided("r1", "CHANGES_REQUESTED", "bob", "", nil); err == nil {
		t.Fatal("second decide must be rejected")
	}
}

func TestReviewProjectionRebuilds(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	_ = s.RecordRequested(Review{ID: "r1", WorkContextID: "wc-1", DiffArtifactID: "a", Status: "PENDING", RequestedAt: "t1"})
	_, _ = s.RecordDecided("r1", "APPROVED", "x", "", nil)
	re, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := re.GetByID("r1")
	if !ok || got.Status != "APPROVED" {
		t.Fatalf("rebuilt: %+v ok=%v", got, ok)
	}
	if q := re.QueryByContext("wc-1"); len(q) != 1 {
		t.Fatalf("query: %+v", q)
	}
}
