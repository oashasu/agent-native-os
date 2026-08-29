package supervisor

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/example/agent-native-microkernel/internal/manifest"
	"github.com/example/agent-native-microkernel/internal/registry"
	"github.com/example/agent-native-microkernel/internal/router"
	"github.com/example/agent-native-microkernel/sdk/go/protocol"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Runtime struct {
	ID                         string
	Manifest                   manifest.Manifest
	Cmd                        *exec.Cmd
	Client                     *router.ProcessClient
	Started                    time.Time
	ManifestPath, ManifestHash string
	Attempts                   int
}

type PluginState string

const (
	StateInstalled PluginState = "INSTALLED"
	StateStarting  PluginState = "STARTING"
	StateReady     PluginState = "READY"
	StateDegraded  PluginState = "DEGRADED"
	StateBlocked   PluginState = "BLOCKED"
	StateFailed    PluginState = "FAILED"
	StateStopped   PluginState = "STOPPED"
)

type PluginStatus struct {
	PluginID  string      `json:"plugin_id"`
	State     PluginState `json:"state"`
	RuntimeID string      `json:"runtime_id,omitempty"`
	Reason    string      `json:"reason,omitempty"`
}
type Supervisor struct {
	reg       *registry.Registry
	router    *router.Router
	mu        sync.Mutex
	runs      map[string]*Runtime
	manifests map[string]manifest.Manifest
	statuses  map[string]PluginStatus
	ctx       context.Context
}

func New(reg *registry.Registry, rt *router.Router) *Supervisor {
	return &Supervisor{reg: reg, router: rt, runs: map[string]*Runtime{}, manifests: map[string]manifest.Manifest{}, statuses: map[string]PluginStatus{}}
}
func (s *Supervisor) Track(m manifest.Manifest) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.manifests[m.Plugin.ID] = m
	if _, ok := s.statuses[m.Plugin.ID]; !ok {
		s.statuses[m.Plugin.ID] = PluginStatus{PluginID: m.Plugin.ID, State: StateInstalled}
	}
}
func (s *Supervisor) setStatus(pluginID string, state PluginState, runtimeID, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statuses[pluginID] = PluginStatus{PluginID: pluginID, State: state, RuntimeID: runtimeID, Reason: reason}
}
func (s *Supervisor) MarkBlocked(m manifest.Manifest, reason string) {
	s.Track(m)
	s.setStatus(m.Plugin.ID, StateBlocked, "", reason)
}

