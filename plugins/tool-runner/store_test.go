package main

import "testing"

func TestToolRunRecordAndReadBack(t *testing.T) {
	dir := t.TempDir()
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	tr := ToolRun{ID: "t1", WorkContextID: "wc-1", Label: "build", Outcome: "PASS", ExitCode: 0, Fingerprint: "abc"}
	if err := s.Record(tr); err != nil {
		t.Fatal(err)
	}
	got, ok := s.GetByID("t1")
	if !ok || got.Label != "build" || got.Outcome != "PASS" || got.Fingerprint != "abc" {
		t.Fatalf("get: %+v ok=%v", got, ok)
	}
	if q := s.QueryByContext("wc-1"); len(q) != 1 || q[0].ID != "t1" {
		t.Fatalf("query: %+v", q)
	}
}

func TestToolRunProjectionRebuilds(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	_ = s.Record(ToolRun{ID: "t1", WorkContextID: "wc-1", Label: "build", Outcome: "PASS"})
	_ = s.Record(ToolRun{ID: "t2", WorkContextID: "wc-1", Label: "test", Outcome: "FAIL", ExitCode: 1})
	re, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if q := re.QueryByContext("wc-1"); len(q) != 2 || q[0].ID != "t1" || q[1].ID != "t2" {
		t.Fatalf("rebuilt query: %+v", q)
	}
}
