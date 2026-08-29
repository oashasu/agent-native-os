package main

import (
	"context"
	"encoding/json"
	"time"

	"github.com/example/agent-native-microkernel/sdk/go/fencing"
	"github.com/example/agent-native-microkernel/sdk/go/pluginhost"
	"github.com/example/agent-native-microkernel/sdk/go/protocol"
)

type runDeps struct {
	Store   *Store
	Prov    Provider
	BlobPut func(payload []byte) (uri string, err error)
	Persist func(runID, status, rawRef string, frames int, meta json.RawMessage) error
	Now     func() string
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
		if err := json.Unmarshal(e.Payload, &q); err != nil || q.WorkContextID == "" || q.WorkspacePath == "" || q.Prompt == "" {
			return nil, &protocol.Error{Code: "INVALID", Message: "work_context_id, workspace_path and prompt are required"}
		}
		if q.Provider != "" && q.Provider != "mock" {
			return nil, &protocol.Error{Code: "INVALID", Message: "only mock provider is available in M1.3"}
		}
		provider := q.Provider
		if provider == "" {
			provider = base.Prov.Name()
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		if base.Now != nil {
			now = base.Now()
		}
		runID := protocol.NewID("run")
		ar := AgentRun{
			ID:              runID,
			WorkContextID:   q.WorkContextID,
			WorkspacePath:   q.WorkspacePath,
			Prompt:          q.Prompt,
			Provider:        provider,
			HarnessNativeID: "mock-" + runID,
			Status:          StatusRunning,
			StartedAt:       now,
		}
		if err := fencing.WithWriteFence(e, func() error { return base.Store.RecordStarted(ar) }); err != nil {
			return nil, &protocol.Error{Code: "IO", Message: err.Error(), Retryable: true}
		}
		d := base
		d.Persist = func(id, status, ref string, n int, meta json.RawMessage) error {
			return fencing.WithWriteFence(e, func() error { return base.Store.RecordCompleted(id, status, ref, n, meta) })
		}
		spec := RunSpec{
			WorkspacePath:    q.WorkspacePath,
			Prompt:           q.Prompt,
			MockSteps:        q.MockSteps,
			MockDelayMS:      q.MockDelayMS,
			MockFailAt:       q.MockFailAt,
			MockWriteFile:    q.MockWriteFile,
			MockWriteContent: q.MockWriteContent,
		}
		out := make(chan any, 64)
		go runOnce(rc.Context(), d, ar, spec, out)
		acc := rc.Stream(out)
		return map[string]any{"agent_run": ar, "stream_id": acc.StreamID}, nil
	}
}

func runOnce(ctx context.Context, d runDeps, ar AgentRun, spec RunSpec, out chan any) {
	tr := runProvider(ctx, d.Prov, spec, out)
	uri, err := d.BlobPut(tr.Bytes())
	if err != nil {
		uri = ""
	}
	_ = d.Persist(ar.ID, tr.Result.Status, uri, len(tr.Frames), tr.Result.ProviderMeta)
}
