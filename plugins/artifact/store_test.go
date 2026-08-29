package main

import "testing"

func TestRecordAndReadBack(t *testing.T) {
	dir := t.TempDir()
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := Artifact{ID: "a1", WorkContextID: "wc-1", Kind: "diff", BlobURI: "blob://sha256/x", Summary: DiffSummary{FilesChanged: 2, Insertions: 5}, CreatedAt: "t0"}
	if err := s.Record(a); err != nil {
		t.Fatal(err)
	}
	got, ok := s.GetByID("a1")
	if !ok || got.Kind != "diff" || got.Summary.FilesChanged != 2 {
		t.Fatalf("get: %+v ok=%v", got, ok)
	}
	if q := s.QueryByContext("wc-1"); len(q) != 1 || q[0].ID != "a1" {
		t.Fatalf("query: %+v", q)
	}
}

func TestArtifactProjectionRebuilds(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	_ = s.Record(Artifact{ID: "a1", WorkContextID: "wc-1", Kind: "diff", CreatedAt: "t1"})
	_ = s.Record(Artifact{ID: "a2", WorkContextID: "wc-1", Kind: "command_output", CreatedAt: "t2"})
	re, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if q := re.QueryByContext("wc-1"); len(q) != 2 || q[0].ID != "a1" || q[1].ID != "a2" {
		t.Fatalf("rebuilt query: %+v", q)
	}
}
