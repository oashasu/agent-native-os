package pluginhost

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/example/agent-native-microkernel/sdk/go/protocol"
)

func TestChildEnvelopeInheritsTraceCorrelationCausationAndParentDeadline(t *testing.T) {
	parentDeadline := time.Now().Add(350 * time.Millisecond).UTC()
	parent := protocol.Envelope{
		MessageID:     "req-parent",
		TraceID:       "trace-root",
		CorrelationID: "corr-workflow",
		Deadline:      parentDeadline.Format(time.RFC3339Nano),
	}

	child := childEnvelope(parent, protocol.KindCommand, "demo.child", 1, map[string]string{"x": "y"}, 5*time.Second)

	if child.TraceID != parent.TraceID {
		t.Fatalf("trace_id changed: got %q want %q", child.TraceID, parent.TraceID)
	}
	if child.CorrelationID != parent.CorrelationID {
		t.Fatalf("correlation_id changed: got %q want %q", child.CorrelationID, parent.CorrelationID)
	}
	if child.CausationID != parent.MessageID {
		t.Fatalf("causation_id = %q want parent message %q", child.CausationID, parent.MessageID)
	}
	gotDeadline, has, err := protocol.ParseDeadline(child.Deadline)
	if err != nil || !has {
		t.Fatalf("child deadline missing/invalid: %q err=%v", child.Deadline, err)
	}
	if gotDeadline.After(parentDeadline.Add(2 * time.Millisecond)) {
		t.Fatalf("child deadline exceeded parent deadline: child=%s parent=%s", gotDeadline, parentDeadline)
	}
}

func TestEffectiveTimeoutNeverOutlivesEnvelopeDeadline(t *testing.T) {
	e := protocol.Envelope{Deadline: time.Now().Add(250 * time.Millisecond).UTC().Format(time.RFC3339Nano)}
	got, err := effectiveTimeout(e, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if got <= 0 || got > time.Second {
		t.Fatalf("effective timeout should be bounded by parent deadline, got %s", got)
	}
}

func TestEffectiveTimeoutRejectsExpiredDeadline(t *testing.T) {
	e := protocol.Envelope{Deadline: time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano)}
	if _, err := effectiveTimeout(e, 5*time.Second); err == nil {
		t.Fatal("expected expired deadline to fail immediately")
	}
}

func TestStreamCancelPreservesTraceCorrelationAndCausation(t *testing.T) {
	var out bytes.Buffer
	h := New("org.vibe.test", "0.0.0", "")
	h.out = &out
	s := &Stream{
		ID:            "stream-1",
		host:          h,
		TraceID:       "trace-root",
		CorrelationID: "corr-workflow",
		RequestID:     "req-child",
	}
	if err := s.Cancel(); err != nil {
		t.Fatal(err)
	}
	var e protocol.Envelope
	if err := json.NewDecoder(&out).Decode(&e); err != nil {
		t.Fatal(err)
	}
	if e.Kind != protocol.KindCancel || e.StreamID != "stream-1" {
		t.Fatalf("unexpected cancel envelope: %+v", e)
	}
	if e.TraceID != "trace-root" || e.CorrelationID != "corr-workflow" || e.CausationID != "req-child" {
		t.Fatalf("cancel lost trace context: %+v", e)
	}
}

func TestRootInvocationCreatesFreshTraceAndCorrelation(t *testing.T) {
	e := childEnvelope(protocol.Envelope{}, protocol.KindQuery, "demo.query", 1, nil, time.Second)
	if e.TraceID == "" || e.CorrelationID == "" {
		t.Fatalf("root invocation must create trace/correlation: %+v", e)
	}
	if e.CausationID != "" {
		t.Fatalf("root invocation must not invent causation, got %q", e.CausationID)
	}
}

func TestBulkStreamBackpressureDoesNotBlockControlPlane(t *testing.T) {
	var in bytes.Buffer
	var out bytes.Buffer
	h := New("org.vibe.test", "0", "")
	h.in = &in
	h.out = &out
	ch := make(chan protocol.Envelope, 64)
	for i := 0; i < cap(ch); i++ {
		ch <- protocol.Envelope{Kind: protocol.KindStreamData, StreamID: "s"}
	}
	h.streams["s"] = ch
	enc := json.NewEncoder(&in)
	_ = enc.Encode(protocol.Envelope{Protocol: 1, MessageID: "bulk", Kind: protocol.KindStreamData, StreamID: "s"})
	_ = enc.Encode(protocol.Envelope{Protocol: 1, MessageID: "ping1", Kind: protocol.KindPing})
	if err := h.Serve(); err != nil {
		t.Fatal(err)
	}
	// Make room for the async explicit close and prevent a leaked goroutine.
	<-ch
	deadline := time.Now().Add(time.Second)
	for len(ch) < 64 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	dec := json.NewDecoder(&out)
	seenPong := false
	seenCancel := false
	for dec.More() {
		var e protocol.Envelope
		if dec.Decode(&e) != nil {
			break
		}
		if e.Kind == protocol.KindPong {
			seenPong = true
		}
		if e.Kind == protocol.KindCancel {
			seenCancel = true
		}
	}
	// json.Decoder.More is for arrays; decode the newline stream explicitly instead.
	if !seenPong || !seenCancel {
		seenPong = false
		seenCancel = false
		dec = json.NewDecoder(bytes.NewReader(out.Bytes()))
		for {
			var e protocol.Envelope
			if dec.Decode(&e) != nil {
				break
			}
			if e.Kind == protocol.KindPong {
				seenPong = true
			}
			if e.Kind == protocol.KindCancel {
				seenCancel = true
			}
		}
	}
	if !seenPong || !seenCancel {
		t.Fatalf("control plane stalled under stream pressure: output=%s", out.String())
	}
}
