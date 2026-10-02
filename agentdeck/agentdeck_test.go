package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestIndexer(t *testing.T) {
	home := t.TempDir()
	// claude: injected pseudo-prompt must not become the title; ai-title (last) wins
	write(t, filepath.Join(home, ".claude/projects/-x/aaaaaaaa-1111-2222-3333-444444444444.jsonl"),
		`{"type":"user","cwd":"/work/a","message":{"role":"user","content":"<system-reminder>x</system-reminder>"}}`+"\n"+
			`{"type":"user","message":{"role":"user","content":"帮我看下支付"}}`+"\n"+
			`{"type":"ai-title","aiTitle":"旧标题","sessionId":"a"}`+"\n"+
			`{"type":"ai-title","aiTitle":"支付排查","sessionId":"a"}`+"\n")
	// claude without ai-title: falls back to first real user text (array content form)
	write(t, filepath.Join(home, ".claude/projects/-x/bbbbbbbb-1111-2222-3333-444444444444.jsonl"),
		`{"type":"user","cwd":"/work/b","message":{"role":"user","content":[{"type":"text","text":"  hello   world "}]}}`+"\n")
	// codex user thread + subagent thread (must be skipped) + index name
	write(t, filepath.Join(home, ".codex/sessions/2026/01/01/rollout-u.jsonl"),
		`{"type":"session_meta","payload":{"id":"cccccccc-1111","cwd":"/work/c","thread_source":"user"}}`+"\n")
	write(t, filepath.Join(home, ".codex/sessions/2026/01/01/rollout-s.jsonl"),
		`{"type":"session_meta","payload":{"id":"dddddddd-1111","cwd":"/work/d","thread_source":"subagent"}}`+"\n")
	write(t, filepath.Join(home, ".codex/session_index.jsonl"),
		`{"id":"cccccccc-1111","thread_name":"订单折扣"}`+"\n")

	got := map[string]Session{}
	for _, s := range NewIndexer(home).Scan() {
		got[s.ID] = s
	}
	if len(got) != 3 {
		t.Fatalf("want 3 sessions (subagent excluded), got %d: %+v", len(got), got)
	}
	check := func(id, title, cwd string) {
		s, ok := got[id]
		if !ok || s.Title != title || s.Cwd != cwd {
			t.Errorf("%s: got %+v, want title=%q cwd=%q", id, s, title, cwd)
		}
	}
	check("aaaaaaaa-1111-2222-3333-444444444444", "支付排查", "/work/a")
	check("bbbbbbbb-1111-2222-3333-444444444444", "hello world", "/work/b")
	check("cccccccc-1111", "订单折扣", "/work/c")
	if _, bad := got["dddddddd-1111"]; bad {
		t.Error("subagent thread leaked into index")
	}
}

func testServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	// never touch the user's real tmux server: every test gets its own socket
	prev := tmuxSocket
	tmuxSocket = fmt.Sprintf("adtest-%d-%d", os.Getpid(), time.Now().UnixNano()%1e6)
	t.Cleanup(func() {
		_ = exec.Command("tmux", "-L", tmuxSocket, "kill-server").Run()
		// the server exits (and leaves its socket file) when the last session dies, so don't ask tmux for the path
		dir := os.Getenv("TMUX_TMPDIR")
		if dir == "" {
			dir = "/tmp"
		}
		_ = os.Remove(filepath.Join(dir, fmt.Sprintf("tmux-%d", os.Getuid()), tmuxSocket))
		tmuxSocket = prev
	})
	dir := t.TempDir()
	tm, err := NewTmux(dir, Config{})
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{ix: NewIndexer(t.TempDir()), meta: NewMetaStore(dir), tm: tm, token: "tok0123456789abcdef0123456789abcdef", port: 0}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	_, _ = fmt.Sscanf(ts.URL[strings.LastIndex(ts.URL, ":")+1:], "%d", &s.port)
	return s, ts
}

func dial(t *testing.T, s *Server, ts *httptest.Server, name, origin string) (*websocket.Conn, *http.Response, error) {
	h := http.Header{"Cookie": {"ad_token=" + s.token}}
	if origin != "" {
		h.Set("Origin", origin)
	}
	return websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/ws/term?name="+name, h)
}

