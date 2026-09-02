package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/example/agent-native-microkernel/sdk/go/pluginhost"
	"github.com/example/agent-native-microkernel/sdk/go/protocol"
)

func agentRunPayload(provider, wc, path, prompt, writeFile, writeContent string) map[string]any {
	return map[string]any{
		"work_context_id":    wc,
		"workspace_path":     path,
		"prompt":             prompt,
		"provider":           provider,
		"mock_write_file":    writeFile,
		"mock_write_content": writeContent,
	}
}

func runHandler(mkCaps func(rc *pluginhost.RequestContext, e protocol.Envelope) caps) pluginhost.ContextHandler {
	return func(rc *pluginhost.RequestContext, e protocol.Envelope) (any, *protocol.Error) {
		var req RunRequest
		if err := json.Unmarshal(e.Payload, &req); err != nil || req.TaskID == "" || req.Prompt == "" || len(req.BuildCommand) == 0 || len(req.TestCommand) == 0 {
			return nil, &protocol.Error{Code: "INVALID", Message: "task_id, prompt, build_command and test_command are required"}
		}
		ctx := context.Background()
		if rc != nil {
			ctx = rc.Context()
		}
		res := runPipeline(ctx, mkCaps(rc, e), req)
		return map[string]any{"result": res}, nil
	}
}

func getHandler(replay func() ([]JournalRecord, error)) pluginhost.Handler {
	return func(e protocol.Envelope) (any, *protocol.Error) {
		var q struct {
			WorkContextID string `json:"work_context_id"`
			TaskID        string `json:"task_id"`
		}
		if err := json.Unmarshal(e.Payload, &q); err != nil || (q.WorkContextID == "") == (q.TaskID == "") {
			return nil, &protocol.Error{Code: "INVALID", Message: "exactly one of work_context_id or task_id is required"}
		}
		recs, err := replay()
		if err != nil {
			return nil, &protocol.Error{Code: "IO", Message: err.Error(), Retryable: true}
		}
		key := q.WorkContextID
		if key == "" {
			key = q.TaskID
		}
		stage, matched := project(recs, key)
		outcome := ""
		if stage == "DONE" || stage == "SEALED" {
			outcome = "DONE"
		}
		return map[string]any{"stage": stage, "events": matched, "outcome": outcome}, nil
	}
}

func decode(resp protocol.Envelope, v any) error {
	if err := json.Unmarshal(resp.Payload, v); err != nil {
		return fmt.Errorf("decode %s: %w", resp.Capability, err)
	}
	return nil
}

