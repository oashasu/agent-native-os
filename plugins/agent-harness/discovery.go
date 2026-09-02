package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var argvTemplates = map[string]func(RunSpec) []string{
	"codex": codexArgv,
}

var defaultEnvAllowlist = []string{
	"PATH", "HOME", "USER", "LOGNAME", "SHELL", "LANG", "LC_ALL", "LC_CTYPE",
	"TERM", "TMPDIR", "TZ", "SSL_CERT_FILE", "SSL_CERT_DIR",
	"CODEX_HOME", "OPENAI_API_KEY", "CODEX_API_KEY",
}

func splitList(v string) []string {
	var out []string
	seen := map[string]bool{}
	for _, p := range strings.Split(v, ",") {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

func candidatesFromEnv() []string {
	if c := splitList(os.Getenv("VIBE_AGENT_PROVIDERS")); len(c) > 0 {
		return c
	}
	return []string{"codex"}
}

func allowlistFromEnv() []string {
	if c := splitList(os.Getenv("VIBE_AGENT_ENV_ALLOWLIST")); len(c) > 0 {
		return c
	}
	return append([]string(nil), defaultEnvAllowlist...)
}

func denied(name string) bool {
	return strings.HasPrefix(name, "FAKE_AGENT_") || strings.HasPrefix(name, "VIBE_")
}

func allowlistedEnv(names []string) []string {
	out := make([]string, 0, len(names)) // non-nil
	for _, n := range names {
		if denied(n) {
			continue
		}
		if v, ok := os.LookupEnv(n); ok {
			out = append(out, n+"="+v)
		}
	}
	return out
}

func discoverProviders(candidates, envAllowlist []string, logw io.Writer) map[string]Provider {
	m := map[string]Provider{"mock": MockProvider{}}
	logf := func(f string, a ...any) { fmt.Fprintf(logw, "agent-harness: "+f+"\n", a...) }

	if !realProviderSupported() {
		logf("platform has no process-group support; no real providers")
		return m
	}
	env := allowlistedEnv(envAllowlist)

	for _, name := range candidates {
		switch {
		case name == "mock":
			logf("candidate %q is reserved; skipped", name)
			continue
		case argvTemplates[name] == nil:
			logf("no argv template for %q; skipped", name)
			continue
		}
		bin, err := exec.LookPath(name)
		if err != nil {
			logf("provider %q not on PATH; skipped", name)
			continue
		}
		abs, err := filepath.Abs(bin)
		if err != nil {
			logf("provider %q path resolution failed: %v; skipped", name, err)
			continue
		}
		if err := probeVersion(abs, env); err != nil {
			logf("provider %q probe failed: %v; skipped", name, err)
			continue
		}
		m[name] = RealProvider{name: name, bin: abs, argv: argvTemplates[name], env: env, timeout: 0}
		logf("provider %q -> %s (registered)", name, abs)
	}
	return m
}

func probeVersion(bin string, env []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.Command(bin, "--version")
	cmd.Env = env
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("devnull: %w", err)
	}
	defer devnull.Close()
	cmd.Stdout = devnull
	cmd.Stderr = devnull
	if err := startProcess(cmd); err != nil {
		return err
	}
	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()
	select {
	case e := <-waitErr:
		return e
	case <-ctx.Done():
		killProcessGroup(cmd)
		select {
		case <-waitErr:
		case <-time.After(3 * time.Second):
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			<-waitErr
		}
		return fmt.Errorf("--version timed out")
	}
}
