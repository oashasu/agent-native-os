package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/example/agent-native-microkernel/sdk/go/contracts/eventv1"
	"github.com/example/agent-native-microkernel/sdk/go/protocol"
)

// fenceEnv builds the stateful metadata WithWriteFence requires, and writes the
// matching lease file the same way the kernel registry would on writer promotion.
func fenceEnv(t *testing.T, dir string) protocol.Envelope {
	t.Helper()
	fenceRoot := filepath.Join(dir, ".fences")
	if err := os.MkdirAll(fenceRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VIBE_DATA_DIR", dir)
	t.Setenv("VIBE_RUNTIME_ID", "rt-test")
	t.Setenv("VIBE_FENCE_ROOT", fenceRoot)
	lease := map[string]any{"service": "default-event-journal", "authority": "journal-main", "runtime_id": "rt-test", "epoch": 1}
	b, _ := json.Marshal(lease)
	if err := os.WriteFile(filepath.Join(fenceRoot, "default-event-journal--journal-main.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	return protocol.Envelope{
		Protocol: 1, MessageID: "m1", Kind: protocol.KindCommand,
		Capability: "event.journal.append", Major: 1,
		Service: "default-event-journal", Authority: "journal-main", FencingEpoch: 1,
		Caller: "org.vibe.test", Principal: "tester", ActorChain: []string{"tester"},
		TraceID: "trace-1", CorrelationID: "corr-1",
	}
}

func TestAppendThenReplayReturnsRecordsInOrder(t *testing.T) {
	dir := t.TempDir()
	j := &journal{path: filepath.Join(dir, "events.jsonl")}
	env := fenceEnv(t, dir)

	for _, typ := range []string{"work.created", "agent.run.started", "work.done"} {
		env.Payload = protocol.NewPayload(eventv1.AppendRequest{Type: typ, Source: "org.vibe.test", Payload: protocol.NewPayload(map[string]string{"t": typ})})
		if _, perr := appendHandler(j)(env); perr != nil {
			t.Fatalf("append %s: %+v", typ, perr)
		}
	}

	out, perr := replayHandler(j)(protocol.Envelope{Protocol: 1, MessageID: "q", Kind: protocol.KindQuery, Payload: protocol.NewPayload(eventv1.ReplayRequest{})})
	if perr != nil {
		t.Fatalf("replay: %+v", perr)
	}
	resp := out.(eventv1.ReplayResponse)
	if len(resp.Records) != 3 || resp.Records[0].Type != "work.created" || resp.Records[2].Type != "work.done" {
		t.Fatalf("unexpected replay: %+v", resp.Records)
	}
	if resp.Records[1].PreviousSHA256 != resp.Records[0].SHA256 {
		t.Fatalf("hash chain broken between record 0 and 1")
	}
}

func TestReplayDetectsTampering(t *testing.T) {
	dir := t.TempDir()
	j := &journal{path: filepath.Join(dir, "events.jsonl")}
	env := fenceEnv(t, dir)
	env.Payload = protocol.NewPayload(eventv1.AppendRequest{Type: "work.created", Source: "org.vibe.test", Payload: protocol.NewPayload(map[string]int{"n": 1})})
	if _, perr := appendHandler(j)(env); perr != nil {
		t.Fatalf("append: %+v", perr)
	}

	// Flip a byte in the persisted payload.
	raw, _ := os.ReadFile(j.path)
	tampered := []byte(string(raw))
	for i, c := range tampered {
		if c == '1' {
			tampered[i] = '2'
			break
		}
	}
	if err := os.WriteFile(j.path, tampered, 0o644); err != nil {
		t.Fatal(err)
	}

	fresh := &journal{path: j.path}
	_, perr := replayHandler(fresh)(protocol.Envelope{Protocol: 1, MessageID: "q", Kind: protocol.KindQuery, Payload: protocol.NewPayload(eventv1.ReplayRequest{})})
	if perr == nil || perr.Code != "INTEGRITY_ERROR" {
		t.Fatalf("tampered journal replayed without INTEGRITY_ERROR: %+v", perr)
	}
}