// MarkManifestRejected records a manifest that failed to load or be admitted.
// id may be empty when the manifest did not parse; the file path is then used as
// the key so the failure is still visible via Status/Statuses.
func (s *Supervisor) MarkManifestRejected(id, path, reason string) {
	key := id
	if key == "" {
		key = path
	}
	s.setStatus(key, StateFailed, "", reason)
}
func (s *Supervisor) Status(pluginID string) (PluginStatus, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.statuses[pluginID]
	return v, ok
}
func (s *Supervisor) Statuses() []PluginStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]PluginStatus, 0, len(s.statuses))
	for _, v := range s.statuses {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PluginID < out[j].PluginID })
	return out
}
func (s *Supervisor) reconcileDependencies() {
	// Dependency health can cascade, so iterate to a fixed point.
	for pass := 0; pass < 8; pass++ {
		changed := false
		s.mu.Lock()
		runs := make([]*Runtime, 0, len(s.runs))
		for _, r := range s.runs {
			runs = append(runs, r)
		}
		s.mu.Unlock()
		for _, run := range runs {
			missingReq := s.reg.MissingHealthyRequired(run.Manifest)
			depOK := len(missingReq) == 0
			if s.reg.MarkDependencyHealth(run.ID, depOK) {
				changed = true
			}
			if !depOK {
				s.setStatus(run.Manifest.Plugin.ID, StateBlocked, run.ID, fmt.Sprintf("missing healthy required capabilities: %v", missingReq))
				continue
			}
			missingOpt := s.reg.MissingHealthyOptional(run.Manifest)
			if len(missingOpt) > 0 {
				s.setStatus(run.Manifest.Plugin.ID, StateDegraded, run.ID, fmt.Sprintf("missing optional capabilities: %v", missingOpt))
			} else {
				s.setStatus(run.Manifest.Plugin.ID, StateReady, run.ID, "")
			}
		}
		if !changed {
			break
		}
	}
}
func (s *Supervisor) Start(ctx context.Context, m manifest.Manifest, manifestPath, manifestHash string) (*Runtime, error) {
	s.ctx = ctx
	s.Track(m)
	s.setStatus(m.Plugin.ID, StateStarting, "", "")
	run, err := s.start(ctx, m, manifestPath, manifestHash, 0)
	if err != nil {
		s.setStatus(m.Plugin.ID, StateFailed, "", err.Error())
	}
	return run, err
}
func (s *Supervisor) start(ctx context.Context, m manifest.Manifest, manifestPath, manifestHash string, attempt int) (*Runtime, error) {
	exe := m.Runtime.Executable
	if !filepath.IsAbs(exe) {
		exe = filepath.Join(filepath.Dir(manifestPath), exe)
	}
	if _, err := os.Stat(exe); err != nil {
		return nil, fmt.Errorf("plugin executable %s: %w", exe, err)
	}
	cmd := exec.CommandContext(ctx, exe, m.Runtime.Args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = os.Stderr
	runtimeID := protocol.NewID("runtime")
	dataRoot := os.Getenv("VIBE_DATA_ROOT")
	if dataRoot == "" {
		dataRoot = "./data"
	}
	ns := m.Runtime.DataNamespace
	if ns == "" {
		ns = m.Plugin.ID
	}
	dataDir := filepath.Join(dataRoot, ns)
	_ = os.MkdirAll(dataDir, 0755)
	cmd.Env = append(os.Environ(), "VIBE_PLUGIN_ID="+m.Plugin.ID, "VIBE_RUNTIME_ID="+runtimeID, "VIBE_DATA_DIR="+dataDir, "VIBE_FENCE_ROOT="+filepath.Join(dataRoot, ".vibe-fences"))
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	if m.Resources.CPUWeight > 0 {
		nice := 19 - (m.Resources.CPUWeight*19)/100
		if err := syscall.Setpriority(syscall.PRIO_PROCESS, cmd.Process.Pid, nice); err != nil {
			_ = cmd.Process.Kill()
			return nil, fmt.Errorf("enforce cpu_weight: %w", err)
		}
	}
	client := router.NewProcessClient(runtimeID, m.Plugin.ID, stdin, stdout)
	hello := protocol.Hello{RuntimeProtocol: protocol.RuntimeProtocol, RuntimeID: runtimeID, PluginID: m.Plugin.ID}
	req := protocol.Envelope{Protocol: 1, MessageID: protocol.NewID("hello"), Kind: protocol.KindHello, Payload: protocol.NewPayload(hello)}
	resp, err := client.Call(req, 5*time.Second)
	if err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("handshake failed: %w", err)
	}
	if resp.Kind != protocol.KindReady {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("expected ready, got %s", resp.Kind)
	}
	var ready protocol.Ready
	if err := json.Unmarshal(resp.Payload, &ready); err != nil {
		_ = cmd.Process.Kill()
		return nil, err
	}
	if ready.RuntimeProtocol != protocol.RuntimeProtocol || ready.PluginID != m.Plugin.ID {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("incompatible ready response: %+v", ready)
	}
	if ready.ManifestHash != "" && ready.ManifestHash != manifestHash {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("manifest hash mismatch")
	}
	for _, ex := range m.Exports {
		meta, ok := s.reg.ContractMetadata(ex.Contract)
		if !ok {
			_ = cmd.Process.Kill()
			return nil, fmt.Errorf("missing contract metadata %s", ex.Contract)
		}
		found := false
		for _, h := range ready.Handlers {
			if h.Capability == ex.Name && h.Major == ex.Major {
				found = true
				if h.Kind != meta.Kind {
					_ = cmd.Process.Kill()
					return nil, fmt.Errorf("handler kind mismatch %s@%d: ready=%s contract=%s", ex.Name, ex.Major, h.Kind, meta.Kind)
				}
				break
			}
		}
		if !found {
			_ = cmd.Process.Kill()
			return nil, fmt.Errorf("provider did not register handler for export %s@%d", ex.Name, ex.Major)
		}
	}
	run := &Runtime{ID: runtimeID, Manifest: m, Cmd: cmd, Client: client, Started: time.Now(), ManifestPath: manifestPath, ManifestHash: manifestHash, Attempts: attempt}
	s.mu.Lock()
	s.runs[runtimeID] = run
	s.mu.Unlock()
	if err := s.reg.RegisterRuntime(m, runtimeID); err != nil {
		_ = cmd.Process.Kill()
		s.mu.Lock()
		delete(s.runs, runtimeID)
		s.mu.Unlock()
		return nil, fmt.Errorf("register runtime: %w", err)
	}
	s.router.AddClient(runtimeID, client)
	s.setStatus(m.Plugin.ID, StateReady, runtimeID, "")
	s.reconcileDependencies()
	go s.watch(run)
	go s.heartbeat(run)
	return run, nil
}

