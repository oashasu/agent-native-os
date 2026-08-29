package clientgateway

import (
	"github.com/example/agent-native-microkernel/sdk/go/protocol"
	"testing"
)

func TestAuthenticateBindsTokenToIdentity(t *testing.T) {
	g := New("/tmp/unused", nil, map[string]string{
		"alice": "9c220f200955d76c0a38d308225e0ef10c5f971acaf2f8d1d8f732affa5bd1dc",
		"bob":   "97dd3707015dcf069cf73022ed7173b1165db6eff24b441cb57fd069a8c4e525",
	})
	if !g.authenticate("alice", "alice-token") {
		t.Fatal("expected valid credentials")
	}
	if g.authenticate("alice", "bob-token") {
		t.Fatal("token from another identity must not authenticate")
	}
	if g.authenticate("bob", "alice-token") {
		t.Fatal("identity spoof with another token must fail")
	}
	if g.authenticate("unknown", "alice-token") {
		t.Fatal("unknown identity must fail")
	}
}

func TestPrepareNeverTrustsCaller(t *testing.T) {
	req := protocol.Envelope{Caller: "admin"}
	prepare(&req)
	if req.Caller != "" {
		t.Fatalf("client supplied caller survived prepare: %q", req.Caller)
	}
	if req.MessageID == "" || req.TraceID == "" || req.CorrelationID == "" {
		t.Fatal("prepare did not create request context")
	}
}