func realCaps(rc *pluginhost.RequestContext, _ protocol.Envelope) caps {
	var repo string
	var wcVersion int
	return caps{
		WorkGet: func(taskID string) (Task, string, int, error) {
			resp, err := rc.Query("work.get", 1, map[string]string{"task_id": taskID}, 30*time.Second)
			if err != nil {
				return Task{}, "", 0, err
			}
			var out struct {
				Task struct {
					ID            string `json:"id"`
					Version       int    `json:"version"`
					WorkContextID string `json:"work_context_id"`
				} `json:"task"`
				WorkContext struct {
					ID      string `json:"id"`
					Repo    string `json:"repo"`
					Version int    `json:"version"`
				} `json:"work_context"`
			}
			if err = decode(resp, &out); err != nil {
				return Task{}, "", 0, err
			}
			repo = out.WorkContext.Repo
			wcVersion = out.WorkContext.Version
			return Task{ID: out.Task.ID, Version: out.Task.Version, WorkContextID: out.Task.WorkContextID}, out.WorkContext.ID, out.Task.Version, nil
		},
		WorkTransition: func(wc, to string, ver int) (int, error) {
			resp, err := rc.Command("work.transition", 1, map[string]any{"work_context_id": wc, "to": to, "expected_version": ver}, 30*time.Second)
			if err != nil {
				return 0, err
			}
			var out struct {
				Task struct {
					Version int `json:"version"`
				} `json:"task"`
			}
			if err = decode(resp, &out); err != nil {
				return 0, err
			}
			return out.Task.Version, nil
		},
		WorkspaceAlloc: func(wc, base string) (string, string, error) {
			resp, err := rc.Command("workspace.allocate", 1, map[string]any{"work_context_id": wc, "repo": repo, "base_ref": base}, 2*time.Minute)
			if err != nil {
				return "", "", err
			}
			var out struct {
				Workspace struct {
					ID   string `json:"id"`
					Path string `json:"path"`
				} `json:"workspace"`
			}
			if err = decode(resp, &out); err != nil {
				return "", "", err
			}
			return out.Workspace.ID, out.Workspace.Path, nil
		},
		WorkspaceRelease: func(id, policy string) error {
			_, err := rc.Command("workspace.release", 1, map[string]string{"workspace_id": id, "policy": policy}, 2*time.Minute)
			return err
		},
		AgentRun: func(provider, wc, path, prompt, writeFile, writeContent string) (string, string, error) {
			stream, accepted, err := rc.CommandStream("agent.run", 1, agentRunPayload(provider, wc, path, prompt, writeFile, writeContent), 30*time.Minute)
			if err != nil {
				return "", "", err
			}
			var a struct {
				AgentRun struct {
					ID string `json:"id"`
				} `json:"agent_run"`
			}
			if err = decode(accepted, &a); err != nil {
				return "", "", err
			}
			for range stream.C {
			}
			// The frame stream closes when the provider's frame goroutine returns;
			// the harness persists the terminal status a moment later (blob.put +
			// RecordCompleted). Poll agent.run.get until the run is no longer
			// RUNNING — same pattern the M1.3 agent smoke uses.
			var status string
			for i := 0; i < 200; i++ {
				resp, err := rc.Query("agent.run.get", 1, map[string]string{"agent_run_id": a.AgentRun.ID}, 30*time.Second)
				if err != nil {
					return a.AgentRun.ID, "", err
				}
				var g struct {
					AgentRun struct {
						Status string `json:"status"`
					} `json:"agent_run"`
				}
				if err = decode(resp, &g); err != nil {
					return a.AgentRun.ID, "", err
				}
				status = g.AgentRun.Status
				if status != "" && status != "RUNNING" {
					break
				}
				time.Sleep(25 * time.Millisecond)
			}
			return a.AgentRun.ID, status, nil
		},
		CollectDiff: func(wc, path string) (string, int, error) {
			resp, err := rc.Command("artifact.collect_diff", 1, map[string]string{"work_context_id": wc, "workspace_path": path}, 2*time.Minute)
			if err != nil {
				return "", 0, err
			}
			var out struct {
				Artifact struct {
					ID      string `json:"id"`
					Summary struct {
						Files []json.RawMessage `json:"files"`
					} `json:"summary"`
				} `json:"artifact"`
			}
			if err = decode(resp, &out); err != nil {
				return "", 0, err
			}
			return out.Artifact.ID, len(out.Artifact.Summary.Files), nil
		},
		ToolRun: func(wc, path, label string, argv []string) (string, string, error) {
			resp, err := rc.Command("tool.run", 1, map[string]any{"work_context_id": wc, "workspace_path": path, "label": label, "command": argv}, 10*time.Minute)
			if err != nil {
				return "", "", err
			}
			var out struct {
				ToolRun struct {
					ID      string `json:"id"`
					Outcome string `json:"outcome"`
				} `json:"tool_run"`
			}
			if err = decode(resp, &out); err != nil {
				return "", "", err
			}
			return out.ToolRun.ID, out.ToolRun.Outcome, nil
		},
		AttachEvidence: func(wc, kind, srcCap, srcID, outcome string) error {
			resp, err := rc.Command("work.attach_evidence", 1, map[string]any{"work_context_id": wc, "kind": kind, "source_capability": srcCap, "source_id": srcID, "outcome": outcome, "expected_version": wcVersion}, 30*time.Second)
			if err != nil {
				return err
			}
			var out struct {
				WorkContext struct {
					Version int `json:"version"`
				} `json:"work_context"`
			}
			if err = decode(resp, &out); err != nil {
				return err
			}
			wcVersion = out.WorkContext.Version
			return nil
		},
		ReviewRequest: func(wc, ar, diff string, snapshot []EvItem) (string, error) {
			resp, err := rc.Command("review.request", 1, map[string]any{"work_context_id": wc, "agent_run_id": ar, "diff_artifact_id": diff, "evidence_snapshot": snapshot}, 30*time.Second)
			if err != nil {
				return "", err
			}
			var out struct {
				Review struct {
					ID string `json:"id"`
				} `json:"review"`
			}
			if err = decode(resp, &out); err != nil {
				return "", err
			}
			return out.Review.ID, nil
		},
		ReviewGet: func(id string) (ReviewState, error) {
			resp, err := rc.Query("review.get", 1, map[string]string{"review_id": id}, 30*time.Second)
			if err != nil {
				return ReviewState{}, err
			}
			var out struct {
				Review struct {
					Status            string `json:"status"`
					DiffArtifactID    string `json:"diff_artifact_id"`
					AcceptanceResults []struct {
						Satisfied bool `json:"satisfied"`
					} `json:"acceptance_results"`
				} `json:"review"`
			}
			if err = decode(resp, &out); err != nil {
				return ReviewState{}, err
			}
			r := ReviewState{Status: out.Review.Status, DiffArtifactID: out.Review.DiffArtifactID}
			for _, a := range out.Review.AcceptanceResults {
				r.AcceptanceResults = append(r.AcceptanceResults, struct{ Satisfied bool }{a.Satisfied})
			}
			return r, nil
		},
		SessionSeal: func(wc, ar, path string) (string, error) {
			resp, err := rc.Command("session.seal", 1, map[string]string{"work_context_id": wc, "agent_run_id": ar, "workspace_path": path}, 2*time.Minute)
			if err != nil {
				return "", err
			}
			var out struct {
				SessionRecord struct {
					ID string `json:"id"`
				} `json:"session_record"`
			}
			if err = decode(resp, &out); err != nil {
				return "", err
			}
			return out.SessionRecord.ID, nil
		},
		AppendEvent: func(typ string, payload map[string]any) (string, error) {
			resp, err := rc.Command("event.journal.append", 1, map[string]any{"type": typ, "source": "org.vibe.workflow.engineering", "payload": payload}, 30*time.Second)
			if err != nil {
				return "", err
			}
			var out struct {
				Record struct {
					ID string `json:"id"`
				} `json:"record"`
			}
			if err = decode(resp, &out); err != nil {
				return "", err
			}
			return out.Record.ID, nil
		},
		Sleep: time.Sleep, Now: func() string { return time.Now().UTC().Format(time.RFC3339Nano) },
	}
}

func replayVia(rc *pluginhost.RequestContext) ([]JournalRecord, error) {
	var all []JournalRecord
	after := 0
	for {
		resp, err := rc.Query("event.journal.replay", 1, map[string]int{"after": after, "limit": 100}, 30*time.Second)
		if err != nil {
			return nil, err
		}
		var page struct {
			Records []JournalRecord `json:"records"`
			Next    int             `json:"next"`
		}
		if err = decode(resp, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Records...)
		if page.Next <= after {
			return all, nil
		}
		after = page.Next
	}
}
