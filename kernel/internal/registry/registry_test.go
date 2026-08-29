package registry

import (
	"github.com/example/agent-native-microkernel/internal/manifest"
	"github.com/example/agent-native-microkernel/sdk/go/protocol"
	"testing"
)

func TestStatelessProviderReplacement(t *testing.T) {
	r := New()
	a := manifest.Manifest{Plugin: manifest.Plugin{ID: "a"}, Exports: []manifest.Capability{{Name: "demo.echo", Major: 1, Mode: "stateless"}}}
	b := manifest.Manifest{Plugin: manifest.Plugin{ID: "b"}, Exports: []manifest.Capability{{Name: "demo.echo", Major: 1, Mode: "stateless"}}}
	if err := r.RegisterRuntime(a, "ra"); err != nil {
		t.Fatal(err)
	}
	if err := r.RegisterRuntime(b, "rb"); err != nil {
		t.Fatal(err)
	}
	p, err := r.Resolve("demo.echo", 1, "", "", "")
	if err != nil || p.PluginID != "a" {
		t.Fatalf("unexpected provider %+v %v", p, err)
	}
	r.MarkHealth("ra", false)
	p, err = r.Resolve("demo.echo", 1, "", "", "")
	if err != nil || p.PluginID != "b" {
		t.Fatalf("expected failover %+v %v", p, err)
	}
}
func TestStatefulAuthorityPreventsUnsafeFailover(t *testing.T) {
	r := New()
	a := manifest.Manifest{Plugin: manifest.Plugin{ID: "a"}, Runtime: manifest.Runtime{DataNamespace: "state/db-a"}, Exports: []manifest.Capability{{Name: "work.get", Major: 1, Mode: "stateful", Service: "default", Authority: "db-a", Priority: 100}}}
	b := manifest.Manifest{Plugin: manifest.Plugin{ID: "b"}, Runtime: manifest.Runtime{DataNamespace: "state/db-b"}, Exports: []manifest.Capability{{Name: "work.get", Major: 1, Mode: "stateful", Service: "default", Authority: "db-b", Priority: 50}}}
	if err := r.RegisterRuntime(a, "ra"); err != nil {
		t.Fatal(err)
	}
	if err := r.RegisterRuntime(b, "rb"); err != nil {
		t.Fatal(err)
	}
	r.SetBinding("work.get", 1, Binding{Service: "default", Authority: "db-a"})
	p, err := r.Resolve("work.get", 1, "", "", "")
	if err != nil || p.PluginID != "a" {
		t.Fatalf("expected a %+v %v", p, err)
	}
	r.MarkHealth("ra", false)
	if _, err := r.Resolve("work.get", 1, "", "", ""); err == nil {
		t.Fatal("must not fail over to different authority")
	}
}
func TestStatefulAuthorityRejectsConflictingStorageIdentity(t *testing.T) {
	r := New()
	a := manifest.Manifest{Plugin: manifest.Plugin{ID: "a"}, Runtime: manifest.Runtime{DataNamespace: "state/shared-a"}, Exports: []manifest.Capability{{Name: "work.get", Major: 1, Mode: "stateful", Service: "default", Authority: "work-main", Priority: 100}}}
	b := manifest.Manifest{Plugin: manifest.Plugin{ID: "b"}, Runtime: manifest.Runtime{DataNamespace: "state/shared-b"}, Exports: []manifest.Capability{{Name: "work.get", Major: 1, Mode: "stateful", Service: "default", Authority: "work-main", Priority: 50}}}
	if err := r.RegisterRuntime(a, "ra"); err != nil {
		t.Fatal(err)
	}
	if err := r.RegisterRuntime(b, "rb"); err == nil {
		t.Fatal("same stateful authority must reject a provider with different storage identity")
	}
	ps := r.Providers("work.get", 1)
	if len(ps) != 1 || ps[0].PluginID != "a" {
		t.Fatalf("conflicting provider must not be partially registered: %+v", ps)
	}
}

