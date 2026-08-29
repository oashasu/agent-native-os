package main

import "testing"

func TestSessionRecordReadAndProjection(t *testing.T) {
	dir := t.TempDir()
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := SessionRecord{ID: "s1", WorkContextID: "wc-1", AgentRunID: "r1", ArchiveRef: "blob://sha256/x", ArchiveHash: "abc", SealedAt: "t0"}
	if err := s.Record(a); err != nil {
		t.Fatal(err)
	}
	if got, ok := s.GetByID("s1"); !ok || got.ArchiveHash != "abc" {
		t.Fatalf("get: %+v ok=%v", got, ok)
	}
	if q := s.QueryByContext("wc-1"); len(q) != 1 || q[0].ID != "s1" {
		t.Fatalf("query: %+v", q)
	}
	if err := s.Record(SessionRecord{ID: "s2", WorkContextID: "wc-1", AgentRunID: "r2", ArchiveRef: "blob://sha256/y", SealedAt: "t1"}); err != nil {
		t.Fatal(err)
	}
	re, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	q := re.QueryByContext("wc-1")
	if len(q) != 2 || q[0].ID != "s1" || q[1].ID != "s2" {
		t.Fatalf("rebuilt: %+v", q)
	}
}
