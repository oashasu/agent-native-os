package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/example/agent-native-microkernel/sdk/go/pluginhost"
	"github.com/example/agent-native-microkernel/sdk/go/protocol"
)

func fencedEnv(t *testing.T, dir string) protocol.Envelope {
	t.Helper()
	fenceRoot := filepath.Join(dir, ".fences")
	_ = os.MkdirAll(fenceRoot, 0o755)
	t.Setenv("VIBE_DATA_DIR", dir)
	t.Setenv("VIBE_RUNTIME_ID", "rt-test")
	t.Setenv("VIBE_FENCE_ROOT", fenceRoot)
	lease := map[string]any{"service": "default-agent-harness", "authority": "agent-runs-main", "runtime_id": "rt-test", "epoch": 1}
	b, _ := json.Marshal(lease)
	_ = os.WriteFile(filepath.Join(fenceRoot, "default-agent-harness--agent-runs-main.json"), b, 0o644)
	return protocol.Envelope{Protocol: 1, MessageID: "m", Kind: protocol.KindCommand, Service: "default-agent-harness", Authority: "agent-runs-main", FencingEpoch: 1}
}

func TestRunOncePersistsTerminalRun(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	ws := t.TempDir()
	var putBytes []byte
	var mu sync.Mutex
	deps := runDeps{
		Store: s, Now: func() string { return "t0" },
		BlobPut: func(b []byte) (string, error) {
			mu.Lock()
			putBytes = b
			mu.Unlock()
			return "blob://sha256/deadbeef", nil
		},
		Persist: func(id, status, ref string, n int, meta json.RawMessage) error {
			return s.RecordCompleted(id, status, ref, n, meta)
		},
	}
	ar := AgentRun{ID: "run-x", WorkContextID: "wc-1", WorkspacePath: ws, Prompt: "p", Provider: "mock", Status: StatusRunning, StartedAt: "t0"}
	if err := s.RecordStarted(ar); err != nil {
		t.Fatal(err)
	}
	out := make(chan any, 64)
	done := make(chan struct{})
	go func() {
		runOnce(context.Background(), MockProvider{}, deps, ar, RunSpec{WorkspacePath: ws, Prompt: "p", MockSteps: 3, MockDelayMS: 1, MockWriteFile: "f.txt", MockWriteContent: "z\n"}, out)
		close(done)
	}()
	for range out {
	}
	<-done

	got, _ := s.GetByID("run-x")
	if got.Status != StatusCompleted || got.RawSessionRef != "blob://sha256/deadbeef" || got.FrameCount != 3 {
		t.Fatalf("terminal run: %+v", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(putBytes) == 0 {
		t.Fatal("transcript was not sent to blob.put")
	}
	if b, _ := os.ReadFile(filepath.Join(ws, "f.txt")); string(b) != "z\n" {
		t.Fatalf("workspace change missing: %q", b)
	}
	_ = pluginhost.Host{}
	_ = fencedEnv
	_ = time.Second
}

func TestAgentRunHandlerRejectsMissingFields(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	deps := runDeps{Store: s, Providers: map[string]Provider{"mock": MockProvider{}}, DefaultProvider: "mock", Runs: newRunRegistry(), BlobPut: func([]byte) (string, error) { return "", nil }, Persist: func(string, string, string, int, json.RawMessage) error { return nil }, Now: func() string { return "t0" }}
	h := agentRunHandler(deps)
	_, perr := h(&pluginhost.RequestContext{}, protocol.Envelope{Payload: protocol.NewPayload(map[string]string{"prompt": "p"})})
	if perr == nil || perr.Code != "INVALID" {
		t.Fatalf("want INVALID, got %+v", perr)
	}
}

// agentDeps builds runDeps whose Store is the SAME one the fenced envelope
// expects (fencedEnv wrote the lease under dir; Load(dir) reads/writes there).
func agentDeps(t *testing.T, dir string, providers map[string]Provider) (runDeps, *Store) {
	t.Helper()
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return runDeps{
		Store: s, Providers: providers, DefaultProvider: "mock", Runs: newRunRegistry(),
		Now:     func() string { return "t0" },
		BlobPut: func([]byte) (string, error) { return "blob://sha256/x", nil },
	}, s
}

func TestStartAgentRunProviderSelection(t *testing.T) {
	dir := t.TempDir()
	env := fencedEnv(t, dir)
	deps, _ := agentDeps(t, dir, map[string]Provider{"mock": MockProvider{}})

	out := make(chan any, 64)
	q := agentRunRequest{WorkContextID: "wc-1", WorkspacePath: t.TempDir(), Prompt: "p", MockSteps: 2, MockDelayMS: 1}
	ar, perr := startAgentRun(context.Background(), deps, env, q, out)
	if perr != nil {
		t.Fatalf("perr=%+v", perr)
	}
	for range out {
	}
	if ar.Provider != "mock" || ar.HarnessNativeID != "mock-"+ar.ID {
		t.Fatalf("ar=%+v", ar)
	}
}

func TestStartAgentRunUnknownProviderNoRecord(t *testing.T) {
	dir := t.TempDir()
	env := fencedEnv(t, dir)
	deps, store := agentDeps(t, dir, map[string]Provider{"mock": MockProvider{}})
	out := make(chan any, 1)
	_, perr := startAgentRun(context.Background(), deps, env, agentRunRequest{WorkContextID: "wc-1", WorkspacePath: t.TempDir(), Prompt: "p", Provider: "bogus"}, out)
	if perr == nil || perr.Code != "INVALID" {
		t.Fatalf("want INVALID, got %+v", perr)
	}
	if len(store.QueryByContext("wc-1")) != 0 {
		t.Fatal("orphan RUNNING record written for unknown provider")
	}
}

func TestGetAndQueryHandlers(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	_ = s.RecordStarted(AgentRun{ID: "r1", WorkContextID: "wc-1", Status: StatusRunning, StartedAt: "t1"})
	_ = s.RecordCompleted("r1", StatusCompleted, "blob://sha256/x", 2, nil)

	gout, perr := getHandler(s)(protocol.Envelope{Payload: protocol.NewPayload(map[string]string{"agent_run_id": "r1"})})
	if perr != nil {
		t.Fatalf("get: %+v", perr)
	}
	gb, _ := json.Marshal(gout)
	var gr struct {
		AgentRun AgentRun `json:"agent_run"`
	}
	_ = json.Unmarshal(gb, &gr)
	if gr.AgentRun.Status != StatusCompleted {
		t.Fatalf("get returned: %+v", gr.AgentRun)
	}

	qout, _ := queryHandler(s)(protocol.Envelope{Payload: protocol.NewPayload(map[string]string{"work_context_id": "wc-1"})})
	qb, _ := json.Marshal(qout)
	var qr struct {
		AgentRuns []AgentRun `json:"agent_runs"`
	}
	_ = json.Unmarshal(qb, &qr)
	if len(qr.AgentRuns) != 1 {
		t.Fatalf("query returned %d runs", len(qr.AgentRuns))
	}

	_, perr = getHandler(s)(protocol.Envelope{Payload: protocol.NewPayload(map[string]string{"agent_run_id": "nope"})})
	if perr == nil || perr.Code != "NOT_FOUND" {
		t.Fatalf("want NOT_FOUND, got %+v", perr)
	}
}

func TestCancelHandlerMarksRunCancelled(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	env := fencedEnv(t, dir)
	_ = s.RecordStarted(AgentRun{ID: "r1", Status: StatusRunning, StartedAt: "t1"})
	env.Payload = protocol.NewPayload(map[string]string{"agent_run_id": "r1"})
	out, perr := cancelHandler(s)(env)
	if perr != nil {
		t.Fatalf("cancel: %+v", perr)
	}
	_ = out
	got, _ := s.GetByID("r1")
	if got.Status != StatusCancelled {
		t.Fatalf("run not cancelled: %+v", got)
	}
	_, perr = cancelHandler(s)(env)
	if perr == nil || perr.Code != "CONFLICT" {
		t.Fatalf("second cancel: %+v", perr)
	}
}
