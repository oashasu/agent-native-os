package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var tmuxSocket = func() string {
	if v := os.Getenv("AGENTDECK_SOCKET"); v != "" {
		return v
	}
	return "agentdeck"
}()

// A dedicated tmux server (-L agentdeck): isolated from the user's own tmux,
// and it keeps agent processes alive when the UI window is closed.
const tmuxConf = `set -g status off
set -g mouse on
set -g history-limit 50000
set -g escape-time 10
set -g default-terminal "xterm-256color"
set -g remain-on-exit failed
`

var idRe = regexp.MustCompile(`^[0-9a-fA-F-]{8,64}$`)

type Config struct {
	ClaudeArgs []string `json:"claude_args"`
	CodexArgs  []string `json:"codex_args"`
	IDECmd     string   `json:"ide_cmd"` // e.g. "code {cwd}"; {cwd} is replaced
}

type Tmux struct {
	conf string
	cfg  Config
}

func NewTmux(dir string, cfg Config) (*Tmux, error) {
	conf := filepath.Join(dir, "tmux.conf")
	if err := os.WriteFile(conf, []byte(tmuxConf), 0o600); err != nil {
		return nil, err
	}
	return &Tmux{conf: conf, cfg: cfg}, nil
}

func (t *Tmux) run(args ...string) ([]byte, error) {
	a := append([]string{"-L", tmuxSocket, "-f", t.conf}, args...)
	return exec.Command("tmux", a...).CombinedOutput()
}

func TmuxName(provider, id string) string {
	if len(id) > 8 {
		id = id[:8]
	}
	return "ad-" + provider + "-" + id
}

type Live struct {
	Name     string
	Activity int64
	RSSMB    int
	Dead     bool
}

func (t *Tmux) ListLive() map[string]Live {
	out, err := t.run("list-panes", "-a", "-F", "#{session_name}\t#{pane_pid}\t#{session_activity}\t#{pane_dead}")
	live := map[string]Live{}
	if err != nil {
		return live // no server running == nothing live
	}
	rss := processRSS()
	for _, ln := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Split(ln, "\t")
		if len(f) < 4 || !strings.HasPrefix(f[0], "ad-") {
			continue
		}
		pid, _ := strconv.Atoi(f[1])
		act, _ := strconv.ParseInt(f[2], 10, 64)
		live[f[0]] = Live{Name: f[0], Activity: act, RSSMB: rss(pid), Dead: f[3] == "1"}
	}
	return live
}

// processRSS returns a function summing RSS (MB) over a pid's process subtree.
func processRSS() func(pid int) int {
	out, _ := exec.Command("ps", "-axo", "pid=,ppid=,rss=").Output()
	kids := map[int][]int{}
	rss := map[int]int{}
	for _, ln := range strings.Split(string(out), "\n") {
		f := strings.Fields(ln)
		if len(f) != 3 {
			continue
		}
		p, _ := strconv.Atoi(f[0])
		pp, _ := strconv.Atoi(f[1])
		r, _ := strconv.Atoi(f[2])
		kids[pp] = append(kids[pp], p)
		rss[p] = r
	}
	return func(pid int) int {
		total, stack := 0, []int{pid}
		for len(stack) > 0 {
			p := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			total += rss[p]
			stack = append(stack, kids[p]...)
		}
		return total / 1024
	}
}

func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func (t *Tmux) command(provider string, args ...string) (string, error) {
	var base string
	switch provider {
	case "claude":
		base, args = "claude", append(append([]string{}, t.cfg.ClaudeArgs...), args...)
	case "codex":
		base, args = "codex", append(append([]string{}, t.cfg.CodexArgs...), args...)
	default:
		return "", fmt.Errorf("unknown provider %q", provider)
	}
	parts := []string{"exec", base}
	for _, a := range args {
		parts = append(parts, shQuote(a))
	}
	sh := os.Getenv("SHELL")
	if sh == "" {
		sh = "/bin/zsh"
	}
	// login shell so PATH (npm/homebrew installs of claude/codex) matches the user's terminal
	return shQuote(sh) + " -lc " + shQuote(strings.Join(parts, " ")), nil
}

func (t *Tmux) has(name string) bool {
	_, err := t.run("has-session", "-t", "="+name)
	return err == nil
}

func existingDir(d string) string {
	if fi, err := os.Stat(d); err == nil && fi.IsDir() {
		return d
	}
	h, _ := os.UserHomeDir()
	return h
}

// Resume (re)starts the agent for an indexed session inside tmux. Idempotent.
func (t *Tmux) Resume(provider, id, cwd string) (string, error) {
	if !idRe.MatchString(id) {
		return "", errors.New("bad session id")
	}
	name := TmuxName(provider, id)
	if t.has(name) {
		return name, nil
	}
	var cmd string
	var err error
	if provider == "claude" {
		cmd, err = t.command("claude", "--resume", id)
	} else {
		cmd, err = t.command("codex", "resume", id)
	}
	if err != nil {
		return "", err
	}
	return name, t.start(name, cwd, cmd)
}

// New starts a fresh agent. Claude gets a pre-assigned id so it links to its jsonl;
// codex cannot, so it shows up as a live-only row until the index catches it.
func (t *Tmux) New(provider, cwd string) (string, error) {
	var name, cmd string
	var err error
	switch provider {
	case "claude":
		id := newUUID()
		name = TmuxName("claude", id)
		cmd, err = t.command("claude", "--session-id", id)
	case "codex":
		b := make([]byte, 4)
		_, _ = rand.Read(b)
		name = "ad-codex-n" + hex.EncodeToString(b)[:7]
		cmd, err = t.command("codex")
	default:
		err = fmt.Errorf("unknown provider %q", provider)
	}
	if err != nil {
		return "", err
	}
	return name, t.start(name, cwd, cmd)
}

func (t *Tmux) start(name, cwd, cmd string) error {
	out, err := t.run("new-session", "-d", "-s", name, "-c", existingDir(cwd), "-x", "200", "-y", "50", cmd)
	if err != nil {
		return fmt.Errorf("tmux new-session: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (t *Tmux) Kill(name string) error {
	if !strings.HasPrefix(name, "ad-") {
		return errors.New("not an agentdeck session")
	}
	out, err := t.run("kill-session", "-t", "="+name)
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}
