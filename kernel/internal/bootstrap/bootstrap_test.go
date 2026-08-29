package bootstrap

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/example/agent-native-microkernel/internal/contractmeta"
	"github.com/example/agent-native-microkernel/internal/registry"
)

func emptyCatalogRegistry() *registry.Registry {
	reg := registry.New()
	reg.SetContractCatalog(&contractmeta.Catalog{
		ByContract:   map[string]contractmeta.Metadata{},
		ByCapability: map[string]contractmeta.Metadata{},
	})
	return reg
}

// A single unparseable manifest file must not abort loading of the rest.
func TestLoadAndAdmitSkipsUnparseableManifestAndContinues(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bad.manifest.json"), []byte("{ not json"), 0644); err != nil {
		t.Fatal(err)
	}
	good := `{
		"manifest_version": 1,
		"plugin": {"id": "org.test.good", "version": "1.0.0"},
		"runtime": {"protocol": "vibe-plugin/1", "executable": "bin/good"}
	}`
	if err := os.WriteFile(filepath.Join(dir, "good.manifest.json"), []byte(good), 0644); err != nil {
		t.Fatal(err)
	}

	reg := emptyCatalogRegistry()
	loaded, failed := LoadAndAdmit(dir, reg)

	if len(loaded) != 1 || loaded[0].Manifest.Plugin.ID != "org.test.good" {
		t.Fatalf("want exactly the good plugin loaded, got %+v", loaded)
	}
	if len(failed) != 1 || filepath.Base(failed[0].Path) != "bad.manifest.json" {
		t.Fatalf("want bad.manifest.json reported as failed, got %+v", failed)
	}
	if _, ok := reg.Manifest("org.test.good"); !ok {
		t.Fatal("good plugin was not admitted to the registry")
	}
}

// An admission conflict for one plugin must not abort admission of unrelated plugins.
func TestLoadAndAdmitReportsAdmissionFailureWithoutAborting(t *testing.T) {
	dir := t.TempDir()
	dupA := `{"manifest_version":1,"plugin":{"id":"org.test.dup","version":"1"},"runtime":{"protocol":"vibe-plugin/1","executable":"bin/x"}}`
	if err := os.WriteFile(filepath.Join(dir, "a.manifest.json"), []byte(dupA), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.manifest.json"), []byte(dupA), 0644); err != nil {
		t.Fatal(err)
	}
	good := `{"manifest_version":1,"plugin":{"id":"org.test.good","version":"1"},"runtime":{"protocol":"vibe-plugin/1","executable":"bin/good"}}`
	if err := os.WriteFile(filepath.Join(dir, "c.manifest.json"), []byte(good), 0644); err != nil {
		t.Fatal(err)
	}

	reg := emptyCatalogRegistry()
	loaded, failed := LoadAndAdmit(dir, reg)

	loadedIDs := map[string]bool{}
	for _, l := range loaded {
		loadedIDs[l.Manifest.Plugin.ID] = true
	}
	if !loadedIDs["org.test.good"] {
		t.Fatalf("unrelated plugin dropped because of a duplicate-id conflict; loaded=%v", loadedIDs)
	}
	if !loadedIDs["org.test.dup"] {
		t.Fatalf("first copy of duplicate id should have been admitted; loaded=%v", loadedIDs)
	}
	if len(failed) != 1 || failed[0].PluginID != "org.test.dup" {
		t.Fatalf("want exactly the second duplicate reported as failed, got %+v", failed)
	}
}
