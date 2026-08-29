package fencing

import (
	"encoding/json"
	"github.com/example/agent-native-microkernel/sdk/go/protocol"
	"os"
	"testing"
)

func TestWithWriteFenceRejectsStaleEpochAndRuntime(t *testing.T) {
	root := t.TempDir()
	t.Setenv("VIBE_FENCE_ROOT", root)
	t.Setenv("VIBE_RUNTIME_ID", "runtime-new")
	leasePath, _ := Paths(root, "svc", "auth")
	b, _ := json.Marshal(Lease{Service: "svc", Authority: "auth", RuntimeID: "runtime-new", Epoch: 9})
	if err := os.WriteFile(leasePath, b, 0644); err != nil {
		t.Fatal(err)
	}
	ran := false
	if err := WithWriteFence(protocol.Envelope{Service: "svc", Authority: "auth", FencingEpoch: 9}, func() error { ran = true; return nil }); err != nil || !ran {
		t.Fatalf("valid fence rejected: %v", err)
	}
	ran = false
	if err := WithWriteFence(protocol.Envelope{Service: "svc", Authority: "auth", FencingEpoch: 8}, func() error { ran = true; return nil }); err == nil || ran {
		t.Fatal("stale epoch executed")
	}
	t.Setenv("VIBE_RUNTIME_ID", "runtime-old")
	if err := WithWriteFence(protocol.Envelope{Service: "svc", Authority: "auth", FencingEpoch: 9}, func() error { return nil }); err == nil {
		t.Fatal("stale runtime executed")
	}
}
