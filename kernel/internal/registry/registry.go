package registry

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"

	"github.com/example/agent-native-microkernel/internal/contractmeta"
	"github.com/example/agent-native-microkernel/internal/manifest"
	"github.com/example/agent-native-microkernel/sdk/go/protocol"
)

type Provider struct {
	PluginID, RuntimeID string
	WriterEpoch         int64
	FailureCount        int
	HeartbeatFailures   int
	ProcessHealthy      bool
	DependenciesHealthy bool
	CircuitOpen         bool
	Healthy             bool
	Manifest            manifest.Manifest
	Export              manifest.Capability
}

type key struct {
	name  string
	major int
}

type Binding struct {
	Service   string `json:"service,omitempty"`
	Authority string `json:"authority,omitempty"`
	Provider  string `json:"provider,omitempty"`
}

type authorityKey struct {
	service   string
	authority string
}

type writerLease struct {
	RuntimeID string
	Epoch     int64
}

type Registry struct {
	mu               sync.RWMutex
	providers        map[key][]Provider
	byPlugin         map[string]manifest.Manifest
	bindings         map[key]Binding
	authorityStorage map[authorityKey]string
	contracts        *contractmeta.Catalog
	writers          map[authorityKey]writerLease
	fenceRoot        string
}

func New() *Registry {
	return &Registry{
		providers:        map[key][]Provider{},
		byPlugin:         map[string]manifest.Manifest{},
		bindings:         map[key]Binding{},
		authorityStorage: map[authorityKey]string{},
		writers:          map[authorityKey]writerLease{},
	}
}

func (r *Registry) SetFenceRoot(root string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fenceRoot = root
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	paths, err := filepath.Glob(filepath.Join(root, "*.json"))
	if err != nil {
		return err
	}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		var v struct {
			Service   string `json:"service"`
			Authority string `json:"authority"`
			Epoch     int64  `json:"epoch"`
		}
		if err = json.Unmarshal(b, &v); err != nil {
			return fmt.Errorf("load fence %s: %w", p, err)
		}
		if v.Service != "" && v.Authority != "" {
			// Runtime ownership never survives a host restart, but the epoch must.
			r.writers[authorityKey{v.Service, v.Authority}] = writerLease{Epoch: v.Epoch}
		}
	}
	return nil
}

func fenceSafe(s string) string {
	return strings.NewReplacer("/", "_", "\\", "_", ":", "_").Replace(s)
}

func (r *Registry) persistFenceLocked(ak authorityKey, l writerLease) error {
	if r.fenceRoot == "" {
		return fmt.Errorf("fence root not configured")
	}
	base := fenceSafe(ak.service) + "--" + fenceSafe(ak.authority)
	leasePath := filepath.Join(r.fenceRoot, base+".json")
	lockPath := filepath.Join(r.fenceRoot, base+".lock")
	lf, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer lf.Close()
	if err = syscall.Flock(int(lf.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lf.Fd()), syscall.LOCK_UN) //nolint:errcheck

	b, err := json.Marshal(map[string]any{
		"service": ak.service, "authority": ak.authority,
		"runtime_id": l.RuntimeID, "epoch": l.Epoch,
	})
	if err != nil {
		return err
	}
	tmp := leasePath + ".tmp"
	if err = os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, leasePath)
}

func (r *Registry) SetContractCatalog(c *contractmeta.Catalog) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.contracts = c
}

func (r *Registry) ContractMetadata(contract string) (contractmeta.Metadata, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.contracts == nil {
		return contractmeta.Metadata{}, false
	}
	return r.contracts.Metadata(contract)
}

