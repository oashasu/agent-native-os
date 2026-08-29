package router

import (
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/example/agent-native-microkernel/internal/authz"
	"github.com/example/agent-native-microkernel/internal/contractmeta"
	"github.com/example/agent-native-microkernel/internal/manifest"
	"github.com/example/agent-native-microkernel/internal/registry"
	"github.com/example/agent-native-microkernel/sdk/go/protocol"
)

func TestSlowExternalConsumerDoesNotStallOtherStreams(t *testing.T) {
	r := New(registry.New(), authz.New())
	r.streamSendTimeout = 50 * time.Millisecond

	provIn, kernelToProv := io.Pipe()
	provStdoutR, provStdoutW := io.Pipe()
	defer provStdoutW.Close() // keep the provider's readLoop alive
	r.AddClient("prov", NewProcessClient("prov", "org.vibe.prov", kernelToProv, provStdoutR))
	cancels := make(chan protocol.Envelope, 4)
	go func() {
		dec := json.NewDecoder(provIn)
		for {
			var e protocol.Envelope
			if dec.Decode(&e) != nil {
				return
			}
			if e.Kind == protocol.KindCancel {
				cancels <- e
			}
		}
	}()

	stalled := make(chan protocol.Envelope)      // never drained
	live := make(chan protocol.Envelope, 4)      // healthy consumer
	r.mu.Lock()
	r.streams["A"] = streamRoute{streamID: "A", providerRuntime: "prov", requestID: "reqA", external: stalled, closeExternal: &sync.Once{}}
	r.streams["B"] = streamRoute{streamID: "B", providerRuntime: "prov", requestID: "reqB", external: live, closeExternal: &sync.Once{}}
	r.mu.Unlock()

	doneA := make(chan struct{})
	go func() {
		r.forwardStream("prov", protocol.Envelope{Protocol: 1, MessageID: "fA", Kind: protocol.KindStreamData, StreamID: "A"})
		close(doneA)
	}()
	select {
	case <-doneA:
	case <-time.After(2 * time.Second):
		t.Fatal("forwardStream blocked on a stalled external consumer")
	}

	r.forwardStream("prov", protocol.Envelope{Protocol: 1, MessageID: "fB", Kind: protocol.KindStreamData, StreamID: "B"})
	select {
	case f := <-live:
		if f.StreamID != "B" {
			t.Fatalf("wrong frame delivered to healthy stream: %+v", f)
		}
	case <-time.After(time.Second):
		t.Fatal("healthy stream B stalled behind stream A")
	}

	select {
	case c := <-cancels:
		if c.StreamID != "A" {
			t.Fatalf("cancel targeted the wrong stream: %+v", c)
		}
	case <-time.After(time.Second):
		t.Fatal("stalled stream's provider was not cancelled")
	}
	r.mu.RLock()
	_, stillA := r.streams["A"]
	r.mu.RUnlock()
	if stillA {
		t.Fatal("stalled stream route was not removed")
	}
}

