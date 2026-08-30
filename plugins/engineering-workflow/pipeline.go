package main

import (
	"context"
	"fmt"
	"time"
)

type Task struct {
	ID string `json:"id"`
}
type caps struct {
	WorkGet          func(taskID string) (Task, string, int, error)
	WorkTransition   func(wcID, to string, expectedVersion int) (int, error)
	WorkspaceAlloc   func(wcID, baseRef string) (string, string, error)
	WorkspaceRelease func(wsID, policy string) error
	AgentRun         func(wcID, wsPath, prompt, writeFile, writeContent string) (string, string, error)
	CollectDiff      func(wcID, wsPath string) (string, int, error)
	ToolRun          func(wcID, wsPath, label string, argv []string) (string, string, error)
	AttachEvidence   func(wcID, kind, srcCap, srcID, outcome string) error
	ReviewRequest    func(wcID, agentRunID, diffArtifactID string, snapshot []EvItem) (string, error)
	ReviewGet        func(reviewID string) (ReviewState, error)
	SessionSeal      func(wcID, agentRunID, wsPath string) (string, error)
	AppendEvent      func(eventType string, payload map[string]any) (string, error)
	Sleep            func(time.Duration)
	Now              func() string
}
type EvItem struct {
	Kind          string `json:"kind"`
	Outcome       string `json:"outcome"`
	EvidenceRefID string `json:"evidence_ref_id"`
}
type RunRequest struct {
	TaskID                string   `json:"task_id"`
	Prompt                string   `json:"prompt"`
	BaseRef               string   `json:"base_ref"`
	BuildCommand          []string `json:"build_command"`
	TestCommand           []string `json:"test_command"`
	ReviewPollMS          int      `json:"review_poll_ms"`
	MockAgentWriteFile    string   `json:"mock_agent_write_file"`
	MockAgentWriteContent string   `json:"mock_agent_write_content"`
}
type RunResult struct {
	WorkContextID  string   `json:"work_context_id"`
	TaskID         string   `json:"task_id"`
	Outcome        string   `json:"outcome"`
	Reason         string   `json:"reason,omitempty"`
	AgentRunID     string   `json:"agent_run_id,omitempty"`
	DiffArtifactID string   `json:"diff_artifact_id,omitempty"`
	BuildToolRunID string   `json:"build_tool_run_id,omitempty"`
	TestToolRunID  string   `json:"test_tool_run_id,omitempty"`
	ReviewID       string   `json:"review_id,omitempty"`
	SessionID      string   `json:"session_id,omitempty"`
	EventIDs       []string `json:"event_ids"`
}

