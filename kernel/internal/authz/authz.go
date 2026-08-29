package authz

import (
	"fmt"
	"github.com/example/agent-native-microkernel/internal/manifest"
	"path"
	"sync"
)

type Grant struct {
	Capabilities     []string            `json:"capabilities,omitempty"` // invoke/query/command
	Publishes        []string            `json:"publishes,omitempty"`
	Subscribes       []string            `json:"subscribes,omitempty"`
	HostPermissions  []string            `json:"host_permissions,omitempty"`
	ServiceAuthority bool                `json:"service_authority,omitempty"`
	Delegations      map[string][]string `json:"delegations,omitempty"` // root capability pattern -> delegated child capability patterns
}
type Engine struct {
	mu     sync.RWMutex
	grants map[string]Grant
}

func New() *Engine { return &Engine{grants: map[string]Grant{}} }
func (e *Engine) SetGrant(identity string, g Grant) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.grants[identity] = g
}
func match(pattern, value string) bool {
	ok, err := path.Match(pattern, value)
	return err == nil && ok
}
func (e *Engine) granted(identity, cap string, major int) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	g, ok := e.grants[identity]
	if !ok {
		return false
	}
	v := fmt.Sprintf("%s@%d", cap, major)
	for _, p := range g.Capabilities {
		if match(p, v) {
			return true
		}
	}
	return false
}
func (e *Engine) CanConsume(caller manifest.Manifest, capability string, major int) error {
	if !(manifest.HasCapability(caller.Consumes.Required, capability, major) || manifest.HasCapability(caller.Consumes.Optional, capability, major)) {
		return fmt.Errorf("plugin %s did not declare consume %s@%d", caller.Plugin.ID, capability, major)
	}
	if !e.granted(caller.Plugin.ID, capability, major) {
		return fmt.Errorf("host policy did not grant %s permission for %s@%d", caller.Plugin.ID, capability, major)
	}
	return nil
}
func (e *Engine) CanExternal(identity, capability string, major int) error {
	if !e.granted(identity, capability, major) {
		return fmt.Errorf("host policy did not grant external identity %s permission for %s@%d", identity, capability, major)
	}
	return nil
}

func matchList(patterns []string, name string, major int) bool {
	v := fmt.Sprintf("%s@%d", name, major)
	for _, p := range patterns {
		if match(p, v) {
			return true
		}
	}
	return false
}
func (e *Engine) CanPublish(caller manifest.Manifest, event string, major int) error {
	if !manifest.HasCapability(caller.Publishes, event, major) {
		return fmt.Errorf("plugin %s did not declare publish %s@%d", caller.Plugin.ID, event, major)
	}
	e.mu.RLock()
	g, ok := e.grants[caller.Plugin.ID]
	e.mu.RUnlock()
	if !ok || !matchList(g.Publishes, event, major) {
		return fmt.Errorf("host policy did not grant %s publish permission for %s@%d", caller.Plugin.ID, event, major)
	}
	return nil
}
func (e *Engine) CanSubscribe(caller manifest.Manifest, event string, major int) error {
	if !manifest.HasCapability(caller.Subscribes, event, major) {
		return fmt.Errorf("plugin %s did not declare subscribe %s@%d", caller.Plugin.ID, event, major)
	}
	e.mu.RLock()
	g, ok := e.grants[caller.Plugin.ID]
	e.mu.RUnlock()
	if !ok || !matchList(g.Subscribes, event, major) {
		return fmt.Errorf("host policy did not grant %s subscribe permission for %s@%d", caller.Plugin.ID, event, major)
	}
	return nil
}

func (e *Engine) DelegationScope(identity, rootCapability string, rootMajor int) []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	g, ok := e.grants[identity]
	if !ok {
		return nil
	}
	root := fmt.Sprintf("%s@%d", rootCapability, rootMajor)
	out := []string{}
	for trigger, scope := range g.Delegations {
		if match(trigger, root) {
			out = append(out, scope...)
		}
	}
	return out
}

func ScopeAllows(scope []string, capability string, major int) bool {
	v := fmt.Sprintf("%s@%d", capability, major)
	for _, p := range scope {
		if match(p, v) {
			return true
		}
	}
	return false
}

func (e *Engine) CanInvokeAsService(identity string) error {
	e.mu.RLock()
	g, ok := e.grants[identity]
	e.mu.RUnlock()
	if !ok || !g.ServiceAuthority {
		return fmt.Errorf("host policy did not grant %s service_authority", identity)
	}
	return nil
}

func (e *Engine) HasHostPermission(identity string, permission string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	g := e.grants[identity]
	for _, p := range g.HostPermissions {
		if match(p, permission) {
			return true
		}
	}
	return false
}