func (s *Supervisor) heartbeat(run *Runtime) {
	t := time.NewTicker(750 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-t.C:
			s.mu.Lock()
			_, alive := s.runs[run.ID]
			s.mu.Unlock()
			if !alive {
				return
			}
			if run.Manifest.Resources.MemoryMB > 0 {
				rss, err := residentBytes(run.Cmd.Process.Pid)
				limit := int64(run.Manifest.Resources.MemoryMB) * 1024 * 1024
				if err != nil || rss > limit {
					// A declared hard resource policy that cannot be measured/enforced fails closed.
					s.reg.MarkHealth(run.ID, false)
					_ = run.Cmd.Process.Kill()
					return
				}
			}
			if err := run.Client.Ping(250 * time.Millisecond); err != nil {
				s.reg.RecordHeartbeatFailure(run.ID)
			} else {
				s.reg.RecordHeartbeatSuccess(run.ID)
			}
			s.reconcileDependencies()
		}
	}
}
func (s *Supervisor) watch(run *Runtime) {
	_ = run.Cmd.Wait()
	s.reg.MarkHealth(run.ID, false)
	s.router.RemoveClient(run.ID)
	s.mu.Lock()
	delete(s.runs, run.ID)
	s.mu.Unlock()
	if s.ctx != nil && s.ctx.Err() != nil {
		s.setStatus(run.Manifest.Plugin.ID, StateStopped, "", "host stopping")
	} else {
		s.setStatus(run.Manifest.Plugin.ID, StateFailed, "", "runtime exited")
	}
	s.reconcileDependencies()
	rp := run.Manifest.Restart
	if rp.Mode == "on_failure" && run.Attempts < rp.MaxAttempts && s.ctx != nil && s.ctx.Err() == nil {
		d := time.Duration(rp.CooldownMS) * time.Millisecond
		if d <= 0 {
			d = 100 * time.Millisecond
		}
		time.Sleep(d)
		s.setStatus(run.Manifest.Plugin.ID, StateStarting, "", "restart")
		if _, err := s.start(s.ctx, run.Manifest, run.ManifestPath, run.ManifestHash, run.Attempts+1); err != nil {
			s.setStatus(run.Manifest.Plugin.ID, StateFailed, "", err.Error())
		}
	}
}
func (s *Supervisor) Stop(runtimeID string) error {
	s.mu.Lock()
	run := s.runs[runtimeID]
	s.mu.Unlock()
	if run == nil {
		return fmt.Errorf("unknown runtime %s", runtimeID)
	}
	if run.Cmd.Process == nil {
		return nil
	}
	return run.Cmd.Process.Kill()
}
func (s *Supervisor) RuntimeIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.runs))
	for id := range s.runs {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func residentBytes(pid int) (int64, error) {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(line, "VmRSS:") {
				f := strings.Fields(line)
				if len(f) >= 2 {
					kb, e := strconv.ParseInt(f[1], 10, 64)
					if e == nil {
						return kb * 1024, nil
					}
				}
			}
		}
	}
	// Portable fallback used on macOS and other Unix hosts. ps rss is KiB.
	out, e := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(pid)).Output()
	if e != nil {
		return 0, e
	}
	kb, e := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if e != nil {
		return 0, e
	}
	return kb * 1024, nil
}