// runPipeline is synchronous and watches only ctx (the request deadline), never the client socket.
func runPipeline(ctx context.Context, c caps, req RunRequest) (res RunResult) {
	res.TaskID = req.TaskID
	_, wc, ver, err := c.WorkGet(req.TaskID)
	if err != nil {
		res.Outcome = "GATE_FAILED"
		res.Reason = "task not found"
		return
	}
	res.WorkContextID = wc
	add := func(t string, p map[string]any) {
		if p == nil {
			p = map[string]any{}
		}
		p["work_context_id"] = wc
		if id, e := c.AppendEvent(t, p); e == nil && id != "" {
			res.EventIDs = append(res.EventIDs, id)
		}
	}
	wsID, wsPath, err := c.WorkspaceAlloc(wc, req.BaseRef)
	if err != nil {
		return fail(&res, "GATE_FAILED", "workspace allocate: "+err.Error())
	}
	add("workspace.allocated", map[string]any{"workspace_id": wsID, "path": wsPath})
	cleaned := false
	cleanup := func() {
		if cleaned {
			return
		}
		cleaned = true
		if res.AgentRunID != "" {
			if sid, e := c.SessionSeal(wc, res.AgentRunID, wsPath); e == nil {
				res.SessionID = sid
				add("session.sealed", map[string]any{"session_id": sid})
			}
		}
		if e := c.WorkspaceRelease(wsID, "preserve"); e == nil {
			add("workspace.released", map[string]any{"workspace_id": wsID, "policy": "preserve"})
		}
	}
	ver, err = c.WorkTransition(wc, "IN_PROGRESS", ver)
	if err != nil {
		cleanup()
		return fail(&res, "GATE_FAILED", "transition IN_PROGRESS: "+err.Error())
	}
	add("work.transitioned", map[string]any{"from": "PLANNED", "to": "IN_PROGRESS"})
	if ctx.Err() != nil {
		cleanup()
		return fail(&res, "TIMEOUT", ctx.Err().Error())
	}
	add("agent.run.started", nil)
	ar, st, err := c.AgentRun(wc, wsPath, req.Prompt, req.MockAgentWriteFile, req.MockAgentWriteContent)
	res.AgentRunID = ar
	if err != nil {
		cleanup()
		return fail(&res, "AGENT_FAILED", err.Error())
	}
	add("agent.run.completed", map[string]any{"agent_run_id": ar, "status": st})
	if st != "COMPLETED" {
		cleanup()
		return fail(&res, "AGENT_FAILED", "agent status "+st)
	}
	diff, n, err := c.CollectDiff(wc, wsPath)
	if err != nil {
		cleanup()
		return fail(&res, "GATE_FAILED", "diff: "+err.Error())
	}
	res.DiffArtifactID = diff
	add("diff.collected", map[string]any{"artifact_id": diff, "files_changed": n})
	bID, bOut, err := c.ToolRun(wc, wsPath, "build", req.BuildCommand)
	if err != nil {
		cleanup()
		return fail(&res, "BUILD_FAILED", err.Error())
	}
	res.BuildToolRunID = bID
	add("tool.run.completed", map[string]any{"tool_run_id": bID, "label": "build", "outcome": bOut})
	_ = c.AttachEvidence(wc, "build", "tool.run@1", bID, bOut)
	add("evidence.attached", map[string]any{"kind": "build", "source_id": bID, "outcome": bOut})
	tID, tOut, err := c.ToolRun(wc, wsPath, "test", req.TestCommand)
	if err != nil {
		cleanup()
		return fail(&res, "TEST_FAILED", err.Error())
	}
	res.TestToolRunID = tID
	add("tool.run.completed", map[string]any{"tool_run_id": tID, "label": "test", "outcome": tOut})
	_ = c.AttachEvidence(wc, "test", "tool.run@1", tID, tOut)
	add("evidence.attached", map[string]any{"kind": "test", "source_id": tID, "outcome": tOut})
	ver, err = c.WorkTransition(wc, "IN_REVIEW", ver)
	if err != nil {
		cleanup()
		return fail(&res, "GATE_FAILED", err.Error())
	}
	add("work.transitioned", map[string]any{"from": "IN_PROGRESS", "to": "IN_REVIEW"})
	rid, err := c.ReviewRequest(wc, ar, diff, []EvItem{{Kind: "build", Outcome: bOut, EvidenceRefID: bID}, {Kind: "test", Outcome: tOut, EvidenceRefID: tID}})
	if err != nil {
		cleanup()
		return fail(&res, "GATE_FAILED", err.Error())
	}
	res.ReviewID = rid
	add("review.requested", map[string]any{"review_id": rid, "diff_artifact_id": diff})
	add("workflow.waiting_review", map[string]any{"review_id": rid})
	poll := req.ReviewPollMS
	if poll <= 0 {
		poll = 500
	}
	var rs ReviewState
	for {
		if ctx.Err() != nil {
			cleanup()
			return fail(&res, "TIMEOUT", ctx.Err().Error())
		}
		rs, err = c.ReviewGet(rid)
		if err != nil {
			cleanup()
			return fail(&res, "GATE_FAILED", "review: "+err.Error())
		}
		if rs.Status != "PENDING" {
			break
		}
		c.Sleep(time.Duration(poll) * time.Millisecond)
	}
	add("review.decided", map[string]any{"review_id": rid, "status": rs.Status})
	if ok, why := doneGate(EvidenceOutcome{bOut}, EvidenceOutcome{tOut}, rs, diff); !ok {
		cleanup()
		return fail(&res, "GATE_FAILED", why)
	}
	ver, err = c.WorkTransition(wc, "DONE", ver)
	_ = ver
	if err != nil {
		cleanup()
		return fail(&res, "GATE_FAILED", fmt.Sprintf("DONE transition: %v", err))
	}
	add("work.transitioned", map[string]any{"from": "IN_REVIEW", "to": "DONE"})
	cleanup()
	res.Outcome = "DONE"
	res.Reason = ""
	return
}
func fail(r *RunResult, outcome, reason string) RunResult {
	r.Outcome = outcome
	r.Reason = reason
	return *r
}
