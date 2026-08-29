package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

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
	lease := map[string]any{"service": "default-tool-runner", "authority": "toolruns-main", "runtime_id": "rt-test", "epoch": 1}
	b, _ := json.Marshal(lease)
	_ = os.WriteFile(filepath.Join(fenceRoot, "default-tool-runner--toolruns-main.json"), b, 0o644)
	return protocol.Envelope{Protocol: 1, MessageID: "m", Kind: protocol.KindCommand, Service: "default-tool-runner", Authority: "toolruns-main", FencingEpoch: 1}
}

func run(t *testing.T, s *Store, dir string, label string, argv []string) ToolRun {
	t.Helper()
	env := fencedEnv(t, dir)
	env.Payload = protocol.NewPayload(map[string]any{
		"work_context_id": "wc-1", "workspace_path": t.TempDir(), "label": label, "command": argv, "timeout_ms": 5000,
	})
	blobN := 0
	h := toolRunHandler(s, func([]byte) (string, error) { blobN++; return "blob://sha256/x", nil })
	out, perr := h(&pluginhost.RequestContext{}, env)
	if perr != nil {
		t.Fatalf("%s: %+v", label, perr)
	}
	if blobN != 2 {
		t.Fatalf("%s: blob.put called %d times, want 2 (stdout+stderr)", label, blobN)
	}
	var r struct {
		ToolRun ToolRun `json:"tool_run"`
	}
	b, _ := json.Marshal(out)
	_ = json.Unmarshal(b, &r)
	return r.ToolRun
}

func TestToolRunPassAndFail(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	pass := run(t, s, dir, "build", []string{"sh", "-c", "echo building; exit 0"})
	if pass.Outcome != "PASS" || pass.ExitCode != 0 || pass.Fingerprint == "" || pass.StdoutURI == "" {
		t.Fatalf("pass run: %+v", pass)
	}
	fail := run(t, s, dir, "test", []string{"sh", "-c", "echo testing; exit 1"})
	if fail.Outcome != "FAIL" || fail.ExitCode != 1 {
		t.Fatalf("fail run: %+v", fail)
	}
	if _, ok := s.GetByID(pass.ID); !ok {
		t.Fatal("pass run not recorded")
	}
}

func TestToolRunHandlerRejectsEmptyCommand(t *testing.T) {
	s, _ := Load(t.TempDir())
	h := toolRunHandler(s, func([]byte) (string, error) { return "", nil })
	env := fencedEnv(t, t.TempDir())
	env.Payload = protocol.NewPayload(map[string]any{"work_context_id": "wc-1", "workspace_path": "/tmp", "label": "x", "command": []string{}})
	_, perr := h(&pluginhost.RequestContext{}, env)
	if perr == nil || perr.Code != "INVALID" {
		t.Fatalf("want INVALID, got %+v", perr)
	}
}
