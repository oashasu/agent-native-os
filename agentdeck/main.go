// agentdeck: standalone manager for claude/codex agent sessions.
//
//	agentdeck            start (if needed) and open the window
//	agentdeck serve      run the server in the foreground
//	agentdeck list       print sessions (live ones first)
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const defaultPort = 47017

func configDir() string {
	h, _ := os.UserHomeDir()
	d := filepath.Join(h, ".config", "agentdeck")
	if v := os.Getenv("AGENTDECK_CONFIG"); v != "" { // isolated config (tests / trying things without touching real marks)
		d = v
	}
	_ = os.MkdirAll(d, 0o700)
	return d
}

func loadConfig(dir string) Config {
	var c Config
	if b, err := os.ReadFile(filepath.Join(dir, "config.json")); err == nil {
		if err := json.Unmarshal(b, &c); err != nil {
			fmt.Fprintln(os.Stderr, "config.json ignored:", err)
		}
	}
	return c
}

// aliasArgs reads the user's own shell alias for an agent (e.g. claude='claude --dangerously-skip-permissions')
// so sessions started here behave exactly like ones started in their terminal.
// Only "<name> <flags...>" aliases are honoured; anything fancier is ignored.
func aliasArgs(name string) []string {
	sh := os.Getenv("SHELL")
	if sh == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, sh, "-ic", "alias "+name).Output()
	if err != nil {
		return nil
	}
	for _, ln := range strings.Split(string(out), "\n") {
		ln = strings.TrimSpace(ln)
		if !strings.HasPrefix(ln, name+"=") {
			continue
		}
		v := strings.Trim(strings.TrimPrefix(ln, name+"="), `'"`)
		if strings.ContainsAny(v, ";&|$`()<>\\") {
			return nil
		}
		f := strings.Fields(v)
		if len(f) > 1 && filepath.Base(f[0]) == name {
			return f[1:]
		}
	}
	return nil
}

func newServer(port int) (*Server, error) {
	dir := configDir()
	cfg := loadConfig(dir)
	if cfg.ClaudeArgs == nil {
		cfg.ClaudeArgs = aliasArgs("claude")
	}
	if cfg.CodexArgs == nil {
		cfg.CodexArgs = aliasArgs("codex")
	}
	tm, err := NewTmux(dir, cfg)
	if err != nil {
		return nil, err
	}
	tok, err := loadOrCreateToken(dir)
	if err != nil {
		return nil, err
	}
	h, _ := os.UserHomeDir()
	return &Server{home: h, ix: NewIndexer(h), meta: NewMetaStore(dir), tm: tm, cfg: cfg, token: tok, port: port}, nil
}

func portOpen(port int) bool {
	c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 300*time.Millisecond)
	if err == nil {
		c.Close()
	}
	return err == nil
}

func openWindow(s *Server, noOpen bool) {
	url := fmt.Sprintf("http://127.0.0.1:%d/?t=%s", s.port, s.token)
	if noOpen {
		fmt.Println(url)
		return
	}
	// dedicated profile: own window/size, never touches the user's browsing profile
	prof := filepath.Join(configDir(), "chrome")
	err := exec.Command("open", "-na", "Google Chrome", "--args", "--app="+url, "--user-data-dir="+prof, "--window-size=1440,900", "--no-first-run").Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "could not open Chrome app window:", err)
		fmt.Println(url)
	}
}

func main() {
	port := flag.Int("port", defaultPort, "listen port (127.0.0.1 only)")
	noOpen := flag.Bool("no-open", false, "print the URL instead of opening a window")
	flag.Parse()
	cmd := "open"
	if flag.NArg() > 0 {
		cmd = flag.Arg(0)
	}
	s, err := newServer(*port)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	switch cmd {
	case "list":
		for _, x := range s.merged() {
			mark := "  "
			if x.Live {
				mark = "● "
			}
			warn := ""
			if len(x.Elsewhere) > 0 {
				warn = fmt.Sprintf("  ⚠ 别处也在运行 pid=%v", x.Elsewhere)
			}
			fmt.Printf("%s%-6s %s  %-8dMB %s  %s%s\n", mark, x.Provider, x.ID[:8], x.RSSMB, x.Cwd, x.Title, warn)
		}
	case "open":
		if !portOpen(*port) {
			self, _ := os.Executable()
			c := exec.Command(self, "-port", fmt.Sprint(*port), "serve")
			// own session: closing the launching terminal must not kill the server (SIGHUP)
			c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
			if lf, err := os.OpenFile(filepath.Join(configDir(), "server.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
				c.Stdout, c.Stderr = lf, lf
			}
			if err := c.Start(); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			for i := 0; i < 30 && !portOpen(*port); i++ {
				time.Sleep(100 * time.Millisecond)
			}
		}
		openWindow(s, *noOpen)
	case "serve":
		addr := fmt.Sprintf("127.0.0.1:%d", *port)
		fmt.Println("agentdeck listening on", addr)
		if err := http.ListenAndServe(addr, s.Handler()); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	default:
		fmt.Fprintln(os.Stderr, "usage: agentdeck [open|serve|list]")
		os.Exit(2)
	}
}