func TestTerminalBridgeAndDetachKeepsSession(t *testing.T) {
	if _, err := os.Stat("/opt/homebrew/bin/tmux"); err != nil {
		if _, err2 := os.Stat("/usr/bin/tmux"); err2 != nil {
			t.Skip("tmux not installed")
		}
	}
	s, ts := testServer(t)
	name := fmt.Sprintf("ad-test-%d", time.Now().UnixNano()%1e9)
	if err := s.tm.start(name, t.TempDir(), "sh -c 'echo READY; exec cat'"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.tm.Kill(name) })

	// foreign origin is refused, same-origin accepted
	if c, _, err := dial(t, s, ts, name, "http://evil.example"); err == nil {
		c.Close()
		t.Fatal("cross-origin websocket accepted")
	}
	c, _, err := dial(t, s, ts, name, ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	read := func(want string) {
		t.Helper()
		_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
		var acc strings.Builder
		for !strings.Contains(acc.String(), want) {
			_, b, err := c.ReadMessage()
			if err != nil {
				t.Fatalf("waiting for %q, got %q: %v", want, acc.String(), err)
			}
			acc.Write(b)
		}
	}
	read("READY")
	if err := c.WriteMessage(websocket.BinaryMessage, []byte("ping-123\n")); err != nil {
		t.Fatal(err)
	}
	read("ping-123")

	// closing the UI connection must NOT stop the agent
	c.Close()
	time.Sleep(500 * time.Millisecond)
	if !s.tm.has(name) {
		t.Fatal("session died when websocket closed")
	}
	if live, ok := s.tm.ListLive()[name]; !ok || live.Dead {
		t.Fatalf("session not listed live: %+v", live)
	}
	// sleep really ends it
	if err := s.tm.Kill(name); err != nil || s.tm.has(name) {
		t.Fatalf("kill failed: %v", err)
	}
}

func TestGuard(t *testing.T) {
	s, ts := testServer(t)
	get := func(path, cookie, host string) int {
		req, _ := http.NewRequest("GET", ts.URL+path, nil)
		if cookie != "" {
			req.Header.Set("Cookie", "ad_token="+cookie)
		}
		if host != "" {
			req.Host = host
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if c := get("/api/sessions", "", ""); c != 401 {
		t.Errorf("no cookie: %d", c)
	}
	if c := get("/api/sessions", "wrong", ""); c != 401 {
		t.Errorf("wrong cookie: %d", c)
	}
	if c := get("/api/sessions", s.token, "evil.com"); c != 403 {
		t.Errorf("rebinding host: %d", c)
	}
	if c := get("/api/sessions", s.token, ""); c != 200 {
		t.Errorf("valid: %d", c)
	}
	if c := get("/ws/term?name=ad-nope", s.token, ""); c != 404 {
		t.Errorf("unknown session ws: %d", c)
	}
}

func TestResumeRejectsBadID(t *testing.T) {
	s, _ := testServer(t)
	if _, err := s.tm.Resume("claude", "x; rm -rf ~", "/tmp"); err == nil {
		t.Fatal("injection-shaped id accepted")
	}
}

func TestAliasArgsParsing(t *testing.T) {
	// shell with a fake alias output: use a tiny script as $SHELL
	dir := t.TempDir()
	sh := filepath.Join(dir, "fakesh")
	write(t, sh, "#!/bin/sh\ncase \"$2\" in\n *claude) echo \"claude='claude --dangerously-skip-permissions'\";;\n *codex) echo \"codex='FOO=1 codex --yolo'\";;\n *evil) echo \"evil='evil; rm -rf ~'\";;\nesac\n")
	_ = os.Chmod(sh, 0o755)
	t.Setenv("SHELL", sh)
	if got := aliasArgs("claude"); len(got) != 1 || got[0] != "--dangerously-skip-permissions" {
		t.Errorf("claude: %v", got)
	}
	if got := aliasArgs("codex"); got != nil { // env-prefixed alias is not a plain flag alias
		t.Errorf("codex should be ignored, got %v", got)
	}
	if got := aliasArgs("evil"); got != nil {
		t.Errorf("metachar alias accepted: %v", got)
	}
}

func TestMetaStoreRoundTripAndValidation(t *testing.T) {
	dir := t.TempDir()
	m := NewMetaStore(dir)
	if err := m.Set("claude:x", Meta{Status: "bogus"}); err == nil {
		t.Fatal("invalid status accepted")
	}
	if err := m.Set("claude:x", Meta{Status: "wait", Tags: []string{" 支付 ", "支付", "", "发版"}, Pinned: true, Alias: " 我的会话 "}); err != nil {
		t.Fatal(err)
	}
	got := NewMetaStore(dir).Get("claude:x") // reload from disk: must persist
	if got.Status != "wait" || !got.Pinned || got.Alias != "我的会话" || len(got.Tags) != 2 {
		t.Fatalf("not persisted/normalised: %+v", got)
	}
	_ = m.Set("claude:x", Meta{}) // clearing everything removes the entry
	if g := NewMetaStore(dir).Get("claude:x"); g.Status != "" || g.Pinned || len(g.Tags) != 0 {
		t.Fatalf("not cleared: %+v", g)
	}
}

func TestPreviewBothProviders(t *testing.T) {
	dir := t.TempDir()
	c := filepath.Join(dir, "c.jsonl")
	write(t, c, `{"type":"user","message":{"role":"user","content":"<system-reminder>skip</system-reminder>"}}`+"\n"+
		`{"type":"user","message":{"role":"user","content":"问题一"}}`+"\n"+
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"回答一"},{"type":"tool_use","id":"x"}]}}`+"\n")
	x := filepath.Join(dir, "x.jsonl")
	write(t, x, `{"type":"session_meta","payload":{"id":"1"}}`+"\n"+
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"codex问"}]}}`+"\n"+
		`{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"codex答"}]}}`+"\n")
	pc, px := Preview("claude", c, 8), Preview("codex", x, 8)
	if len(pc) != 2 || pc[0].Text != "问题一" || pc[1].Role != "assistant" || pc[1].Text != "回答一" {
		t.Errorf("claude preview: %+v", pc)
	}
	if len(px) != 2 || px[0].Text != "codex问" || px[1].Text != "codex答" {
		t.Errorf("codex preview: %+v", px)
	}
}

