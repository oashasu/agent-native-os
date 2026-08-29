package main

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/example/agent-native-microkernel/sdk/go/protocol"
)

func fakeGateway(t *testing.T) string {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "k.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		var wr struct {
			Identity string            `json:"identity"`
			Token    string            `json:"token"`
			Envelope protocol.Envelope `json:"envelope"`
		}
		_ = json.NewDecoder(c).Decode(&wr)
		resp := protocol.Envelope{
			Protocol: 1, Kind: protocol.KindResult, ReplyTo: wr.Envelope.MessageID,
			Payload: protocol.NewPayload(map[string]any{"seen_identity": wr.Identity, "cap": wr.Envelope.Capability, "caller": wr.Envelope.Caller, "principal": wr.Envelope.Principal}),
		}
		_ = json.NewEncoder(c).Encode(resp)
	}()
	return sock
}

func TestInvokeSendsIdentityAndClearsTCBFields(t *testing.T) {
	sock := fakeGateway(t)
	req := protocol.Envelope{Kind: protocol.KindCommand, Capability: "work.create", Major: 1, Principal: "attacker", Caller: "attacker"}
	resp, err := invoke(sock, "local-cli", "tok", req)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	var p map[string]any
	_ = json.Unmarshal(resp.Payload, &p)
	if p["seen_identity"] != "local-cli" || p["cap"] != "work.create" {
		t.Fatalf("gateway saw: %v", p)
	}
	if p["caller"] != "" || p["principal"] != "" {
		t.Fatalf("TCB fields were not cleared: %v", p)
	}
	_ = os.Stdout
}
