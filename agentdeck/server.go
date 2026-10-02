package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

//go:embed web
var webFS embed.FS

type Server struct {
	ix    *Indexer
	meta  *MetaStore
	tm    *Tmux
	cfg   Config
	token string
	port  int
	run   runCache
	home  string
	ver   string // changes when the embedded UI changes: lets an open window reload itself
}

func loadOrCreateToken(dir string) (string, error) {
	p := filepath.Join(dir, "token")
	if b, err := os.ReadFile(p); err == nil && len(b) >= 32 {
		return strings.TrimSpace(string(b)), nil
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(b)
	return tok, os.WriteFile(p, []byte(tok), 0o600)
}

func (s *Server) hostOK(r *http.Request) bool {
	h := r.Host
	return h == fmt.Sprintf("127.0.0.1:%d", s.port) || h == fmt.Sprintf("localhost:%d", s.port)
}

func (s *Server) tokenOK(v string) bool {
	return subtle.ConstantTimeCompare([]byte(v), []byte(s.token)) == 1
}

// guard: the terminal endpoint is a local shell, so every request must come
// from our own page (Host check defeats DNS rebinding; cookie defeats other
// local pages; X-AD header forces a CORS preflight on POSTs).
func (s *Server) guard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.hostOK(r) {
			http.Error(w, "bad host", http.StatusForbidden)
			return
		}
		if t := r.URL.Query().Get("t"); t != "" && r.URL.Path == "/" {
			if !s.tokenOK(t) {
				http.Error(w, "bad token", http.StatusForbidden)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "ad_token", Value: t, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
		c, err := r.Cookie("ad_token")
		if err != nil || !s.tokenOK(c.Value) {
			http.Error(w, "unauthorized: open agentdeck from the CLI", http.StatusUnauthorized)
			return
		}
		if r.Method == http.MethodPost && r.Header.Get("X-AD") != "1" {
			http.Error(w, "missing header", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

func (s *Server) Handler() http.Handler {
	if s.ver == "" {
		b, _ := webFS.ReadFile("web/index.html")
		s.ver = fmt.Sprintf("%x", sha256.Sum256(b))[:12]
	}
	mux := http.NewServeMux()
	sub, _ := fs.Sub(webFS, "web")
	mux.Handle("/", s.guardH(http.FileServer(http.FS(sub))))
	mux.HandleFunc("/api/sessions", s.guard(s.handleSessions))
	mux.HandleFunc("/api/resume", s.guard(s.handleResume))
	mux.HandleFunc("/api/new", s.guard(s.handleNew))
	mux.HandleFunc("/api/meta", s.guard(s.handleMeta))
	mux.HandleFunc("/api/preview", s.guard(s.handlePreview))
	mux.HandleFunc("/api/sleep", s.guard(s.handleSleep))
	mux.HandleFunc("/api/ide", s.guard(s.handleIDE))
	mux.HandleFunc("/ws/term", s.guard(s.handleTerm))
	return mux
}

func (s *Server) guardH(h http.Handler) http.Handler {
	return http.HandlerFunc(s.guard(h.ServeHTTP))
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

// merged returns indexed sessions with live tmux state, plus live-only rows.
func (s *Server) merged() []Session {
	list := s.ix.Scan()
	live := s.tm.ListLive()
	used := map[string]bool{}
	for i := range list {
		name := TmuxName(list[i].Provider, list[i].ID)
		if l, ok := live[name]; ok && !l.Dead {
			list[i].Live, list[i].Tmux, list[i].RSSMB, list[i].Active = true, name, l.RSSMB, l.Activity
			used[name] = true
		}
	}
	for name, l := range live {
		if used[name] {
			continue
		}
		p := "claude"
		if strings.HasPrefix(name, "ad-codex-") {
			p = "codex"
		}
		title := "(新会话，尚未落盘)"
		if l.Dead {
			title = "(已退出，保留现场)"
		}
		home, _ := os.UserHomeDir()
		list = append(list, Session{Provider: p, ID: name, Title: title, Cwd: home, Live: true, Tmux: name, RSSMB: l.RSSMB, Active: l.Activity, Updated: timeFromUnix(l.Activity)})
	}
	if s.home != "" {
		rn := s.run.get(s.home, s.tm.PanePIDs())
		for i := range list {
			// codex thread locks are held by a shared background process, not by each CLI, so a thread
			// we host ourselves always looks "held by someone else": don't flag those.
			if _, ours := live[TmuxName(list[i].Provider, list[i].ID)]; ours && list[i].Provider == "codex" {
				continue
			}
			list[i].Elsewhere = rn.Elsewhere(list[i].Provider, list[i].ID)
		}
	}
	if s.meta != nil {
		for i := range list {
			list[i].Meta = s.meta.Get(list[i].Provider + ":" + list[i].ID)
		}
	}
	return list
}

func (s *Server) elsewhere(provider, id string) []int {
	if s.home == "" || (provider == "codex" && s.tm.has(TmuxName(provider, id))) {
		return nil // already hosted here: attaching is not a second writer (see merged)
	}
	return s.run.get(s.home, s.tm.PanePIDs()).Elsewhere(provider, id)
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-AD-Ver", s.ver)
	writeJSON(w, 200, s.merged())
}

type req struct {
	Provider string `json:"provider"`
	ID       string `json:"id"`
	Cwd      string `json:"cwd"`
	Tmux     string `json:"tmux"`
	Force    bool   `json:"force"`
	Meta
}

func decode(r *http.Request) (req, error) {
	var q req
	err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<16)).Decode(&q)
	return q, err
}

func (s *Server) lookup(provider, id string) (Session, bool) {
	for _, x := range s.ix.Scan() {
		if x.Provider == provider && x.ID == id {
			return x, true
		}
	}
	return Session{}, false
}

func (s *Server) handleResume(w http.ResponseWriter, r *http.Request) {
	q, err := decode(r)
	if err != nil {
		fail(w, 400, err)
		return
	}
	sess, ok := s.lookup(q.Provider, q.ID)
	if !ok {
		fail(w, 404, fmt.Errorf("session not found"))
		return
	}
	sess.Elsewhere = s.elsewhere(sess.Provider, sess.ID) // lookup() only knows the index, not who is running what
	if len(sess.Elsewhere) > 0 && !q.Force {
		writeJSON(w, 409, map[string]interface{}{
			"error": fmt.Sprintf("该会话正在别处运行（进程 %v）。继续恢复会让对话分叉：两边互相看不到对方的内容。", sess.Elsewhere),
			"code":  "running_elsewhere", "pids": sess.Elsewhere,
		})
		return
	}
	log.Printf("resume %s %s force=%v elsewhere=%v (from %s)", sess.Provider, sess.ID, q.Force, sess.Elsewhere, r.Referer())
	name, err := s.tm.Resume(sess.Provider, sess.ID, sess.Cwd)
	if err != nil {
		fail(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]string{"tmux": name})
}

func (s *Server) handleNew(w http.ResponseWriter, r *http.Request) {
	q, err := decode(r)
	if err != nil {
		fail(w, 400, err)
		return
	}
	log.Printf("new %s in %s", q.Provider, q.Cwd)
	name, err := s.tm.New(q.Provider, q.Cwd)
	if err != nil {
		fail(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]string{"tmux": name})
}

func (s *Server) handleSleep(w http.ResponseWriter, r *http.Request) {
	q, err := decode(r)
	if err != nil {
		fail(w, 400, err)
		return
	}
	log.Printf("sleep %s", q.Tmux)
	if err := s.tm.Kill(q.Tmux); err != nil {
		fail(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]string{"ok": "1"})
}

func (s *Server) handleIDE(w http.ResponseWriter, r *http.Request) {
	q, err := decode(r)
	if err != nil {
		fail(w, 400, err)
		return
	}
	cwd := existingDir(q.Cwd)
	tpl := s.cfg.IDECmd
	if tpl == "" {
		for _, c := range []string{"code", "cursor"} {
			if _, err := exec.LookPath(c); err == nil {
				tpl = c + " {cwd}"
				break
			}
		}
	}
	if tpl == "" {
		fail(w, 400, fmt.Errorf("未配置 IDE：在 ~/.config/agentdeck/config.json 设置 ide_cmd，例如 \"code {cwd}\""))
		return
	}
	// {cwd} is passed as its own argv element: no shell, no injection.
	var argv []string
	for _, f := range strings.Fields(tpl) {
		argv = append(argv, strings.ReplaceAll(f, "{cwd}", cwd))
	}
	if err := exec.Command(argv[0], argv[1:]...).Start(); err != nil {
		fail(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]string{"ok": "1"})
}

var upgrader = websocket.Upgrader{
	// same-origin only: Origin must be our own host
	CheckOrigin: func(r *http.Request) bool {
		o := r.Header.Get("Origin")
		return o == "http://"+r.Host
	},
}

// handleTerm bridges a websocket to `tmux attach` in a pty. Closing the socket
// only detaches the client; the agent keeps running inside tmux.
func (s *Server) handleTerm(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if !strings.HasPrefix(name, "ad-") || !s.tm.has(name) {
		http.Error(w, "no such session", http.StatusNotFound)
		return
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	cmd := exec.Command("tmux", "-L", tmuxSocket, "-f", s.tm.conf, "attach", "-t", "="+name)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 40, Cols: 120})
	if err != nil {
		_ = conn.WriteMessage(websocket.TextMessage, []byte("attach failed: "+err.Error()))
		return
	}
	var once sync.Once
	stop := func() { once.Do(func() { _ = ptmx.Close(); _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() }) }
	defer stop()
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				if conn.WriteMessage(websocket.BinaryMessage, buf[:n]) != nil {
					break
				}
			}
			if err != nil {
				break
			}
		}
		_ = conn.Close()
	}()
	for {
		mt, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if mt == websocket.TextMessage { // control: {"cols":N,"rows":N}
			var c struct{ Cols, Rows uint16 }
			if json.Unmarshal(data, &c) == nil && c.Cols > 0 && c.Rows > 0 {
				_ = pty.Setsize(ptmx, &pty.Winsize{Rows: c.Rows, Cols: c.Cols})
			}
			continue
		}
		if _, err := ptmx.Write(data); err != nil {
			return
		}
	}
}

func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	q, err := decode(r)
	if err != nil {
		fail(w, 400, err)
		return
	}
	if q.Provider != "claude" && q.Provider != "codex" || q.ID == "" {
		fail(w, 400, fmt.Errorf("bad session"))
		return
	}
	if err := s.meta.Set(q.Provider+":"+q.ID, q.Meta); err != nil {
		fail(w, 400, err)
		return
	}
	writeJSON(w, 200, s.meta.Get(q.Provider+":"+q.ID))
}

func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	p, id := r.URL.Query().Get("provider"), r.URL.Query().Get("id")
	sess, ok := s.lookup(p, id)
	if !ok {
		writeJSON(w, 200, []PreviewMsg{})
		return
	}
	msgs := Preview(sess.Provider, sess.Path, 8)
	if msgs == nil {
		msgs = []PreviewMsg{}
	}
	writeJSON(w, 200, msgs)
}
