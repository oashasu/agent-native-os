package main

import (
	"encoding/json"
	"testing"
)

func jr(id, typ, wc string, extra map[string]any) JournalRecord {
	p := map[string]any{"work_context_id": wc}
	for k, v := range extra {
		p[k] = v
	}
	b, _ := json.Marshal(p)
	return JournalRecord{ID: id, Type: typ, CorrelationID: "outer", Payload: b}
}
func TestProjectFiltersByPayloadWorkContextAndTracksStage(t *testing.T) {
	recs := []JournalRecord{
		jr("1", "workspace.allocated", "wc-1", nil),
		jr("2", "agent.run.started", "wc-2", nil),
		jr("3", "tool.run.completed", "wc-1", map[string]any{"label": "build"}),
		jr("4", "workflow.waiting_review", "wc-1", nil),
		jr("5", "work.transitioned", "wc-2", map[string]any{"to": "DONE"}),
	}
	stage, matched := project(recs, "wc-1")
	if stage != "WAITING_REVIEW" {
		t.Fatalf("stage=%s", stage)
	}
	if len(matched) != 3 || matched[0].ID != "1" || matched[2].ID != "4" {
		t.Fatalf("matched=%v", matched)
	}
}