// AddManifest is runtime admission, not a best-effort repository lint. It is
// atomic: validation completes before any plugin identity/authority is committed.
func (r *Registry) AddManifest(m manifest.Manifest) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.byPlugin[m.Plugin.ID]; exists {
		return fmt.Errorf("duplicate plugin id %s", m.Plugin.ID)
	}
	if r.contracts == nil {
		return fmt.Errorf("contract catalog not configured")
	}

	pendingAuthorities := map[authorityKey]string{}
	for _, c := range m.Exports {
		meta, ok := r.contracts.Metadata(c.Contract)
		if !ok {
			return fmt.Errorf("unknown contract %s for %s@%d", c.Contract, c.Name, c.Major)
		}
		canonical := fmt.Sprintf("%s@%d", c.Name, c.Major)
		if c.Contract != canonical || meta.Contract != canonical {
			return fmt.Errorf("contract mismatch for %s@%d: manifest=%q catalog=%q", c.Name, c.Major, c.Contract, meta.Contract)
		}
		cm := c.Mode
		if cm == "" {
			cm = "stateless"
		}
		for _, other := range r.byPlugin {
			for _, oc := range other.Exports {
				if oc.Name != c.Name || oc.Major != c.Major {
					continue
				}
				om := oc.Mode
				if om == "" {
					om = "stateless"
				}
				if om != cm {
					return fmt.Errorf("mixed stateful/stateless providers for %s@%d", c.Name, c.Major)
				}
			}
		}
		if cm == "stateful" {
			if c.Service == "" || c.Authority == "" || m.Runtime.DataNamespace == "" {
				return fmt.Errorf("stateful provider %s for %s@%d requires service, authority and data_namespace", m.Plugin.ID, c.Name, c.Major)
			}
			ak := authorityKey{c.Service, c.Authority}
			if existing, ok := r.authorityStorage[ak]; ok && existing != m.Runtime.DataNamespace {
				return fmt.Errorf("authority storage conflict for %s/%s: existing=%q new=%q", c.Service, c.Authority, existing, m.Runtime.DataNamespace)
			}
			if existing, ok := pendingAuthorities[ak]; ok && existing != m.Runtime.DataNamespace {
				return fmt.Errorf("manifest declares conflicting storage for %s/%s", c.Service, c.Authority)
			}
			pendingAuthorities[ak] = m.Runtime.DataNamespace
		}
	}
	for _, group := range []struct {
		name      string
		list      []manifest.Capability
		wantEvent bool
	}{
		{name: "consume.required", list: m.Consumes.Required},
		{name: "consume.optional", list: m.Consumes.Optional},
		{name: "publishes", list: m.Publishes, wantEvent: true},
		{name: "subscribes", list: m.Subscribes, wantEvent: true},
	} {
		for _, c := range group.list {
			meta, ok := r.contracts.Metadata(c.Contract)
			if !ok {
				return fmt.Errorf("unknown declared contract %s", c.Contract)
			}
			canonical := fmt.Sprintf("%s@%d", c.Name, c.Major)
			if c.Contract != canonical || meta.Contract != canonical {
				return fmt.Errorf("declared contract mismatch for %s@%d: %q", c.Name, c.Major, c.Contract)
			}
			if group.wantEvent && meta.Kind != protocol.KindEvent {
				return fmt.Errorf("%s %s must use event contract, got kind=%s", group.name, canonical, meta.Kind)
			}
			if !group.wantEvent && meta.Kind == protocol.KindEvent {
				return fmt.Errorf("%s %s cannot consume event contract as request/reply capability", group.name, canonical)
			}
		}
	}

	r.byPlugin[m.Plugin.ID] = m
	for ak, ns := range pendingAuthorities {
		r.authorityStorage[ak] = ns
	}
	return nil
}

func (r *Registry) SetBinding(name string, major int, b Binding) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bindings[key{name, major}] = b
}

