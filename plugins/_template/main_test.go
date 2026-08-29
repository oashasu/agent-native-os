package main

import (
	"encoding/json"
	"testing"

	"github.com/example/agent-native-microkernel/sdk/go/protocol"
)

func TestEchoHandlerReturnsMessage(t *testing.T) {
	in := protocol.Envelope{Payload: protocol.NewPayload(map[string]string{"message": "ping"})}
	out, perr := echoHandler(in)
	if perr != nil {
		t.Fatalf("handler error: %+v", perr)
	}
	b, _ := json.Marshal(out)
	var got map[string]string
	_ = json.Unmarshal(b, &got)
	if got["echo"] != "ping" {
		t.Fatalf("want echo=ping, got %v", got)
	}
}

func TestEchoHandlerRejectsEmptyMessage(t *testing.T) {
	in := protocol.Envelope{Payload: protocol.NewPayload(map[string]string{})}
	_, perr := echoHandler(in)
	if perr == nil || perr.Code != "INVALID" {
		t.Fatalf("want INVALID error for empty message, got %+v", perr)
	}
}
