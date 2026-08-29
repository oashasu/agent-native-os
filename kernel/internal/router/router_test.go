package router

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/example/agent-native-microkernel/internal/authz"
	"github.com/example/agent-native-microkernel/internal/contractmeta"
	"github.com/example/agent-native-microkernel/internal/manifest"
	"github.com/example/agent-native-microkernel/internal/registry"
	"github.com/example/agent-native-microkernel/sdk/go/protocol"
)

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
