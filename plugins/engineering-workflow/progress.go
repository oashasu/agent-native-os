package main

import "encoding/json"

type JournalRecord struct {
	ID            string          `json:"id"`
	Type          string          `json:"type"`
	CorrelationID string          `json:"correlation_id"`
	Payload       json.RawMessage `json:"payload"`
}

func project(records []JournalRecord, correlationID string) (stage string, matched []JournalRecord) {
	stage = "CREATED"
	for _, r := range records {
		var p map[string]any
		_ = json.Unmarshal(r.Payload, &p)
		wc, _ := p["work_context_id"].(string)
		taskID, _ := p["task_id"].(string)
		if wc != correlationID && taskID != correlationID {
			continue
		}
		matched = append(matched, r)
		switch r.Type {
		case "workspace.allocated": stage = "WORKSPACE"
		case "agent.run.started": stage = "AGENT"
		case "diff.collected": stage = "DIFF"
		case "tool.run.completed": if p["label"] == "build" { stage = "BUILD" } else if p["label"] == "test" { stage = "TEST" }
		case "work.transitioned": if p["to"] == "IN_REVIEW" { stage = "REVIEW_REQUESTED" } else if p["to"] == "DONE" { stage = "DONE" }
		case "workflow.waiting_review": stage = "WAITING_REVIEW"
		case "review.decided": stage = "REVIEWED"
		case "session.sealed": stage = "SEALED"
		}
	}
	return
}