func (r *Registry) RegisterRuntime(m manifest.Manifest, runtimeID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Admission has already reserved the logical authority. Recheck because a
	// runtime may never bypass the same storage-identity invariant.
	for _, c := range m.Exports {
		mode := c.Mode
		if mode == "" {
			mode = "stateless"
		}
		if mode != "stateful" {
			continue
		}
		ak := authorityKey{service: c.Service, authority: c.Authority}
		if existing, ok := r.authorityStorage[ak]; ok && existing != m.Runtime.DataNamespace {
			return fmt.Errorf("authority storage conflict for service=%q authority=%q: existing=%q provider=%s declares=%q", c.Service, c.Authority, existing, m.Plugin.ID, m.Runtime.DataNamespace)
		}
	}

	// Commit authority storage reservations only after all runtime declarations validate.
	for _, c := range m.Exports {
		mode := c.Mode
		if mode == "" {
			mode = "stateless"
		}
		if mode == "stateful" {
			r.authorityStorage[authorityKey{service: c.Service, authority: c.Authority}] = m.Runtime.DataNamespace
		}
	}

	for _, c := range m.Exports {
		k := key{c.Name, c.Major}
		mode := c.Mode
		if mode == "" {
			mode = "stateless"
		}
		c.Mode = mode
		p := Provider{
			PluginID: m.Plugin.ID, RuntimeID: runtimeID,
			Healthy: true, ProcessHealthy: true, DependenciesHealthy: true, Manifest: m, Export: c,
		}
		r.providers[k] = append(r.providers[k], p)
		sort.SliceStable(r.providers[k], func(i, j int) bool {
			a, b := r.providers[k][i], r.providers[k][j]
			if a.Export.Priority != b.Export.Priority {
				return a.Export.Priority > b.Export.Priority
			}
			return a.PluginID < b.PluginID
		})
	}
	return nil
}

func recomputeHealth(p *Provider) {
	p.Healthy = p.ProcessHealthy && p.DependenciesHealthy && !p.CircuitOpen && p.HeartbeatFailures < 3
}

func (r *Registry) clearWriterIfUnhealthyLocked(runtimeID string) {
	for ak, l := range r.writers {
		if l.RuntimeID == runtimeID {
			// Keep epoch; next writer must advance it. Clearing in memory is
			// enough because promotion persists a new monotonically higher lease.
			l.RuntimeID = ""
			r.writers[ak] = l
		}
	}
}

// RemoveRuntime prunes every Provider entry for a runtime that has exited. If
// that runtime held an authority's active writer lease, the lease RuntimeID is
// cleared while its epoch is preserved, so the next writer must still advance it.
// Callers should MarkHealth(false) first so dependents observe the loss before
// the entries disappear.
func (r *Registry) RemoveRuntime(runtimeID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k, ps := range r.providers {
		kept := ps[:0]
		for _, p := range ps {
			if p.RuntimeID != runtimeID {
				kept = append(kept, p)
			}
		}
		if len(kept) == 0 {
			delete(r.providers, k)
		} else {
			r.providers[k] = kept
		}
	}
	r.clearWriterIfUnhealthyLocked(runtimeID)
}

func (r *Registry) MarkHealth(runtimeID string, healthy bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	becameUnhealthy := false
	for k, ps := range r.providers {
		for i := range ps {
			if ps[i].RuntimeID == runtimeID {
				ps[i].ProcessHealthy = healthy
				recomputeHealth(&ps[i])
				if !ps[i].Healthy {
					becameUnhealthy = true
				}
			}
		}
		r.providers[k] = ps
	}
	if becameUnhealthy {
		r.clearWriterIfUnhealthyLocked(runtimeID)
	}
}

// RecordFailure is the request-path failure score/circuit breaker. Heartbeat
// success deliberately does not close this circuit: a process can answer ping
// while every business handler is hung.

func (r *Registry) MarkDependencyHealth(runtimeID string, healthy bool) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	changed := false
	becameUnhealthy := false
	for k, ps := range r.providers {
		for i := range ps {
			if ps[i].RuntimeID == runtimeID {
				before := ps[i].Healthy
				if ps[i].DependenciesHealthy != healthy {
					changed = true
				}
				ps[i].DependenciesHealthy = healthy
				recomputeHealth(&ps[i])
				if before && !ps[i].Healthy {
					becameUnhealthy = true
				}
			}
		}
		r.providers[k] = ps
	}
	if becameUnhealthy {
		r.clearWriterIfUnhealthyLocked(runtimeID)
	}
	return changed
}

