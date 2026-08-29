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
		Store: s, Prov: MockProvider{}, Now: func() string { return "t0" },
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
		runOnce(context.Background(), deps, ar, RunSpec{WorkspacePath: ws, Prompt: "p", MockSteps: 3, MockDelayMS: 1, MockWriteFile: "f.txt", MockWriteContent: "z\n"}, out)
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
	deps := runDeps{Store: s, Prov: MockProvider{}, BlobPut: func([]byte) (string, error) { return "", nil }, Persist: func(string, string, string, int, json.RawMessage) error { return nil }, Now: func() string { return "t0" }}
	h := agentRunHandler(deps)
	_, perr := h(&pluginhost.RequestContext{}, protocol.Envelope{Payload: protocol.NewPayload(map[string]string{"prompt": "p"})})
	if perr == nil || perr.Code != "INVALID" {
		t.Fatalf("want INVALID, got %+v", perr)
	}
}
