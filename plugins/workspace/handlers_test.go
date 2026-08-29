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

func allocate(t *testing.T, s *Store, dir, repo, wc string) WorkspaceRef {
	t.Helper()
	env := fencedEnv(t, dir)
	env.Payload = protocol.NewPayload(map[string]string{"work_context_id": wc, "repo": repo})
	out, perr := allocateHandler(s)(env)
	if perr != nil {
		t.Fatal(perr)
	}
	var r struct {
		Workspace WorkspaceRef `json:"workspace"`
	}
	b, _ := json.Marshal(out)
	_ = json.Unmarshal(b, &r)
	return r.Workspace
}

func TestReleasePreserveKeepsWorktreeOnDisk(t *testing.T) {
	repo, _ := scratchRepo(t)
	dir := t.TempDir()
	s, _ := Load(dir)
	ws := allocate(t, s, dir, repo, "wc-1")

	env := fencedEnv(t, dir)
	env.Payload = protocol.NewPayload(map[string]string{"workspace_id": ws.ID, "policy": "preserve"})
	out, perr := releaseHandler(s)(env)
	if perr != nil {
		t.Fatalf("release: %+v", perr)
	}
	var r struct {
		Workspace WorkspaceRef `json:"workspace"`
	}
	b, _ := json.Marshal(out)
	_ = json.Unmarshal(b, &r)
	if r.Workspace.Status != StatusReleased || r.Workspace.ReleasePolicy != "preserve" {
		t.Fatalf("released ref: %+v", r.Workspace)
	}
	if _, err := os.Stat(ws.Path); err != nil {
		t.Fatalf("preserve policy must keep the worktree dir: %v", err)
	}
}

func TestReleaseDeleteRemovesWorktree(t *testing.T) {
	repo, _ := scratchRepo(t)
	dir := t.TempDir()
	s, _ := Load(dir)
	ws := allocate(t, s, dir, repo, "wc-1")

	env := fencedEnv(t, dir)
	env.Payload = protocol.NewPayload(map[string]string{"workspace_id": ws.ID, "policy": "delete"})
	if _, perr := releaseHandler(s)(env); perr != nil {
		t.Fatalf("release delete: %+v", perr)
	}
	if _, err := os.Stat(ws.Path); !os.IsNotExist(err) {
		t.Fatalf("delete policy must remove the worktree dir")
	}
}

func TestGetBySelector(t *testing.T) {
	repo, _ := scratchRepo(t)
	dir := t.TempDir()
	s, _ := Load(dir)
	ws := allocate(t, s, dir, repo, "wc-1")

	for _, sel := range []map[string]string{{"workspace_id": ws.ID}, {"work_context_id": "wc-1"}} {
		out, perr := getHandler(s)(protocol.Envelope{Payload: protocol.NewPayload(sel)})
		if perr != nil {
			t.Fatalf("get %v: %+v", sel, perr)
		}
		var r struct {
			Workspace WorkspaceRef `json:"workspace"`
		}
		b, _ := json.Marshal(out)
		_ = json.Unmarshal(b, &r)
		if r.Workspace.ID != ws.ID {
			t.Fatalf("get %v returned %s", sel, r.Workspace.ID)
		}
	}
	_, perr := getHandler(s)(protocol.Envelope{Payload: protocol.NewPayload(map[string]string{})})
	if perr == nil || perr.Code != "INVALID" {
		t.Fatalf("no selector must be INVALID, got %+v", perr)
	}
}