func (r *Registry) RuntimeHealthy(runtimeID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	found := false
	for _, ps := range r.providers {
		for _, p := range ps {
			if p.RuntimeID == runtimeID {
				found = true
				if !p.Healthy {
					return false
				}
			}
		}
	}
	// Plugins with no exported capability are still runtime-healthy from the
	// registry's perspective; the supervisor tracks their process lifecycle.
	return found
}

func (r *Registry) RecordFailure(runtimeID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	opened := false
	for k, ps := range r.providers {
		for i := range ps {
			if ps[i].RuntimeID == runtimeID {
				ps[i].FailureCount++
				if ps[i].FailureCount >= 3 {
					ps[i].CircuitOpen = true
				}
				recomputeHealth(&ps[i])
				if !ps[i].Healthy {
					opened = true
				}
			}
		}
		r.providers[k] = ps
	}
	if opened {
		r.clearWriterIfUnhealthyLocked(runtimeID)
	}
}

func (r *Registry) RecordSuccess(runtimeID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k, ps := range r.providers {
		for i := range ps {
			if ps[i].RuntimeID == runtimeID {
				ps[i].FailureCount = 0
				ps[i].CircuitOpen = false
				recomputeHealth(&ps[i])
			}
		}
		r.providers[k] = ps
	}
}

func (r *Registry) RecordHeartbeatFailure(runtimeID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	unhealthy := false
	for k, ps := range r.providers {
		for i := range ps {
			if ps[i].RuntimeID == runtimeID {
				ps[i].HeartbeatFailures++
				recomputeHealth(&ps[i])
				if !ps[i].Healthy {
					unhealthy = true
				}
			}
		}
		r.providers[k] = ps
	}
	if unhealthy {
		r.clearWriterIfUnhealthyLocked(runtimeID)
	}
}

func (r *Registry) RecordHeartbeatSuccess(runtimeID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k, ps := range r.providers {
		for i := range ps {
			if ps[i].RuntimeID == runtimeID {
				ps[i].HeartbeatFailures = 0
				// Do not clear CircuitOpen here.
				recomputeHealth(&ps[i])
			}
		}
		r.providers[k] = ps
	}
}

// Resolve is retained for compatibility with tests/callers; Query semantics are
// used because legacy callers did not carry an operation kind.
func (r *Registry) Resolve(name string, major int, providerHint, serviceHint, authorityHint string) (Provider, error) {
	return r.ResolveForKind(name, major, providerHint, serviceHint, authorityHint, protocol.KindQuery)
}

