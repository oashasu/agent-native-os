package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

func shortHash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:])[:12] }

func TestSealSelectsByCorrelationAndArchives(t *testing.T) {
	ws := gitRepoWithChange(t)
	s, _ := Load(t.TempDir())
	var puts [][]byte
	d := sealDeps{Store: s, Now: func() string { return "t0" }, BlobPut: func(b []byte) (string, error) {
		puts = append(puts, append([]byte(nil), b...))
		return "blob://sha256/" + shortHash(b), nil
	}, Replay: func() ([]JournalRecord, error) {
		return []JournalRecord{
			{ID: "e1", SHA256: "h1", CorrelationID: "wc-1", Type: "work.created", Raw: json.RawMessage(`{"id":"e1"}`)},
			{ID: "e2", SHA256: "h2", CorrelationID: "wc-OTHER", Type: "noise", Raw: json.RawMessage(`{"id":"e2","correlation_id":"wc-OTHER"}`)},
			{ID: "e3", SHA256: "h3", CorrelationID: "wc-1", Type: "work.done", Raw: json.RawMessage(`{"id":"e3"}`)},
		}, nil
	}}
	sr, perr := sealOnce(d, sealRequest{WorkContextID: "wc-1", AgentRunID: "r1", WorkspacePath: ws})
	if perr != nil {
		t.Fatalf("seal: %+v", perr)
	}
	if sr.EventSelection.EventCount != 2 || sr.ArchiveHash == "" || sr.ArchiveRef == "" {
		t.Fatalf("session record: %+v", sr)
	}
	if !sr.RecoveryCheckpoint.Dirty || sr.RecoveryCheckpoint.TrackedPatchRef == "" {
		t.Fatalf("checkpoint: %+v", sr.RecoveryCheckpoint)
	}
	if len(puts) != 2 {
		t.Fatalf("blob.put calls: %d", len(puts))
	}
	if strings.Contains(string(puts[1]), "wc-OTHER") {
		t.Fatalf("archive leaked a non-matching event")
	}
}
func TestSealVerifiesEventSHAWhenIDsGiven(t *testing.T) {
	ws := gitRepoWithChange(t)
	s, _ := Load(t.TempDir())
	d := sealDeps{Store: s, Now: func() string { return "t0" }, BlobPut: func([]byte) (string, error) { return "blob://sha256/x", nil }, Replay: func() ([]JournalRecord, error) {
		return []JournalRecord{{ID: "e1", SHA256: "GOOD", Raw: json.RawMessage(`{"id":"e1"}`)}}, nil
	}}
	_, perr := sealOnce(d, sealRequest{WorkContextID: "wc-1", AgentRunID: "r1", WorkspacePath: ws, EventIDs: []string{"e1"}, EventSHA256s: []string{"WRONG"}})
	if perr == nil || perr.Code != "INTEGRITY_ERROR" {
		t.Fatalf("want INTEGRITY_ERROR for sha mismatch, got %+v", perr)
	}
}
func TestSealRequiresFields(t *testing.T) {
	s, _ := Load(t.TempDir())
	_, perr := sealOnce(sealDeps{Store: s}, sealRequest{})
	if perr == nil || perr.Code != "INVALID" {
		t.Fatalf("want INVALID, got %+v", perr)
	}
}
