package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/example/agent-native-microkernel/sdk/go/fencing"
	"github.com/example/agent-native-microkernel/sdk/go/pluginhost"
	"github.com/example/agent-native-microkernel/sdk/go/protocol"
)

type runDeps struct {
	Store           *Store
	Providers       map[string]Provider
	DefaultProvider string
	Runs            *runRegistry
	BlobPut         func(payload []byte) (uri string, err error)
	Persist         func(runID, status, rawRef string, frames int, meta json.RawMessage) error
	Now             func() string
}

type agentRunRequest struct {
	WorkContextID    string `json:"work_context_id"`
	WorkspacePath    string `json:"workspace_path"`
	Prompt           string `json:"prompt"`
	Provider         string `json:"provider"`
	MockSteps        int    `json:"mock_steps"`
	MockDelayMS      int    `json:"mock_delay_ms"`
	MockFailAt       int    `json:"mock_fail_at"`
	MockWriteFile    string `json:"mock_write_file"`
	MockWriteContent string `json:"mock_write_content"`
}

func agentRunHandler(base runDeps) pluginhost.ContextHandler {
	return func(rc *pluginhost.RequestContext, e protocol.Envelope) (any, *protocol.Error) {
		var q agentRunRequest
		if err := json.Unmarshal(e.Payload, &q); err != nil {
			return nil, &protocol.Error{Code: "INVALID", Message: "work_context_id, workspace_path and prompt are required"}
		}
		out := make(chan any, 64)
		ar, perr := startAgentRun(rc.Context(), base, e, q, out)
		if perr != nil {
			close(out)
			return nil, perr
		}
		acc := rc.Stream(out)
		return map[string]any{"agent_run": ar, "stream_id": acc.StreamID}, nil
	}
}

