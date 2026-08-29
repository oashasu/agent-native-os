package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/example/agent-native-microkernel/sdk/go/fencing"
	"github.com/example/agent-native-microkernel/sdk/go/pluginhost"
	"github.com/example/agent-native-microkernel/sdk/go/protocol"
)

type toolRunRequest struct {
	WorkContextID string   `json:"work_context_id"`
	WorkspacePath string   `json:"workspace_path"`
	Label         string   `json:"label"`
	Command       []string `json:"command"`
	EnvAllowlist  []string `json:"env_allowlist"`
	TimeoutMS     int      `json:"timeout_ms"`
}

func runTool(ctx context.Context, s *Store, blobPut func([]byte) (string, error), req toolRunRequest) (ToolRun, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	effectiveTimeout := req.TimeoutMS
	if effectiveTimeout <= 0 {
		effectiveTimeout = 600000
	}
	started := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := runCommand(ctx, req.WorkspacePath, req.Command, req.EnvAllowlist, effectiveTimeout)
	if err != nil {
		return ToolRun{}, err
	}
	stdoutURI, err := blobPut(res.Stdout)
	if err != nil {
		return ToolRun{}, fmt.Errorf("stdout blob.put: %w", err)
	}
	stderrURI, err := blobPut(res.Stderr)
	if err != nil {
		return ToolRun{}, fmt.Errorf("stderr blob.put: %w", err)
	}
	outcome := "PASS"
	if res.ExitCode != 0 || res.TimedOut {
		outcome = "FAIL"
	}
	tr := ToolRun{
		ID:            protocol.NewID("trun"),
		WorkContextID: req.WorkContextID,
		WorkspacePath: req.WorkspacePath,
		Label:         req.Label,
		Command:       append([]string(nil), req.Command...),
		Cwd:           req.WorkspacePath,
		EnvAllowlist:  append([]string(nil), req.EnvAllowlist...),
		TimeoutMS:     effectiveTimeout,
		ExitCode:      res.ExitCode,
		Outcome:       outcome,
		StdoutURI:     stdoutURI,
		StderrURI:     stderrURI,
		Fingerprint:   fingerprint(req.Command, req.EnvAllowlist, req.WorkspacePath),
		StartedAt:     started,
		EndedAt:       time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := s.Record(tr); err != nil {
		return ToolRun{}, err
	}
	return tr, nil
}

func toolRunHandler(s *Store, blobPut func([]byte) (string, error)) pluginhost.ContextHandler {
	return func(rc *pluginhost.RequestContext, e protocol.Envelope) (any, *protocol.Error) {
		var q toolRunRequest
		if err := json.Unmarshal(e.Payload, &q); err != nil || q.WorkContextID == "" || q.WorkspacePath == "" || q.Label == "" || len(q.Command) == 0 {
			return nil, &protocol.Error{Code: "INVALID", Message: "work_context_id, workspace_path, label and non-empty command are required"}
		}
		ctx := context.Background()
		if rc != nil && rc.Context() != nil {
			ctx = rc.Context()
		}
		var tr ToolRun
		err := fencing.WithWriteFence(e, func() error {
			var err error
			tr, err = runTool(ctx, s, blobPut, q)
			return err
		})
		if err != nil {
			return nil, &protocol.Error{Code: "IO", Message: err.Error(), Retryable: true}
		}
		return map[string]any{"tool_run": tr}, nil
	}
}

type toolRunIDRequest struct {
	ToolRunID string `json:"tool_run_id"`
}

type toolRunQueryRequest struct {
	WorkContextID string `json:"work_context_id"`
}

func getHandler(s *Store) pluginhost.Handler {
	return func(e protocol.Envelope) (any, *protocol.Error) {
		var q toolRunIDRequest
		if err := json.Unmarshal(e.Payload, &q); err != nil || q.ToolRunID == "" {
			return nil, &protocol.Error{Code: "INVALID", Message: "tool_run_id is required"}
		}
		tr, ok := s.GetByID(q.ToolRunID)
		if !ok {
			return nil, &protocol.Error{Code: "NOT_FOUND", Message: ErrNotFound.Error()}
		}
		return map[string]any{"tool_run": tr}, nil
	}
}

func queryHandler(s *Store) pluginhost.Handler {
	return func(e protocol.Envelope) (any, *protocol.Error) {
		var q toolRunQueryRequest
		if err := json.Unmarshal(e.Payload, &q); err != nil || q.WorkContextID == "" {
			return nil, &protocol.Error{Code: "INVALID", Message: "work_context_id is required"}
		}
		runs := s.QueryByContext(q.WorkContextID)
		if runs == nil {
			runs = []ToolRun{}
		}
		return map[string]any{"tool_runs": runs}, nil
	}
}
