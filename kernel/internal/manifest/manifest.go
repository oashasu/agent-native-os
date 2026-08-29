package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/example/agent-native-microkernel/sdk/go/protocol"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Capability struct {
	Name      string `json:"capability"`
	Major     int    `json:"major"`
	Contract  string `json:"contract,omitempty"`
	Mode      string `json:"mode,omitempty"`      // stateless | stateful
	Service   string `json:"service,omitempty"`   // logical service identity for stateful capability
	Authority string `json:"authority,omitempty"` // storage/ownership authority for stateful replicas
	Priority  int    `json:"priority,omitempty"`
}

type Runtime struct {
	Protocol      string   `json:"protocol"`
	Executable    string   `json:"executable"`
	Args          []string `json:"args,omitempty"`
	Isolation     string   `json:"isolation,omitempty"`
	DataNamespace string   `json:"data_namespace,omitempty"`
}

type RestartPolicy struct {
	Mode        string `json:"mode,omitempty"`
	MaxAttempts int    `json:"max_attempts,omitempty"`
	CooldownMS  int    `json:"cooldown_ms,omitempty"`
}
type ResourcePolicy struct {
	MemoryMB  int `json:"memory_mb,omitempty"`
	CPUWeight int `json:"cpu_weight,omitempty"`
}
type Plugin struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}
type Consumes struct {
	Required []Capability `json:"required,omitempty"`
	Optional []Capability `json:"optional,omitempty"`
}

type Manifest struct {
	ManifestVersion int            `json:"manifest_version"`
	Plugin          Plugin         `json:"plugin"`
	Runtime         Runtime        `json:"runtime"`
	Exports         []Capability   `json:"exports,omitempty"`
	Consumes        Consumes       `json:"consumes,omitempty"`
	Publishes       []Capability   `json:"publishes,omitempty"`
	Subscribes      []Capability   `json:"subscribes,omitempty"`
	Permissions     []string       `json:"permissions,omitempty"`
	Restart         RestartPolicy  `json:"restart,omitempty"`
	Resources       ResourcePolicy `json:"resources,omitempty"`
}

func Load(path string) (Manifest, string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, "", err
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return Manifest{}, "", err
	}
	if err := Validate(m); err != nil {
		return Manifest{}, "", err
	}
	sum := sha256.Sum256(b)
	return m, hex.EncodeToString(sum[:]), nil
}

func Validate(m Manifest) error {
	if m.ManifestVersion != 1 {
		return fmt.Errorf("unsupported manifest_version %d", m.ManifestVersion)
	}
	if m.Plugin.ID == "" || m.Plugin.Version == "" {
		return fmt.Errorf("plugin id and version are required")
	}
	if m.Runtime.Protocol == "" || m.Runtime.Executable == "" {
		return fmt.Errorf("runtime protocol and executable are required")
	}
	if m.Runtime.Protocol != protocol.RuntimeProtocol {
		return fmt.Errorf("runtime protocol mismatch: %s", m.Runtime.Protocol)
	}
	if ns := m.Runtime.DataNamespace; ns != "" {
		clean := filepath.Clean(ns)
		if filepath.IsAbs(ns) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return fmt.Errorf("invalid runtime.data_namespace %q", ns)
		}
	}
	seen := map[string]struct{}{}
	hasStateful := false
	for _, c := range m.Exports {
		if c.Name == "" || c.Major <= 0 {
			return fmt.Errorf("invalid exported capability")
		}
		if c.Contract != fmt.Sprintf("%s@%d", c.Name, c.Major) {
			return fmt.Errorf("export %s@%d contract identity must be %s@%d", c.Name, c.Major, c.Name, c.Major)
		}
		if c.Mode == "" {
			c.Mode = "stateless"
		}
		if c.Mode != "stateless" && c.Mode != "stateful" {
			return fmt.Errorf("invalid mode for %s@%d", c.Name, c.Major)
		}
		if c.Mode == "stateful" {
			hasStateful = true
			if c.Service == "" || c.Authority == "" {
				return fmt.Errorf("stateful %s@%d requires service and authority", c.Name, c.Major)
			}
		}
		key := fmt.Sprintf("%s@%d/%s/%s", c.Name, c.Major, c.Service, c.Authority)
		if _, ok := seen[key]; ok {
			return fmt.Errorf("duplicate exported capability %s", key)
		}
		seen[key] = struct{}{}
	}
	if hasStateful && m.Runtime.DataNamespace == "" {
		return fmt.Errorf("stateful plugin %s requires runtime.data_namespace as storage identity", m.Plugin.ID)
	}
	if m.Resources.MemoryMB < 0 {
		return fmt.Errorf("resources.memory_mb must be non-negative")
	}
	if m.Resources.CPUWeight < 0 || m.Resources.CPUWeight > 100 {
		return fmt.Errorf("resources.cpu_weight must be between 0 and 100")
	}
	for _, list := range [][]Capability{m.Consumes.Required, m.Consumes.Optional, m.Publishes, m.Subscribes} {
		for _, c := range list {
			if c.Name == "" || c.Major <= 0 || c.Contract != fmt.Sprintf("%s@%d", c.Name, c.Major) {
				return fmt.Errorf("invalid or mismatched declared contract for %s@%d", c.Name, c.Major)
			}
		}
	}
	perms := append([]string(nil), m.Permissions...)
	sort.Strings(perms)
	for i := 1; i < len(perms); i++ {
		if perms[i] == perms[i-1] {
			return fmt.Errorf("duplicate permission %q", perms[i])
		}
	}
	return nil
}

func HasCapability(list []Capability, name string, major int) bool {
	for _, c := range list {
		if c.Name == name && c.Major == major {
			return true
		}
	}
	return false
}
func FindCapability(list []Capability, name string, major int) (Capability, bool) {
	for _, c := range list {
		if c.Name == name && c.Major == major {
			return c, true
		}
	}
	return Capability{}, false
}
