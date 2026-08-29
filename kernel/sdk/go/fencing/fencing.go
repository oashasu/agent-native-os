package fencing

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/example/agent-native-microkernel/sdk/go/protocol"
)

type Lease struct {
	Service   string `json:"service"`
	Authority string `json:"authority"`
	RuntimeID string `json:"runtime_id"`
	Epoch     int64  `json:"epoch"`
}

func safe(s string) string {
	r := strings.NewReplacer("/", "_", "\\", "_", ":", "_")
	return r.Replace(s)
}
func Paths(root, service, authority string) (string, string) {
	base := safe(service) + "--" + safe(authority)
	return filepath.Join(root, base+".json"), filepath.Join(root, base+".lock")
}

// WithWriteFence serializes state mutation with host lease promotion and rejects stale writers.
func WithWriteFence(e protocol.Envelope, fn func() error) error {
	if e.FencingEpoch <= 0 || e.Service == "" || e.Authority == "" {
		return fmt.Errorf("missing stateful fencing metadata")
	}
	root := os.Getenv("VIBE_FENCE_ROOT")
	if root == "" {
		return fmt.Errorf("VIBE_FENCE_ROOT not configured")
	}
	leasePath, lockPath := Paths(root, e.Service, e.Authority)
	if err := os.MkdirAll(root, 0755); err != nil {
		return err
	}
	lf, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	defer lf.Close()
	if err = syscall.Flock(int(lf.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lf.Fd()), syscall.LOCK_UN)
	b, err := os.ReadFile(leasePath)
	if err != nil {
		return err
	}
	var l Lease
	if err = json.Unmarshal(b, &l); err != nil {
		return err
	}
	if l.RuntimeID != os.Getenv("VIBE_RUNTIME_ID") || l.Epoch != e.FencingEpoch {
		return fmt.Errorf("stale writer fence: lease runtime=%s epoch=%d, request runtime=%s epoch=%d", l.RuntimeID, l.Epoch, os.Getenv("VIBE_RUNTIME_ID"), e.FencingEpoch)
	}
	return fn()
}
