package registry

import (
	"github.com/example/agent-native-microkernel/internal/contractmeta"
	"github.com/example/agent-native-microkernel/internal/manifest"
	"github.com/example/agent-native-microkernel/sdk/go/protocol"
	"testing"
)

func testCatalog(names ...string) *contractmeta.Catalog {
	c := &contractmeta.Catalog{ByContract: map[string]contractmeta.Metadata{}, ByCapability: map[string]contractmeta.Metadata{}}
	for _, n := range names {
		m := contractmeta.Metadata{Contract: n, Kind: protocol.KindQuery}
		c.ByContract[n] = m
		c.ByCapability[n] = m
	}
	return c
}
func baseManifest(id, cap, mode, ns string) manifest.Manifest {
	ex := manifest.Capability{Name: cap, Major: 1, Contract: cap + "@1", Mode: mode}
	if mode == "stateful" {
		ex.Service = "svc"
		ex.Authority = "auth"
	}
	return manifest.Manifest{ManifestVersion: 1, Plugin: manifest.Plugin{ID: id, Version: "1"}, Runtime: manifest.Runtime{Protocol: protocol.RuntimeProtocol, Executable: "x", DataNamespace: ns}, Exports: []manifest.Capability{ex}}
}
func TestAdmissionRejectsDuplicatePluginID(t *testing.T) {
	r := New()
	r.SetContractCatalog(testCatalog("demo@1"))
	m := baseManifest("same", "demo", "stateless", "")
	if err := r.AddManifest(m); err != nil {
		t.Fatal(err)
	}
	if err := r.AddManifest(m); err == nil {
		t.Fatal("duplicate plugin id admitted")
	}
}
func TestAdmissionRejectsMixedStatefulStateless(t *testing.T) {
	r := New()
	r.SetContractCatalog(testCatalog("demo@1"))
	if err := r.AddManifest(baseManifest("a", "demo", "stateless", "")); err != nil {
		t.Fatal(err)
	}
	if err := r.AddManifest(baseManifest("b", "demo", "stateful", "state/a")); err == nil {
		t.Fatal("mixed provider modes admitted")
	}
}
func TestAdmissionConflictIsAtomic(t *testing.T) {
	r := New()
	r.SetContractCatalog(testCatalog("demo@1", "other@1"))
	a := baseManifest("a", "demo", "stateful", "state/a")
	if err := r.AddManifest(a); err != nil {
		t.Fatal(err)
	}
	b := baseManifest("b", "other", "stateful", "state/b") // same svc/auth but different storage
	if err := r.AddManifest(b); err == nil {
		t.Fatal("authority storage conflict admitted")
	}
	if _, ok := r.Manifest("b"); ok {
		t.Fatal("failed admission left plugin identity behind")
	}
}
func TestMissingHealthyRequiredUsesRuntimeHealth(t *testing.T) {
	r := New()
	need := manifest.Manifest{Consumes: manifest.Consumes{Required: []manifest.Capability{{Name: "demo", Major: 1}}}}
	p := baseManifest("p", "demo", "stateless", "")
	if len(r.MissingHealthyRequired(need)) != 1 {
		t.Fatal("manifest absence should be missing")
	}
	if err := r.RegisterRuntime(p, "rp"); err != nil {
		t.Fatal(err)
	}
	if len(r.MissingHealthyRequired(need)) != 0 {
		t.Fatal("healthy runtime should satisfy dependency")
	}
	r.MarkHealth("rp", false)
	if len(r.MissingHealthyRequired(need)) != 1 {
		t.Fatal("unhealthy runtime must not satisfy required dependency")
	}
}