// ResolveForKind enforces logical authority and single-writer fencing for
// stateful commands. Queries may use any healthy replica in the same authority;
// commands are routed only to the current fenced writer.
func (r *Registry) ResolveForKind(name string, major int, providerHint, serviceHint, authorityHint string, kind protocol.Kind) (Provider, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := key{name, major}
	ps := r.providers[k]
	if len(ps) == 0 {
		return Provider{}, fmt.Errorf("no provider for %s@%d", name, major)
	}
	b := r.bindings[k]
	if serviceHint == "" {
		serviceHint = b.Service
	}
	if authorityHint == "" {
		authorityHint = b.Authority
	}
	if providerHint == "" {
		providerHint = b.Provider
	}

	stateful := ps[0].Export.Mode == "stateful"
	if !stateful {
		for _, p := range ps {
			if !p.Healthy {
				continue
			}
			if providerHint != "" && p.PluginID != providerHint {
				continue
			}
			return p, nil
		}
		return Provider{}, errors.New("all providers unhealthy or filtered")
	}

	// Infer authority only when the capability has exactly one logical pair.
	if serviceHint == "" || authorityHint == "" {
		pairs := map[authorityKey]struct{}{}
		for _, p := range ps {
			pairs[authorityKey{p.Export.Service, p.Export.Authority}] = struct{}{}
		}
		if len(pairs) != 1 {
			return Provider{}, fmt.Errorf("stateful %s@%d requires logical service/authority binding; found %d authorities", name, major, len(pairs))
		}
		for ak := range pairs {
			if serviceHint == "" {
				serviceHint = ak.service
			}
			if authorityHint == "" {
				authorityHint = ak.authority
			}
		}
	}
	ak := authorityKey{serviceHint, authorityHint}
	candidates := make([]Provider, 0, len(ps))
	for _, p := range ps {
		if !p.Healthy || p.Export.Service != serviceHint || p.Export.Authority != authorityHint {
			continue
		}
		candidates = append(candidates, p)
	}
	if len(candidates) == 0 {
		return Provider{}, fmt.Errorf("stateful authority unavailable for %s@%d service=%q authority=%q", name, major, serviceHint, authorityHint)
	}

	if kind != protocol.KindCommand {
		for _, p := range candidates {
			if providerHint == "" || p.PluginID == providerHint {
				return p, nil
			}
		}
		return Provider{}, fmt.Errorf("provider %q unavailable for stateful authority %s/%s", providerHint, serviceHint, authorityHint)
	}

	lease := r.writers[ak]
	if lease.RuntimeID != "" {
		for _, p := range candidates {
			if p.RuntimeID == lease.RuntimeID {
				if providerHint != "" && p.PluginID != providerHint {
					return Provider{}, fmt.Errorf("provider hint %q is not active writer for %s/%s", providerHint, serviceHint, authorityHint)
				}
				p.WriterEpoch = lease.Epoch
				return p, nil
			}
		}
		// Active writer is no longer healthy/available. Preserve epoch and promote below.
		lease.RuntimeID = ""
	}

	var chosen *Provider
	for i := range candidates {
		if providerHint != "" && candidates[i].PluginID != providerHint {
			continue
		}
		chosen = &candidates[i]
		break
	}
	if chosen == nil {
		return Provider{}, fmt.Errorf("provider %q unavailable for stateful authority %s/%s", providerHint, serviceHint, authorityHint)
	}
	lease.Epoch++
	lease.RuntimeID = chosen.RuntimeID
	if err := r.persistFenceLocked(ak, lease); err != nil {
		return Provider{}, fmt.Errorf("persist writer fence: %w", err)
	}
	r.writers[ak] = lease
	chosen.WriterEpoch = lease.Epoch
	return *chosen, nil
}

func (r *Registry) Manifest(pluginID string) (manifest.Manifest, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m, ok := r.byPlugin[pluginID]
	return m, ok
}

func (r *Registry) Providers(name string, major int) []Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]Provider(nil), r.providers[key{name, major}]...)
}

func (r *Registry) MissingHealthyRequired(m manifest.Manifest) []manifest.Capability {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var missing []manifest.Capability
	for _, need := range m.Consumes.Required {
		found := false
		for _, p := range r.providers[key{need.Name, need.Major}] {
			if p.Healthy {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, need)
		}
	}
	return missing
}

func (r *Registry) MissingHealthyOptional(m manifest.Manifest) []manifest.Capability {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var missing []manifest.Capability
	for _, need := range m.Consumes.Optional {
		found := false
		for _, p := range r.providers[key{need.Name, need.Major}] {
			if p.Healthy {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, need)
		}
	}
	return missing
}

// MissingRequired is a repository/install-time helper and intentionally checks
// declarations rather than runtime health.
func MissingRequired(m manifest.Manifest, installed []manifest.Manifest) []manifest.Capability {
	var missing []manifest.Capability
	for _, need := range m.Consumes.Required {
		found := false
		for _, candidate := range installed {
			if manifest.HasCapability(candidate.Exports, need.Name, need.Major) {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, need)
		}
	}
	return missing
}
