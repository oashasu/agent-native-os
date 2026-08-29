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
	lease := map[string]any{"service": "default-artifact", "authority": "artifact-main", "runtime_id": "rt-test", "epoch": 1}
	b, _ := json.Marshal(lease)
	_ = os.WriteFile(filepath.Join(fenceRoot, "default-artifact--artifact-main.json"), b, 0o644)
	return protocol.Envelope{Protocol: 1, MessageID: "m", Kind: protocol.KindCommand, Service: "default-artifact", Authority: "artifact-main", FencingEpoch: 1}
}

func TestCollectDiffHandlerRecordsArtifact(t *testing.T) {
	ws := gitRepoWithChange(t)
	dir := t.TempDir()
	s, _ := Load(dir)
	var putN int
	put := func(b []byte) (string, error) { putN++; return "blob://sha256/deadbeef", nil }
	env := fencedEnv(t, dir)
	env.Payload = protocol.NewPayload(map[string]string{"work_context_id": "wc-1", "workspace_path": ws})

	out, perr := collectDiffHandler(s, put)(env)
	if perr != nil {
		t.Fatalf("collect: %+v", perr)
	}
	var r struct {
		Artifact Artifact `json:"artifact"`
	}
	b, _ := json.Marshal(out)
	_ = json.Unmarshal(b, &r)
	if r.Artifact.Kind != "diff" || r.Artifact.BlobURI != "blob://sha256/deadbeef" || r.Artifact.Summary.FilesChanged < 1 {
		t.Fatalf("artifact: %+v", r.Artifact)
	}
	if putN != 1 {
		t.Fatalf("blob.put called %d times, want 1", putN)
	}
	if _, ok := s.GetByID(r.Artifact.ID); !ok {
		t.Fatal("artifact not recorded")
	}
}

func TestCollectDiffHandlerRejectsMissingFields(t *testing.T) {
	s, _ := Load(t.TempDir())
	env := fencedEnv(t, t.TempDir())
	env.Payload = protocol.NewPayload(map[string]string{"work_context_id": "wc-1"})
	_, perr := collectDiffHandler(s, func([]byte) (string, error) { return "", nil })(env)
	if perr == nil || perr.Code != "INVALID" {
		t.Fatalf("want INVALID, got %+v", perr)
	}
}
