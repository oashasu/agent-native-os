package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/example/agent-native-microkernel/sdk/go/protocol"
)

func fencedEnv(t *testing.T, dir string) protocol.Envelope {
	t.Helper()
	fenceRoot := filepath.Join(dir, ".fences")
	_ = os.MkdirAll(fenceRoot, 0o755)
	t.Setenv("VIBE_DATA_DIR", dir)
	t.Setenv("VIBE_RUNTIME_ID", "rt-test")
	t.Setenv("VIBE_FENCE_ROOT", fenceRoot)
	lease := map[string]any{"service": "default-work-registry", "authority": "work-main", "runtime_id": "rt-test", "epoch": 1}
	b, _ := json.Marshal(lease)
	_ = os.WriteFile(filepath.Join(fenceRoot, "default-work-registry--work-main.json"), b, 0o644)
	return protocol.Envelope{
		Protocol: 1, MessageID: "m", Kind: protocol.KindCommand,
		Service: "default-work-registry", Authority: "work-main", FencingEpoch: 1,
	}
}

func TestCreateHandlerRejectsMissingRequired(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	env := fencedEnv(t, dir)
	env.Payload = protocol.NewPayload(map[string]any{"title": "x"})
	_, perr := createHandler(s)(env)
	if perr == nil || perr.Code != "INVALID" {
		t.Fatalf("want INVALID, got %+v", perr)
	}
}

func TestCreateThenGetByTaskAndByContext(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	env := fencedEnv(t, dir)
	env.Payload = protocol.NewPayload(map[string]any{
		"title": "harden add", "goal": "reject bad input", "repo": "fixtures/sample-java-project",
		"acceptance_criteria": []map[string]string{{"id": "AC1", "text": "mvn test PASS"}},
	})
	out, perr := createHandler(s)(env)
	if perr != nil {
		t.Fatalf("create: %+v", perr)
	}
	var cr struct {
		Task        Task        `json:"task"`
		WorkContext WorkContext `json:"work_context"`
	}
	b, _ := json.Marshal(out)
	_ = json.Unmarshal(b, &cr)
	if cr.Task.Status != StatusPlanned || len(cr.Task.AcceptanceCriteria) != 1 {
		t.Fatalf("created task: %+v", cr.Task)
	}

	for _, q := range []map[string]string{{"task_id": cr.Task.ID}, {"work_context_id": cr.WorkContext.ID}} {
		gout, gperr := getHandler(s)(protocol.Envelope{Payload: protocol.NewPayload(q)})
		if gperr != nil {
			t.Fatalf("get %v: %+v", q, gperr)
		}
		gb, _ := json.Marshal(gout)
		var gr struct{ Task Task `json:"task"` }
		_ = json.Unmarshal(gb, &gr)
		if gr.Task.ID != cr.Task.ID {
			t.Fatalf("get %v returned %s", q, gr.Task.ID)
		}
	}
}

func TestGetRequiresExactlyOneSelector(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	_, perr := getHandler(s)(protocol.Envelope{Payload: protocol.NewPayload(map[string]string{})})
	if perr == nil || perr.Code != "INVALID" {
		t.Fatalf("want INVALID for no selector, got %+v", perr)
	}
	_, perr = getHandler(s)(protocol.Envelope{Payload: protocol.NewPayload(map[string]string{"task_id": "unknown"})})
	if perr == nil || perr.Code != "NOT_FOUND" {
		t.Fatalf("want NOT_FOUND for unknown task, got %+v", perr)
	}
}