func TestStatefulAuthorityAllowsReplicaWithSameStorageIdentity(t *testing.T) {
	r := New()
	a := manifest.Manifest{Plugin: manifest.Plugin{ID: "a"}, Runtime: manifest.Runtime{DataNamespace: "state/shared"}, Exports: []manifest.Capability{{Name: "work.get", Major: 1, Mode: "stateful", Service: "default", Authority: "work-main", Priority: 100}}}
	b := manifest.Manifest{Plugin: manifest.Plugin{ID: "b"}, Runtime: manifest.Runtime{DataNamespace: "state/shared"}, Exports: []manifest.Capability{{Name: "work.get", Major: 1, Mode: "stateful", Service: "default", Authority: "work-main", Priority: 50}}}
	if err := r.RegisterRuntime(a, "ra"); err != nil {
		t.Fatal(err)
	}
	if err := r.RegisterRuntime(b, "rb"); err != nil {
		t.Fatalf("same authority replica with same storage identity should be allowed: %v", err)
	}
}

func TestMissingRequired(t *testing.T) {
	consumer := manifest.Manifest{Consumes: manifest.Consumes{Required: []manifest.Capability{{Name: "x", Major: 1}}}}
	if len(MissingRequired(consumer, nil)) != 1 {
		t.Fatal("expected missing")
	}
	provider := manifest.Manifest{Exports: []manifest.Capability{{Name: "x", Major: 1}}}
	if len(MissingRequired(consumer, []manifest.Manifest{provider})) != 0 {
		t.Fatal("unexpected missing")
	}
}

func TestStatefulCommandSingleWriterAndFencingEpoch(t *testing.T) {
	r := New()
	if err := r.SetFenceRoot(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	a := manifest.Manifest{Plugin: manifest.Plugin{ID: "a"}, Runtime: manifest.Runtime{DataNamespace: "state/shared"}, Exports: []manifest.Capability{{Name: "work.create", Major: 1, Mode: "stateful", Service: "default", Authority: "main", Priority: 100}}}
	b := manifest.Manifest{Plugin: manifest.Plugin{ID: "b"}, Runtime: manifest.Runtime{DataNamespace: "state/shared"}, Exports: []manifest.Capability{{Name: "work.create", Major: 1, Mode: "stateful", Service: "default", Authority: "main", Priority: 50}}}
	if err := r.RegisterRuntime(a, "ra"); err != nil {
		t.Fatal(err)
	}
	if err := r.RegisterRuntime(b, "rb"); err != nil {
		t.Fatal(err)
	}
	first, err := r.ResolveForKind("work.create", 1, "", "default", "main", protocol.KindCommand)
	if err != nil || first.RuntimeID != "ra" || first.WriterEpoch <= 0 {
		t.Fatalf("unexpected first writer %+v %v", first, err)
	}
	if _, err := r.ResolveForKind("work.create", 1, "b", "default", "main", protocol.KindCommand); err == nil {
		t.Fatal("provider_hint must not bypass active writer")
	}
	r.MarkHealth("ra", false)
	second, err := r.ResolveForKind("work.create", 1, "", "default", "main", protocol.KindCommand)
	if err != nil || second.RuntimeID != "rb" || second.WriterEpoch <= first.WriterEpoch {
		t.Fatalf("expected fenced promotion %+v after %+v err=%v", second, first, err)
	}
}

func TestRequestCircuitBreakerNotClearedByHeartbeat(t *testing.T) {
	r := New()
	a := manifest.Manifest{Plugin: manifest.Plugin{ID: "a"}, Exports: []manifest.Capability{{Name: "demo.echo", Major: 1, Mode: "stateless", Priority: 100}}}
	b := manifest.Manifest{Plugin: manifest.Plugin{ID: "b"}, Exports: []manifest.Capability{{Name: "demo.echo", Major: 1, Mode: "stateless", Priority: 50}}}
	_ = r.RegisterRuntime(a, "ra")
	_ = r.RegisterRuntime(b, "rb")
	for i := 0; i < 3; i++ {
		r.RecordFailure("ra")
	}
	r.RecordHeartbeatSuccess("ra")
	p, err := r.ResolveForKind("demo.echo", 1, "", "", "", protocol.KindQuery)
	if err != nil || p.RuntimeID != "rb" {
		t.Fatalf("heartbeat must not erase handler circuit breaker: %+v %v", p, err)
	}
}