func TestSessionsEndpointCarriesUIVersion(t *testing.T) {
	s, ts := testServer(t)
	get := func() string {
		req, _ := http.NewRequest("GET", ts.URL+"/api/sessions", nil)
		req.Header.Set("Cookie", "ad_token="+s.token)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.Header.Get("X-AD-Ver")
	}
	a, b := get(), get()
	if a == "" || a != b {
		t.Fatalf("UI version missing or unstable: %q vs %q", a, b)
	}
}

// psLstart returns pid's start time the way claude records it: English format, in UTC.
func psLstart(t *testing.T, pid int) string {
	t.Helper()
	ps := psTable()
	p, ok := ps[pid]
	if !ok {
		t.Fatalf("pid %d not in ps table", pid)
	}
	return time.Unix(p.start, 0).UTC().Format(lstartLayout)
}

func TestScanRunningClaudeRegistry(t *testing.T) {
	home := t.TempDir()
	me := os.Getpid()
	reg := func(pid int, sid, start string) {
		write(t, filepath.Join(home, ".claude/sessions", strconv.Itoa(pid)+".json"),
			fmt.Sprintf(`{"pid":%d,"sessionId":%q,"procStart":%q}`, pid, sid, start))
	}
	reg(me, "alive-session", psLstart(t, me))                  // genuinely running
	reg(me+1000000, "dead-session", "Mon Jan 1 00:00:00 2001") // no such pid
	reg(1, "recycled-pid", "Mon Jan  1 00:00:00 2001")         // pid exists but started at another time
	if _, ok := psTable()[1]; !ok {
		t.Fatal("ps table does not contain pid 1: the recycled-pid case would prove nothing")
	}
	r := scanRunning(home, nil)
	if got := r.Elsewhere("claude", "alive-session"); len(got) != 1 || got[0] != me {
		t.Errorf("live session not detected: %v", got)
	}
	if got := r.Elsewhere("claude", "dead-session"); len(got) != 0 {
		t.Errorf("dead pid counted as running: %v", got)
	}
	if got := r.Elsewhere("claude", "recycled-pid"); len(got) != 0 {
		t.Errorf("recycled pid counted as running: %v", got)
	}
	// a process hosted by agentdeck's own tmux is not "elsewhere"
	if got := scanRunning(home, []int{me}).Elsewhere("claude", "alive-session"); len(got) != 0 {
		t.Errorf("our own process reported as elsewhere: %v", got)
	}
}