func TestProviderErrorPassesThroughRouterUnchanged(t *testing.T) {
	reg := registry.New()
	reg.SetContractCatalog(&contractmeta.Catalog{ByContract: map[string]contractmeta.Metadata{"demo.fail@1": {Contract: "demo.fail@1", Kind: protocol.KindCommand}}, ByCapability: map[string]contractmeta.Metadata{"demo.fail@1": {Contract: "demo.fail@1", Kind: protocol.KindCommand}}})
	auth := authz.New()
	caller := manifest.Manifest{
		Plugin:   manifest.Plugin{ID: "org.vibe.consumer", Version: "1.0.0"},
		Consumes: manifest.Consumes{Required: []manifest.Capability{{Name: "demo.fail", Major: 1, Contract: "demo.fail@1"}}},
	}
	provider := manifest.Manifest{
		Plugin:  manifest.Plugin{ID: "org.vibe.provider", Version: "1.0.0"},
		Exports: []manifest.Capability{{Name: "demo.fail", Major: 1, Contract: "demo.fail@1", Mode: "stateless"}},
	}
	if err := reg.AddManifest(caller); err != nil {
		t.Fatal(err)
	}
	auth.SetGrant(caller.Plugin.ID, authz.Grant{Capabilities: []string{"demo.fail@1"}})
	if err := reg.RegisterRuntime(provider, "rt-provider"); err != nil {
		t.Fatal(err)
	}

	providerIn, kernelToProvider := io.Pipe()
	providerToKernel, providerOut := io.Pipe()
	pc := NewProcessClient("rt-provider", provider.Plugin.ID, kernelToProvider, providerToKernel)
	r := New(reg, auth)
	r.AddClient("rt-provider", pc)

	go func() {
		defer providerOut.Close()
		var req protocol.Envelope
		if err := json.NewDecoder(providerIn).Decode(&req); err != nil {
			return
		}
		_ = json.NewEncoder(providerOut).Encode(protocol.Envelope{
			Protocol:  1,
			MessageID: protocol.NewID("err"),
			Kind:      protocol.KindError,
			ReplyTo:   req.MessageID,
			Error: &protocol.Error{
				Code:      "CONFLICT",
				Message:   "version mismatch",
				Retryable: true,
				Details:   map[string]any{"expected_version": 7},
			},
		})
	}()

	resp, err := r.Invoke(caller.Plugin.ID, protocol.Envelope{
		Protocol:   1,
		MessageID:  "request-1",
		Kind:       protocol.KindCommand,
		Capability: "demo.fail",
		Major:      1,
		Deadline:   time.Now().Add(time.Second).UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatalf("provider error was reinterpreted as route error: %v", err)
	}
	if resp.Kind != protocol.KindError || resp.Error == nil {
		t.Fatalf("expected provider KindError response, got %+v", resp)
	}
	if resp.Error.Code != "CONFLICT" || !resp.Error.Retryable || resp.Error.Details["expected_version"] != float64(7) {
		t.Fatalf("provider error semantics changed: %+v", resp.Error)
	}
}

func TestEventAuthorizationAndCallerIntegrity(t *testing.T) {
	meta := contractmeta.Metadata{Contract: "sensitive.changed@1", Kind: protocol.KindEvent}
	reg := registry.New()
	reg.SetContractCatalog(&contractmeta.Catalog{
		ByContract:   map[string]contractmeta.Metadata{"sensitive.changed@1": meta},
		ByCapability: map[string]contractmeta.Metadata{"sensitive.changed@1": meta},
	})
	publisher := manifest.Manifest{Plugin: manifest.Plugin{ID: "org.vibe.publisher"}, Publishes: []manifest.Capability{{Name: "sensitive.changed", Major: 1, Contract: "sensitive.changed@1"}}}
	allowed := manifest.Manifest{Plugin: manifest.Plugin{ID: "org.vibe.allowed"}, Subscribes: []manifest.Capability{{Name: "sensitive.changed", Major: 1, Contract: "sensitive.changed@1"}}}
	denied := manifest.Manifest{Plugin: manifest.Plugin{ID: "org.vibe.denied"}, Subscribes: []manifest.Capability{{Name: "sensitive.changed", Major: 1, Contract: "sensitive.changed@1"}}}
	for _, m := range []manifest.Manifest{publisher, allowed, denied} {
		if err := reg.AddManifest(m); err != nil {
			t.Fatal(err)
		}
	}
	auth := authz.New()
	auth.SetGrant(publisher.Plugin.ID, authz.Grant{Publishes: []string{"sensitive.changed@1"}})
	auth.SetGrant(allowed.Plugin.ID, authz.Grant{Subscribes: []string{"sensitive.changed@1"}})
	// denied intentionally has no subscribe grant.
	r := New(reg, auth)

	allowedIn, kernelToAllowed := io.Pipe()
	allowedToKernel, allowedOut := io.Pipe()
	deniedIn, kernelToDenied := io.Pipe()
	deniedToKernel, deniedOut := io.Pipe()
	defer allowedIn.Close()
	defer allowedOut.Close()
	defer deniedIn.Close()
	defer deniedOut.Close()
	r.AddClient("rt-allowed", NewProcessClient("rt-allowed", allowed.Plugin.ID, kernelToAllowed, allowedToKernel))
	r.AddClient("rt-denied", NewProcessClient("rt-denied", denied.Plugin.ID, kernelToDenied, deniedToKernel))

	got := make(chan protocol.Envelope, 1)
	go func() { var e protocol.Envelope; _ = json.NewDecoder(allowedIn).Decode(&e); got <- e }()
	forged := protocol.Envelope{Protocol: 1, MessageID: "evt-1", Kind: protocol.KindEvent, Capability: "sensitive.changed", Major: 1, Caller: "org.vibe.security.review", Principal: "admin", ActorChain: []string{"admin"}, Payload: protocol.NewPayload(map[string]any{"x": 1})}
	if err := r.PublishEnvelope(publisher.Plugin.ID, forged); err != nil {
		t.Fatal(err)
	}

	select {
	case e := <-got:
		if e.Caller != publisher.Plugin.ID || e.Principal != publisher.Plugin.ID {
			t.Fatalf("TCB actor metadata not rewritten: %+v", e)
		}
		if len(e.ActorChain) != 1 || e.ActorChain[0] != publisher.Plugin.ID {
			t.Fatalf("unexpected actor chain: %+v", e.ActorChain)
		}
	case <-time.After(time.Second):
		t.Fatal("authorized subscriber did not receive event")
	}

	// If the denied subscriber received a frame, the write would be waiting in its pipe.
	deniedGot := make(chan struct{}, 1)
	go func() {
		var e protocol.Envelope
		if json.NewDecoder(deniedIn).Decode(&e) == nil {
			deniedGot <- struct{}{}
		}
	}()
	select {
	case <-deniedGot:
		t.Fatal("subscriber without host grant received sensitive event")
	case <-time.After(75 * time.Millisecond):
	}

	// A publisher declaration without a host grant is not authority.
	auth.SetGrant(publisher.Plugin.ID, authz.Grant{})
	if err := r.PublishEnvelope(publisher.Plugin.ID, forged); err == nil {
		t.Fatal("publish without host grant must be denied")
	}
}

type blockingWriter struct{ release chan struct{} }

func (b blockingWriter) Write(p []byte) (int, error) { <-b.release; return len(p), nil }

func TestSendToStuckProviderTimesOutInsteadOfBlocking(t *testing.T) {
	bw := blockingWriter{release: make(chan struct{})}
	defer close(bw.release)
	stdoutR, stdoutW := io.Pipe()
	defer stdoutW.Close() // keep readLoop alive so c.closed is not set by EOF
	pc := NewProcessClient("rt-stuck", "org.vibe.stuck", bw, stdoutR)
	pc.SetWriteTimeout(50 * time.Millisecond)

	done := make(chan error, 1)
	go func() {
		done <- pc.Send(protocol.Envelope{Protocol: 1, MessageID: "x", Kind: protocol.KindEvent, Capability: "c", Major: 1})
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Send to a stuck provider returned nil instead of a timeout error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Send blocked on a stuck provider instead of timing out")
	}

	// After a write timeout the client is disconnected: further sends fail fast.
	second := make(chan error, 1)
	go func() {
		second <- pc.Send(protocol.Envelope{Protocol: 1, MessageID: "y", Kind: protocol.KindEvent, Capability: "c", Major: 1})
	}()
	select {
	case err := <-second:
		if err == nil {
			t.Fatal("send after write timeout should fail fast, got nil")
		}
	case <-time.After(time.Second):
		t.Fatal("send after write timeout still blocked")
	}
}

func TestEventFanoutSurvivesABrokenSubscriber(t *testing.T) {
	meta := contractmeta.Metadata{Contract: "sensitive.changed@1", Kind: protocol.KindEvent}
	reg := registry.New()
	reg.SetContractCatalog(&contractmeta.Catalog{
		ByContract:   map[string]contractmeta.Metadata{"sensitive.changed@1": meta},
		ByCapability: map[string]contractmeta.Metadata{"sensitive.changed@1": meta},
	})
	publisher := manifest.Manifest{Plugin: manifest.Plugin{ID: "org.vibe.publisher"}, Publishes: []manifest.Capability{{Name: "sensitive.changed", Major: 1, Contract: "sensitive.changed@1"}}}
	broken := manifest.Manifest{Plugin: manifest.Plugin{ID: "org.vibe.broken"}, Subscribes: []manifest.Capability{{Name: "sensitive.changed", Major: 1, Contract: "sensitive.changed@1"}}}
	healthy := manifest.Manifest{Plugin: manifest.Plugin{ID: "org.vibe.healthy"}, Subscribes: []manifest.Capability{{Name: "sensitive.changed", Major: 1, Contract: "sensitive.changed@1"}}}
	for _, m := range []manifest.Manifest{publisher, broken, healthy} {
		if err := reg.AddManifest(m); err != nil {
			t.Fatal(err)
		}
	}
	auth := authz.New()
	auth.SetGrant(publisher.Plugin.ID, authz.Grant{Publishes: []string{"sensitive.changed@1"}})
	auth.SetGrant(broken.Plugin.ID, authz.Grant{Subscribes: []string{"sensitive.changed@1"}})
	auth.SetGrant(healthy.Plugin.ID, authz.Grant{Subscribes: []string{"sensitive.changed@1"}})
	r := New(reg, auth)

	// The broken subscriber's stdin has no reader: every kernel write to it fails.
	brokenIn, kernelToBroken := io.Pipe()
	_ = brokenIn.Close()
	r.AddClient("rt-broken", NewProcessClient("rt-broken", broken.Plugin.ID, kernelToBroken, strings.NewReader("")))

	healthyIn, kernelToHealthy := io.Pipe()
	healthyToKernel, healthyOut := io.Pipe()
	defer healthyIn.Close()
	defer healthyOut.Close()
	r.AddClient("rt-healthy", NewProcessClient("rt-healthy", healthy.Plugin.ID, kernelToHealthy, healthyToKernel))

	got := make(chan struct{}, 1)
	go func() {
		var e protocol.Envelope
		if json.NewDecoder(healthyIn).Decode(&e) == nil {
			got <- struct{}{}
		}
	}()

	evt := protocol.Envelope{Protocol: 1, MessageID: "evt-1", Kind: protocol.KindEvent, Capability: "sensitive.changed", Major: 1, Payload: protocol.NewPayload(map[string]any{"x": 1})}
	if err := r.PublishEnvelope(publisher.Plugin.ID, evt); err != nil {
		t.Fatalf("a broken subscriber must not surface as a publish error: %v", err)
	}
	select {
	case <-got:
	case <-time.After(time.Second):
		t.Fatal("healthy subscriber did not receive the event after a broken subscriber")
	}
}

func TestPluginCannotDropDelegationToBorrowServiceAuthority(t *testing.T) {
	reg := registry.New()
	meta := contractmeta.Metadata{Contract: "demo.child@1", Kind: protocol.KindCommand}
	reg.SetContractCatalog(&contractmeta.Catalog{ByContract: map[string]contractmeta.Metadata{"demo.child@1": meta}, ByCapability: map[string]contractmeta.Metadata{"demo.child@1": meta}})
	caller := manifest.Manifest{Plugin: manifest.Plugin{ID: "org.vibe.workflow"}, Consumes: manifest.Consumes{Required: []manifest.Capability{{Name: "demo.child", Major: 1, Contract: "demo.child@1"}}}}
	if err := reg.AddManifest(caller); err != nil {
		t.Fatal(err)
	}
	a := authz.New()
	a.SetGrant(caller.Plugin.ID, authz.Grant{Capabilities: []string{"demo.child@1"}})
	r := New(reg, a)
	_, err := r.invoke("runtime-workflow", caller.Plugin.ID, true, protocol.Envelope{Protocol: 1, MessageID: "child", Kind: protocol.KindCommand, Capability: "demo.child", Major: 1}, nil)
	if err == nil || !strings.Contains(err.Error(), "delegation required") {
		t.Fatalf("dropping delegation should fail closed, got %v", err)
	}
}

func TestDelegationTokenIsBoundToRuntime(t *testing.T) {
	reg := registry.New()
	meta := contractmeta.Metadata{Contract: "demo.child@1", Kind: protocol.KindCommand}
	reg.SetContractCatalog(&contractmeta.Catalog{ByContract: map[string]contractmeta.Metadata{"demo.child@1": meta}, ByCapability: map[string]contractmeta.Metadata{"demo.child@1": meta}})
	caller := manifest.Manifest{Plugin: manifest.Plugin{ID: "org.vibe.workflow"}, Consumes: manifest.Consumes{Required: []manifest.Capability{{Name: "demo.child", Major: 1, Contract: "demo.child@1"}}}}
	if err := reg.AddManifest(caller); err != nil {
		t.Fatal(err)
	}
	a := authz.New()
	a.SetGrant(caller.Plugin.ID, authz.Grant{Capabilities: []string{"demo.child@1"}})
	r := New(reg, a)
	r.delegations["deleg-x"] = delegationContext{principal: "alice", actorChain: []string{"alice"}, runtimeID: "runtime-other"}
	_, err := r.invoke("runtime-workflow", caller.Plugin.ID, true, protocol.Envelope{Protocol: 1, MessageID: "child", Kind: protocol.KindCommand, Capability: "demo.child", Major: 1, DelegationID: "deleg-x"}, nil)
	if err == nil || !strings.Contains(err.Error(), "cross-runtime") {
		t.Fatalf("cross-runtime delegation token accepted: %v", err)
	}
}

func TestRuntimeEventCannotDropDelegationWithoutServiceAuthority(t *testing.T) {
	meta := contractmeta.Metadata{Contract: "sensitive.changed@1", Kind: protocol.KindEvent}
	reg := registry.New()
	reg.SetContractCatalog(&contractmeta.Catalog{ByContract: map[string]contractmeta.Metadata{"sensitive.changed@1": meta}, ByCapability: map[string]contractmeta.Metadata{"sensitive.changed@1": meta}})
	publisher := manifest.Manifest{Plugin: manifest.Plugin{ID: "org.vibe.publisher"}, Publishes: []manifest.Capability{{Name: "sensitive.changed", Major: 1, Contract: "sensitive.changed@1"}}}
	if err := reg.AddManifest(publisher); err != nil {
		t.Fatal(err)
	}
	a := authz.New()
	a.SetGrant(publisher.Plugin.ID, authz.Grant{Publishes: []string{"sensitive.changed@1"}})
	r := New(reg, a)
	e := protocol.Envelope{Protocol: 1, MessageID: "evt", Kind: protocol.KindEvent, Capability: "sensitive.changed", Major: 1}
	if err := r.publishEnvelope("runtime-pub", publisher.Plugin.ID, e); err == nil || !strings.Contains(err.Error(), "delegation required") {
		t.Fatalf("runtime dropped request delegation and published autonomously: %v", err)
	}
	a.SetGrant(publisher.Plugin.ID, authz.Grant{Publishes: []string{"sensitive.changed@1"}, ServiceAuthority: true})
	if err := r.publishEnvelope("runtime-pub", publisher.Plugin.ID, e); err != nil {
		t.Fatalf("explicit service authority should allow autonomous event publish: %v", err)
	}
}
