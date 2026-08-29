// Package bootstrap turns a directory of plugin manifests into an admitted
// plugin set. A single malformed or rejected manifest is a local failure: it is
// reported, not fatal. This matches docs/04 ("provider start failure is local;
// the host continues with other plugins").
package bootstrap

import (
	"path/filepath"

	"github.com/example/agent-native-microkernel/internal/manifest"
	"github.com/example/agent-native-microkernel/internal/registry"
)

// Loaded is a manifest that parsed, validated and was admitted to the registry.
type Loaded struct {
	Manifest manifest.Manifest
	Path     string
	Hash     string
}

// Failed is a manifest file that could not be loaded or admitted. PluginID is
// empty when the manifest itself did not parse.
type Failed struct {
	Path     string
	PluginID string
	Reason   string
}

// LoadAndAdmit globs "*.manifest.json" under pluginsDir, loads and admits each
// one, and returns the admitted set plus per-file failures. It never aborts the
// batch because of one bad file.
func LoadAndAdmit(pluginsDir string, reg *registry.Registry) ([]Loaded, []Failed) {
	matches, _ := filepath.Glob(filepath.Join(pluginsDir, "*.manifest.json"))
	var loaded []Loaded
	var failed []Failed
	for _, p := range matches {
		m, hash, err := manifest.Load(p)
		if err != nil {
			failed = append(failed, Failed{Path: p, Reason: err.Error()})
			continue
		}
		if err := reg.AddManifest(m); err != nil {
			failed = append(failed, Failed{Path: p, PluginID: m.Plugin.ID, Reason: err.Error()})
			continue
		}
		loaded = append(loaded, Loaded{Manifest: m, Path: p, Hash: hash})
	}
	return loaded, failed
}
