package protocol

import "testing"

func TestValidateEnvelope(t *testing.T) {
	ok := Envelope{Protocol: 1, MessageID: "m1", Kind: KindQuery, Capability: "demo.uppercase", Major: 1}
	if err := ValidateEnvelope(ok); err != nil {
		t.Fatalf("expected valid envelope: %v", err)
	}
	bad := Envelope{Protocol: 1, MessageID: "m2", Kind: KindQuery}
	if err := ValidateEnvelope(bad); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestRemoteErrorPreservesProviderSemantics(t *testing.T) {
	pe := &Error{Code: "CONFLICT", Message: "version mismatch", Retryable: true, Details: map[string]any{"expected": float64(2)}}
	err := NewRemoteError(pe)
	re, ok := err.(*RemoteError)
	if !ok {
		t.Fatalf("expected *RemoteError, got %T", err)
	}
	if re.Remote.Code != "CONFLICT" || !re.Remote.Retryable || re.Remote.Details["expected"] != float64(2) {
		t.Fatalf("remote error semantics lost: %+v", re.Remote)
	}
}
