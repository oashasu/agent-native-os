package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Running answers "which live processes are working on this session right now?".
// Two agents writing the same session file fork the conversation (each side only
// ever sees its own half), so resuming something that is already running elsewhere
// must be a conscious decision.
type Running struct {
	ByID map[string][]int // "claude:<id>" / "codex:<id>" -> live pids holding that session
	Ours map[int]bool     // pids inside agentdeck's own tmux server
}

type procInfo struct {
	ppid  int
	start int64 // unix seconds
}

const lstartLayout = "Mon Jan 2 15:04:05 2006"

func normSpaces(s string) string { return strings.Join(strings.Fields(s), " ") }

// psTable reads every process once: pid -> (ppid, start time).
// LC_ALL=C is essential: in a zh-CN locale ps prints "五 10月/ 2 19:47:21 2026", which is
// neither parseable nor comparable with claude's own record.
func psTable() map[int]procInfo {
	cmd := exec.Command("ps", "-axo", "pid=,ppid=,lstart=")
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	out, _ := cmd.Output()
	t := map[int]procInfo{}
	for _, ln := range strings.Split(string(out), "\n") {
		f := strings.Fields(ln)
		if len(f) < 7 {
			continue
		}
		pid, e1 := strconv.Atoi(f[0])
		ppid, e2 := strconv.Atoi(f[1])
		st, e3 := time.ParseInLocation(lstartLayout, strings.Join(f[2:7], " "), time.Local)
		if e1 != nil || e2 != nil || e3 != nil {
			continue
		}
		t[pid] = procInfo{ppid: ppid, start: st.Unix()}
	}
	return t
}

// sameStart: claude records procStart in UTC, ps shows local time. A recycled pid would start
// at a different moment, so compare as instants (2s slack for rounding).
func sameStart(psStart int64, procStart string) bool {
	t, err := time.Parse(lstartLayout, normSpaces(procStart))
	if err != nil {
		return false
	}
	d := psStart - t.Unix()
	return d >= -2 && d <= 2
}

func scanRunning(home string, panePIDs []int) Running {
	r := Running{ByID: map[string][]int{}, Ours: map[int]bool{}}
	ps := psTable()
	kids := map[int][]int{}
	for pid, p := range ps {
		kids[p.ppid] = append(kids[p.ppid], pid)
	}
	for _, root := range panePIDs {
		st := []int{root}
		for len(st) > 0 {
			p := st[len(st)-1]
			st = st[:len(st)-1]
			if r.Ours[p] {
				continue
			}
			r.Ours[p] = true
			st = append(st, kids[p]...)
		}
	}
	// claude: ~/.claude/sessions/<pid>.json. Valid only if that pid is alive AND started at
	// the recorded time (a recycled pid must not look like a running session).
	files, _ := filepath.Glob(filepath.Join(home, ".claude", "sessions", "*.json"))
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var e struct {
			PID       int    `json:"pid"`
			SessionID string `json:"sessionId"`
			ProcStart string `json:"procStart"`
		}
		if json.Unmarshal(b, &e) != nil || e.PID == 0 || e.SessionID == "" {
			continue
		}
		if p, ok := ps[e.PID]; ok && sameStart(p.start, e.ProcStart) {
			k := "claude:" + e.SessionID
			r.ByID[k] = append(r.ByID[k], e.PID)
		}
	}
	// codex: a running thread keeps thread-writer-locks/<id>.lock open.
	dir := filepath.Join(home, ".codex", "thread-writer-locks")
	if _, err := os.Stat(dir); err == nil {
		out, _ := exec.Command("lsof", "-nP", "-F", "pn", "+D", dir).Output()
		pid := 0
		for _, ln := range strings.Split(string(out), "\n") {
			switch {
			case strings.HasPrefix(ln, "p"):
				pid, _ = strconv.Atoi(ln[1:])
			case strings.HasPrefix(ln, "n") && strings.HasSuffix(ln, ".lock") && pid > 0:
				id := strings.TrimSuffix(filepath.Base(ln[1:]), ".lock")
				k := "codex:" + id
				dup := false
				for _, x := range r.ByID[k] {
					dup = dup || x == pid
				}
				if !dup {
					r.ByID[k] = append(r.ByID[k], pid)
				}
			}
		}
	}
	return r
}

// Elsewhere returns the live pids for a session that are NOT hosted by agentdeck's tmux.
func (r Running) Elsewhere(provider, id string) []int {
	var out []int
	for _, p := range r.ByID[provider+":"+id] {
		if !r.Ours[p] {
			out = append(out, p)
		}
	}
	return out
}

type runCache struct {
	mu sync.Mutex
	at time.Time
	r  Running
}

func (c *runCache) get(home string, panes []int) Running {
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Since(c.at) < 2*time.Second {
		return c.r
	}
	c.r, c.at = scanRunning(home, panes), time.Now()
	return c.r
}