func startAgentRun(ctx context.Context, base runDeps, e protocol.Envelope, q agentRunRequest, out chan any) (AgentRun, *protocol.Error) {
	if q.WorkContextID == "" || q.WorkspacePath == "" || q.Prompt == "" {
		return AgentRun{}, &protocol.Error{Code: "INVALID", Message: "work_context_id, workspace_path and prompt are required"}
	}
	name := q.Provider
	if name == "" {
		name = base.DefaultProvider
	}
	if name == "" {
		name = "mock"
	}
	prov, ok := base.Providers[name]
	if !ok {
		return AgentRun{}, &protocol.Error{Code: "INVALID", Message: fmt.Sprintf("unknown provider %q", name)}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if base.Now != nil {
		now = base.Now()
	}
	runID := protocol.NewID("run")
	ar := AgentRun{
		ID: runID, WorkContextID: q.WorkContextID, WorkspacePath: q.WorkspacePath,
		Prompt: q.Prompt, Provider: name, HarnessNativeID: name + "-" + runID,
		Status: StatusRunning, StartedAt: now,
	}
	if err := fencing.WithWriteFence(e, func() error { return base.Store.RecordStarted(ar) }); err != nil {
		return AgentRun{}, &protocol.Error{Code: "IO", Message: err.Error(), Retryable: true}
	}
	d := base
	d.Persist = func(id, status, ref string, n int, meta json.RawMessage) error {
		return fencing.WithWriteFence(e, func() error { return base.Store.RecordCompleted(id, status, ref, n, meta) })
	}
	spec := RunSpec{
		WorkspacePath: q.WorkspacePath, Prompt: q.Prompt,
		MockSteps: q.MockSteps, MockDelayMS: q.MockDelayMS, MockFailAt: q.MockFailAt,
		MockWriteFile: q.MockWriteFile, MockWriteContent: q.MockWriteContent,
	}
	runCtx, runCancel := context.WithCancel(ctx)
	done := make(chan struct{})
	if base.Runs != nil {
		base.Runs.register(runID, runCancel, done)
	}
	go func() {
		defer func() {
			runCancel()
			if base.Runs != nil {
				base.Runs.done(runID)
			}
			close(done)
		}()
		runOnce(runCtx, prov, d, ar, spec, out)
	}()
	return ar, nil
}

func runOnce(ctx context.Context, prov Provider, d runDeps, ar AgentRun, spec RunSpec, out chan any) {
	tr := runProvider(ctx, prov, spec, out)
	uri, err := d.BlobPut(tr.Bytes())
	if err != nil {
		uri = ""
	}
	_ = d.Persist(ar.ID, tr.Result.Status, uri, len(tr.Frames), tr.Result.ProviderMeta)
}

type agentRunIDRequest struct {
	AgentRunID string `json:"agent_run_id"`
}

type agentRunQueryRequest struct {
	WorkContextID string `json:"work_context_id"`
}

func getHandler(s *Store) pluginhost.Handler {
	return func(e protocol.Envelope) (any, *protocol.Error) {
		var q agentRunIDRequest
		if err := json.Unmarshal(e.Payload, &q); err != nil || q.AgentRunID == "" {
			return nil, &protocol.Error{Code: "INVALID", Message: "agent_run_id is required"}
		}
		ar, ok := s.GetByID(q.AgentRunID)
		if !ok {
			return nil, &protocol.Error{Code: "NOT_FOUND", Message: "agent run not found"}
		}
		return map[string]any{"agent_run": ar}, nil
	}
}

func queryHandler(s *Store) pluginhost.Handler {
	return func(e protocol.Envelope) (any, *protocol.Error) {
		var q agentRunQueryRequest
		if err := json.Unmarshal(e.Payload, &q); err != nil || q.WorkContextID == "" {
			return nil, &protocol.Error{Code: "INVALID", Message: "work_context_id is required"}
		}
		return map[string]any{"agent_runs": s.QueryByContext(q.WorkContextID)}, nil
	}
}

func cancelHandler(s *Store, runs *runRegistry) pluginhost.Handler {
	return func(e protocol.Envelope) (any, *protocol.Error) {
		var q agentRunIDRequest
		if err := json.Unmarshal(e.Payload, &q); err != nil || q.AgentRunID == "" {
			return nil, &protocol.Error{Code: "INVALID", Message: "agent_run_id is required"}
		}
		ar, ok := s.GetByID(q.AgentRunID)
		if !ok {
			return nil, &protocol.Error{Code: "NOT_FOUND", Message: "agent run not found"}
		}
		if ar.Status != StatusRunning {
			return nil, &protocol.Error{Code: "CONFLICT", Message: "already " + ar.Status}
		}

		done, live := runs.stop(q.AgentRunID)
		if live {
			select {
			case <-done:
				final, ok := s.GetByID(q.AgentRunID)
				if !ok {
					return nil, &protocol.Error{Code: "NOT_FOUND", Message: "agent run disappeared after cancel"}
				}
				if final.Status == StatusRunning {
					return nil, &protocol.Error{Code: "IO", Retryable: true, Message: "cancel: provider stopped but terminal record was not persisted"}
				}
				return map[string]any{"agent_run": final}, nil
			case <-time.After(30 * time.Second):
				return nil, &protocol.Error{Code: "IO", Retryable: true, Message: "cancel: provider did not stop within 30s"}
			}
		}

		// Not live: registry lost the run (plugin restarted) or it unregistered
		// between GetByID and stop. Re-check, then fall back to a fenced
		// store-only CANCELLED.
		cur, ok := s.GetByID(q.AgentRunID)
		if !ok {
			return nil, &protocol.Error{Code: "NOT_FOUND", Message: "agent run not found"}
		}
		if cur.Status != StatusRunning {
			return nil, &protocol.Error{Code: "CONFLICT", Message: "already " + cur.Status}
		}
		err := fencing.WithWriteFence(e, func() error { return s.RecordCancelled(q.AgentRunID) })
		if err != nil {
			if errors.Is(err, ErrAlreadyTerminal) {
				again, _ := s.GetByID(q.AgentRunID)
				return nil, &protocol.Error{Code: "CONFLICT", Message: "already " + again.Status}
			}
			if errors.Is(err, ErrNotFound) {
				return nil, &protocol.Error{Code: "NOT_FOUND", Message: err.Error()}
			}
			return nil, &protocol.Error{Code: "IO", Message: err.Error(), Retryable: true}
		}
		final, _ := s.GetByID(q.AgentRunID)
		return map[string]any{"agent_run": final}, nil
	}
}