func TestScanRunningCodexLocks(t *testing.T) {
	home := t.TempDir()
	lock := filepath.Join(home, ".codex/thread-writer-locks/held-thread.lock")
	write(t, lock, "")
	write(t, filepath.Join(home, ".codex/thread-writer-locks/idle-thread.lock"), "")
	f, err := os.Open(lock) // a running codex keeps its thread lock open
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := exec.LookPath("lsof"); err != nil {
		t.Skip("lsof not available")
	}
	r := scanRunning(home, nil)
	if got := r.Elsewhere("codex", "held-thread"); len(got) != 1 || got[0] != os.Getpid() {
		t.Errorf("held lock not detected: %v", got)
	}
	if got := r.Elsewhere("codex", "idle-thread"); len(got) != 0 {
		t.Errorf("unheld lock reported as running: %v", got)
	}
}

func TestResumeRefusedWhenRunningElsewhere(t *testing.T) {
	s, ts := testServer(t)
	home := t.TempDir()
	s.home, s.ix = home, NewIndexer(home)
	sid := "aaaaaaaa-1111-2222-3333-444444444444"
	write(t, filepath.Join(home, ".claude/projects/-x/"+sid+".jsonl"), `{"type":"user","cwd":"/tmp","message":{"role":"user","content":"hi"}}`+"\n")
	me := os.Getpid()
	write(t, filepath.Join(home, ".claude/sessions/x.json"), fmt.Sprintf(`{"pid":%d,"sessionId":%q,"procStart":%q}`, me, sid, psLstart(t, me)))
	post := func(body string) (int, string) {
		req, _ := http.NewRequest("POST", ts.URL+"/api/resume", strings.NewReader(body))
		req.Header.Set("Cookie", "ad_token="+s.token)
		req.Header.Set("X-AD", "1")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b := make([]byte, 400)
		n, _ := res.Body.Read(b)
		return res.StatusCode, string(b[:n])
	}
	code, body := post(fmt.Sprintf(`{"provider":"claude","id":%q}`, sid))
	if code != 409 || !strings.Contains(body, "running_elsewhere") {
		t.Fatalf("resume of a session running elsewhere was not refused: %d %s", code, body)
	}
	if s.tm.has(TmuxName("claude", sid)) {
		t.Fatal("a process was started despite the refusal")
	}
	// an explicit, informed override must go through (this is a warning, not a lock)
	if code, body := post(fmt.Sprintf(`{"provider":"claude","id":%q,"force":true}`, sid)); code != 200 {
		t.Fatalf("force=true was refused: %d %s", code, body)
	}
	if !s.tm.has(TmuxName("claude", sid)) {
		t.Fatal("force=true did not start the session")
	}
}

func TestCodexHostedHereIsNotFlaggedButDormantHeldIsRefused(t *testing.T) {
	if _, err := exec.LookPath("lsof"); err != nil {
		t.Skip("lsof not available")
	}
	s, ts := testServer(t)
	home := t.TempDir()
	s.home, s.ix = home, NewIndexer(home)
	hosted, dormant := "11111111-aaaa-bbbb-cccc-000000000001", "22222222-aaaa-bbbb-cccc-000000000002"
	for _, id := range []string{hosted, dormant} {
		write(t, filepath.Join(home, ".codex/sessions/2026/01/01/rollout-"+id+".jsonl"),
			fmt.Sprintf(`{"type":"session_meta","payload":{"id":%q,"cwd":"/tmp","thread_source":"user"}}`+"\n", id))
		lock := filepath.Join(home, ".codex/thread-writer-locks/"+id+".lock")
		write(t, lock, "")
		f, err := os.Open(lock) // someone (the codex daemon) holds both threads
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
	}
	if err := s.tm.start(TmuxName("codex", hosted), t.TempDir(), "sleep 60"); err != nil {
		t.Fatal(err)
	}
	post := func(id string) int {
		req, _ := http.NewRequest("POST", ts.URL+"/api/resume", strings.NewReader(fmt.Sprintf(`{"provider":"codex","id":%q}`, id)))
		req.Header.Set("Cookie", "ad_token="+s.token)
		req.Header.Set("X-AD", "1")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if c := post(hosted); c != 200 {
		t.Errorf("attaching to a codex thread we host ourselves was blocked: %d", c)
	}
	if c := post(dormant); c != 409 {
		t.Errorf("dormant codex thread held by another process was not refused: %d", c)
	}
	for _, x := range s.merged() {
		if x.ID == hosted && len(x.Elsewhere) != 0 {
			t.Errorf("hosted codex thread wrongly flagged: %v", x.Elsewhere)
		}
		if x.ID == dormant && len(x.Elsewhere) == 0 {
			t.Errorf("dormant held codex thread not flagged")
		}
	}
}
