package main

import (
	"encoding/json"
	"testing"

	"github.com/example/agent-native-microkernel/sdk/go/pluginhost"
	"github.com/example/agent-native-microkernel/sdk/go/protocol"
)

func cmdEnv(payload any) protocol.Envelope {
	return protocol.Envelope{Protocol: 1, MessageID: "m1", Kind: protocol.KindCommand, Capability: "workflow.engineering.run", Major: 1, Payload: protocol.NewPayload(payload)}
}
func TestRunHandlerHappyAndInvalid(t *testing.T) {
	f := &fakePipeline{reviews: []ReviewState{approved("art-1", true)}}
	h := runHandler(func(_ *pluginhost.RequestContext, _ protocol.Envelope) caps { return f.caps() })
	out, pe := h(nil, cmdEnv(baseRun()))
	if pe != nil {
		t.Fatalf("err=%+v", pe)
	}
	b, _ := json.Marshal(out)
	var got struct {
		Result RunResult `json:"result"`
	}
	_ = json.Unmarshal(b, &got)
	if got.Result.Outcome != "DONE" {
		t.Fatalf("%s", b)
	}
	_, pe = h(nil, cmdEnv(map[string]any{"prompt": "x", "build_command": []string{"true"}, "test_command": []string{"true"}}))
	if pe == nil || pe.Code != "INVALID" {
		t.Fatalf("expected INVALID, got %+v", pe)
	}
}
func TestGetHandlerFilters(t *testing.T) {
	replay := func() ([]JournalRecord, error) {
		return []JournalRecord{jr("1", "workspace.allocated", "wc-1", map[string]any{"task_id": "task-1"}), jr("2", "workflow.waiting_review", "wc-1", map[string]any{"task_id": "task-1"}), jr("3", "agent.run.started", "wc-2", map[string]any{"task_id": "task-2"})}, nil
	}
	out, pe := getHandler(replay)(protocol.Envelope{Payload: protocol.NewPayload(map[string]string{"task_id": "task-1"})})
	if pe != nil {
		t.Fatal(pe)
	}
	b, _ := json.Marshal(out)
	var got struct {
		Stage  string          `json:"stage"`
		Events []JournalRecord `json:"events"`
	}
	_ = json.Unmarshal(b, &got)
	if got.Stage != "WAITING_REVIEW" || len(got.Events) != 2 {
		t.Fatalf("%s", b)
	}
}
