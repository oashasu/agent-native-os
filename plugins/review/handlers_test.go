package main

import (
	"encoding/json"
	"github.com/example/agent-native-microkernel/sdk/go/protocol"
	"os"
	"path/filepath"
	"testing"
)

func fencedEnv(t *testing.T, dir string) protocol.Envelope {
	t.Helper()
	fenceRoot := filepath.Join(dir, ".fences")
	_ = os.MkdirAll(fenceRoot, 0o755)
	t.Setenv("VIBE_DATA_DIR", dir)
	t.Setenv("VIBE_RUNTIME_ID", "rt-test")
	t.Setenv("VIBE_FENCE_ROOT", fenceRoot)
	lease := map[string]any{"service": "default-review", "authority": "reviews-main", "runtime_id": "rt-test", "epoch": 1}
	b, _ := json.Marshal(lease)
	_ = os.WriteFile(filepath.Join(fenceRoot, "default-review--reviews-main.json"), b, 0o644)
	return protocol.Envelope{Protocol: 1, MessageID: "m", Kind: protocol.KindCommand, Service: "default-review", Authority: "reviews-main", FencingEpoch: 1}
}
func reviewFrom(t *testing.T, out any) Review {
	t.Helper()
	b, _ := json.Marshal(out)
	var r struct {
		Review Review `json:"review"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	return r.Review
}
func TestReviewHandlers(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	env := fencedEnv(t, dir)
	env.Payload = protocol.NewPayload(map[string]any{"work_context_id": "wc-1", "diff_artifact_id": "art-1", "agent_run_id": "run-1", "evidence_snapshot": []map[string]string{{"kind": "test", "outcome": "PASS"}}})
	out, perr := requestHandler(s)(env)
	if perr != nil {
		t.Fatalf("request: %+v", perr)
	}
	r := reviewFrom(t, out)
	if r.Status != "PENDING" || r.ID == "" {
		t.Fatalf("review: %+v", r)
	}
	bad := env
	bad.Payload = protocol.NewPayload(map[string]string{"work_context_id": "wc-1"})
	if _, e := requestHandler(s)(bad); e == nil || e.Code != "INVALID" {
		t.Fatalf("missing diff: %+v", e)
	}
	de := env
	de.Payload = protocol.NewPayload(map[string]any{"review_id": r.ID, "decision": "APPROVED", "reviewer": "a", "acceptance_results": []map[string]any{{"criterion_id": "AC1", "satisfied": true}}})
	out, perr = decideHandler(s)(de)
	if perr != nil {
		t.Fatalf("decide: %+v", perr)
	}
	if got := reviewFrom(t, out); got.Status != "APPROVED" {
		t.Fatalf("decided: %+v", got)
	}
	unknown := env
	unknown.Payload = protocol.NewPayload(map[string]string{"review_id": "nope", "decision": "APPROVED"})
	if _, e := decideHandler(s)(unknown); e == nil || e.Code != "NOT_FOUND" {
		t.Fatalf("unknown decide: %+v", e)
	}
	if _, e := decideHandler(s)(de); e == nil || e.Code != "CONFLICT" {
		t.Fatalf("second decide: %+v", e)
	}
	ge := env
	ge.Payload = protocol.NewPayload(map[string]string{"review_id": "nope"})
	if _, e := getHandler(s)(ge); e == nil || e.Code != "NOT_FOUND" {
		t.Fatalf("get unknown: %+v", e)
	}
	qe := env
	qe.Payload = protocol.NewPayload(map[string]string{"work_context_id": "wc-1"})
	qout, e := queryHandler(s)(qe)
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(qout)
	var qr struct {
		Reviews []Review `json:"reviews"`
	}
	_ = json.Unmarshal(b, &qr)
	if len(qr.Reviews) != 1 {
		t.Fatalf("query: %s", b)
	}
}
