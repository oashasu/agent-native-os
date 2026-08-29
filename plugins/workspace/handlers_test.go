package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/example/agent-native-microkernel/sdk/go/protocol"
)

func fencedEnv(t *testing.T, dir string) protocol.Envelope {
	t.Helper()
	fenceRoot := filepath.Join(dir, ".fences")
	_ = os.MkdirAll(fenceRoot, 0o755)
	t.Setenv("VIBE_DATA_DIR", dir)
	t.Setenv("VIBE_RUNTIME_ID", "rt-test")
	t.Setenv("VIBE_FENCE_ROOT", fenceRoot)
	lease := map[string]any{"service": "default-workspace", "authority": "workspace-main", "runtime_id": "rt-test", "epoch": 1}
	b, _ := json.Marshal(lease)
	_ = os.WriteFile(filepath.Join(fenceRoot, "default-workspace--workspace-main.json"), b, 0o644)
	return protocol.Envelope{
		Protocol: 1, MessageID: "m", Kind: protocol.KindCommand,
		Service: "default-workspace", Authority: "workspace-main", FencingEpoch: 1,
	}
}

func TestAllocateCreatesWorktreeOnNewBranch(t *testing.T) {
	repo, head := scratchRepo(t)
	dir := t.TempDir()
	s, _ := Load(dir)
	env := fencedEnv(t, dir)
	env.Payload = protocol.NewPayload(map[string]string{"work_context_id": "wc-42", "repo": repo})

	out, perr := allocateHandler(s)(env)
	if perr != nil {
		t.Fatalf("allocate: %+v", perr)
	}
	var r struct {
		Workspace WorkspaceRef `json:"workspace"`
	}
	b, _ := json.Marshal(out)
	_ = json.Unmarshal(b, &r)
	ws := r.Workspace
	if ws.WorkContextID != "wc-42" || ws.BaseCommit != head || ws.Status != StatusAllocated {
		t.Fatalf("workspace: %+v", ws)
	}
	if br := gitOut(t, ws.Path, "rev-parse", "--abbrev-ref", "HEAD"); br != ws.Branch {
		t.Fatalf("worktree on %q, ref says %q", br, ws.Branch)
	}
	if _, ok := s.GetByID(ws.ID); !ok {
		t.Fatalf("allocate did not record the workspace")
	}
}

func TestAllocateRejectsNonRepo(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	env := fencedEnv(t, dir)
	env.Payload = protocol.NewPayload(map[string]string{"work_context_id": "wc-1", "repo": t.TempDir()})
	_, perr := allocateHandler(s)(env)
	if perr == nil || perr.Code != "GIT_ERROR" {
		t.Fatalf("want GIT_ERROR for a non-repo, got %+v", perr)
	}
}

func TestAllocateRejectsMissingFields(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	env := fencedEnv(t, dir)
	env.Payload = protocol.NewPayload(map[string]string{"work_context_id": "wc-1"})
	_, perr := allocateHandler(s)(env)
	if perr == nil || perr.Code != "INVALID" {
		t.Fatalf("want INVALID, got %+v", perr)
	}
}
