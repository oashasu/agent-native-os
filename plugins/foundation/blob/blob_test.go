package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/agent-native-microkernel/sdk/go/protocol"
)

func fenced(t *testing.T, dir string) protocol.Envelope {
	t.Helper()
	fenceRoot := filepath.Join(dir, ".fences")
	if err := os.MkdirAll(fenceRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VIBE_DATA_DIR", dir)
	t.Setenv("VIBE_RUNTIME_ID", "rt-test")
	t.Setenv("VIBE_FENCE_ROOT", fenceRoot)
	lease := map[string]any{"service": "default-blob", "authority": "blob-main", "runtime_id": "rt-test", "epoch": 1}
	b, _ := json.Marshal(lease)
	if err := os.WriteFile(filepath.Join(fenceRoot, "default-blob--blob-main.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	return protocol.Envelope{
		Protocol: 1, MessageID: "m", Kind: protocol.KindCommand,
		Service: "default-blob", Authority: "blob-main", FencingEpoch: 1,
	}
}

func TestPutIsContentAddressedAndIdempotent(t *testing.T) {
	dir := t.TempDir()
	s := &store{root: dir}
	env := fenced(t, dir)
	payload := []byte("hello M1.1")

	env.Payload = protocol.NewPayload(map[string]string{"content_base64": base64.StdEncoding.EncodeToString(payload)})
	out1, perr := putHandler(s)(env)
	if perr != nil {
		t.Fatalf("put: %+v", perr)
	}
	r1 := out1.(putResponse)
	if r1.Existed || r1.Size != len(payload) || r1.URI == "" {
		t.Fatalf("first put: %+v", r1)
	}

	out2, _ := putHandler(s)(env)
	r2 := out2.(putResponse)
	if !r2.Existed || r2.URI != r1.URI {
		t.Fatalf("second put of same bytes must report existed with the same uri: %+v", r2)
	}

	getEnv := protocol.Envelope{Payload: protocol.NewPayload(map[string]string{"uri": r1.URI})}
	gout, gperr := getHandler(s)(getEnv)
	if gperr != nil {
		t.Fatalf("get: %+v", gperr)
	}
	got, _ := base64.StdEncoding.DecodeString(gout.(getResponse).ContentBase64)
	if string(got) != string(payload) {
		t.Fatalf("round-trip mismatch: %q", got)
	}
}

func TestGetUnknownURIIsNotFound(t *testing.T) {
	dir := t.TempDir()
	s := &store{root: dir}
	_, perr := getHandler(s)(protocol.Envelope{Payload: protocol.NewPayload(map[string]string{"uri": "blob://sha256/" + strings.Repeat("0", 64)})})
	if perr == nil || perr.Code != "NOT_FOUND" {
		t.Fatalf("want NOT_FOUND for unknown uri, got %+v", perr)
	}
}
