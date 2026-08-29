package supervisor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/example/agent-native-microkernel/internal/authz"
	"github.com/example/agent-native-microkernel/internal/contractmeta"
	"github.com/example/agent-native-microkernel/internal/manifest"
	"github.com/example/agent-native-microkernel/internal/registry"
	"github.com/example/agent-native-microkernel/internal/router"
	"github.com/example/agent-native-microkernel/sdk/go/pluginhost"
	"github.com/example/agent-native-microkernel/sdk/go/protocol"
)

func TestResourcePluginHelper(t *testing.T) {
	id := os.Getenv("VIBE_PLUGIN_ID")
	if id == "org.vibe.test.kind" {
		h := pluginhost.New(id, "test", "")
		h.HandleQuery("demo.mutate", 1, func(protocol.Envelope) (any, *protocol.Error) { return map[string]any{"ok": true}, nil })
		_ = h.Serve()
		return
	}
	if id != "org.vibe.test.resource" {
		return
	}
	// Touch memory so RSS enforcement sees it, then serve protocol/heartbeat.
	b := make([]byte, 32*1024*1024)
	for i := 0; i < len(b); i += 4096 {
		b[i] = 1
	}
	h := pluginhost.New(id, "test", "")
	_ = h.Serve()
	_ = b
}

func TestMemoryBudgetIsActuallyEnforced(t *testing.T) {
	reg := registry.New()
	reg.SetContractCatalog(&contractmeta.Catalog{ByContract: map[string]contractmeta.Metadata{}, ByCapability: map[string]contractmeta.Metadata{}})
	rt := router.New(reg, authz.New())
	sup := New(reg, rt)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := manifest.Manifest{ManifestVersion: 1, Plugin: manifest.Plugin{ID: "org.vibe.test.resource", Version: "1"}, Runtime: manifest.Runtime{Protocol: protocol.RuntimeProtocol, Executable: os.Args[0], Args: []string{"-test.run=TestResourcePluginHelper"}}, Resources: manifest.ResourcePolicy{MemoryMB: 4, CPUWeight: 20}}
	mp := filepath.Join(t.TempDir(), "resource.manifest.json")
	if err := os.WriteFile(mp, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	run, err := sup.Start(ctx, m, mp, "")
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		found := false
		for _, id := range sup.RuntimeIDs() {
			if id == run.ID {
				found = true
			}
		}
		if !found {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("runtime exceeded memory budget but supervisor did not terminate it")
}

func TestProviderHandshakeEnforcesContractKind(t *testing.T) {
	reg := registry.New()
	meta := contractmeta.Metadata{Contract: "demo.mutate@1", Kind: protocol.KindCommand}
	reg.SetContractCatalog(&contractmeta.Catalog{ByContract: map[string]contractmeta.Metadata{"demo.mutate@1": meta}, ByCapability: map[string]contractmeta.Metadata{"demo.mutate@1": meta}})
	rt := router.New(reg, authz.New())
	sup := New(reg, rt)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := manifest.Manifest{ManifestVersion: 1, Plugin: manifest.Plugin{ID: "org.vibe.test.kind", Version: "1"}, Runtime: manifest.Runtime{Protocol: protocol.RuntimeProtocol, Executable: os.Args[0], Args: []string{"-test.run=TestResourcePluginHelper"}}, Exports: []manifest.Capability{{Name: "demo.mutate", Major: 1, Contract: "demo.mutate@1", Mode: "stateless"}}}
	mp := filepath.Join(t.TempDir(), "kind.manifest.json")
	_ = os.WriteFile(mp, []byte("{}"), 0644)
	if _, err := sup.Start(ctx, m, mp, ""); err == nil || !strings.Contains(err.Error(), "handler kind mismatch") {
		t.Fatalf("provider with wrong operation kind admitted: %v", err)
	}
}

func TestMarkManifestRejectedIsObservable(t *testing.T) {
	reg := registry.New()
	rt := router.New(reg, authz.New())
	sup := New(reg, rt)

	// A manifest that parsed but was rejected on admission: keyed by plugin id.
	sup.MarkManifestRejected("org.vibe.test.dup", "dup.manifest.json", "duplicate plugin id")
	st, ok := sup.Status("org.vibe.test.dup")
	if !ok || st.State != StateFailed || !strings.Contains(st.Reason, "duplicate") {
		t.Fatalf("admission-rejected manifest status = %+v ok=%v, want FAILED", st, ok)
	}

	// A manifest that did not parse: no plugin id, keyed by path so it is still visible.
	sup.MarkManifestRejected("", "broken.manifest.json", "invalid character")
	st, ok = sup.Status("broken.manifest.json")
	if !ok || st.State != StateFailed {
		t.Fatalf("unparseable manifest status = %+v ok=%v, want FAILED keyed by path", st, ok)
	}
}

func TestPluginStatusLifecycleIsExplicit(t *testing.T) {
	reg := registry.New()
	rt := router.New(reg, authz.New())
	sup := New(reg, rt)
	m := manifest.Manifest{Plugin: manifest.Plugin{ID: "org.vibe.test.status", Version: "1"}}

	sup.Track(m)
	st, ok := sup.Status(m.Plugin.ID)
	if !ok || st.State != StateInstalled {
		t.Fatalf("tracked plugin status = %+v ok=%v, want INSTALLED", st, ok)
	}

	sup.MarkBlocked(m, "missing demo@1")
	st, ok = sup.Status(m.Plugin.ID)
	if !ok || st.State != StateBlocked || !strings.Contains(st.Reason, "missing") {
		t.Fatalf("blocked plugin status = %+v ok=%v", st, ok)
	}

	sup.setStatus(m.Plugin.ID, StateDegraded, "runtime-test", "optional capability unavailable")
	st, _ = sup.Status(m.Plugin.ID)
	if st.State != StateDegraded || st.RuntimeID != "runtime-test" {
		t.Fatalf("degraded plugin status = %+v", st)
	}

	sup.setStatus(m.Plugin.ID, StateReady, "runtime-test", "")
	st, _ = sup.Status(m.Plugin.ID)
	if st.State != StateReady || st.Reason != "" {
		t.Fatalf("ready plugin status = %+v", st)
	}
}
