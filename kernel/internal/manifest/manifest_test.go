package manifest

import "testing"

func TestValidateManifest(t *testing.T) {
	m := Manifest{
		ManifestVersion: 1,
		Plugin:          Plugin{ID: "demo", Version: "1.0.0"},
		Runtime:         Runtime{Protocol: "vibe-plugin/1", Executable: "demo"},
		Exports:         []Capability{{Name: "demo.echo", Major: 1, Contract: "demo.echo@1"}},
	}
	if err := Validate(m); err != nil {
		t.Fatalf("expected valid manifest: %v", err)
	}
}

func TestStatefulManifestRequiresExplicitDataNamespace(t *testing.T) {
	m := Manifest{
		ManifestVersion: 1,
		Plugin:          Plugin{ID: "stateful", Version: "1.0.0"},
		Runtime:         Runtime{Protocol: "vibe-plugin/1", Executable: "stateful"},
		Exports: []Capability{{
			Name: "work.get", Major: 1, Contract: "work.get@1", Mode: "stateful", Service: "default", Authority: "work-main",
		}},
	}
	if err := Validate(m); err == nil {
		t.Fatal("stateful plugins must declare runtime.data_namespace")
	}
	m.Runtime.DataNamespace = "state-authority/work-main"
	if err := Validate(m); err != nil {
		t.Fatalf("explicit storage identity should validate: %v", err)
	}
}

func TestValidateRejectsTraversingDataNamespace(t *testing.T) {
	m := Manifest{ManifestVersion: 1, Plugin: Plugin{ID: "p", Version: "1"}, Runtime: Runtime{Protocol: "vibe-plugin/1", Executable: "p", DataNamespace: "../escape"}}
	if err := Validate(m); err == nil {
		t.Fatal("path traversal data_namespace accepted")
	}
}
func TestValidateRejectsRuntimeProtocolMismatch(t *testing.T) {
	m := Manifest{ManifestVersion: 1, Plugin: Plugin{ID: "p", Version: "1"}, Runtime: Runtime{Protocol: "vibe-plugin/999", Executable: "p"}}
	if err := Validate(m); err == nil {
		t.Fatal("runtime protocol mismatch accepted")
	}
}
