package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Session is one resumable agent conversation found on disk. The conversation
// itself lives in the provider's own jsonl; agentdeck only indexes it.
type Session struct {
	Provider  string    `json:"provider"` // claude | codex
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Cwd       string    `json:"cwd"`
	Updated   time.Time `json:"updated"`
	Path      string    `json:"path"`
	Live      bool      `json:"live"`
	Tmux      string    `json:"tmux,omitempty"`
	RSSMB     int       `json:"rss_mb,omitempty"`
	Active    int64     `json:"active,omitempty"`    // unix seconds of last tmux activity
	Elsewhere []int     `json:"elsewhere,omitempty"` // live processes outside agentdeck that hold this session
	Meta
}

type cacheKey struct {
	path  string
	mtime int64
	size  int64
}

type Indexer struct {
	home  string
	mu    sync.Mutex
	cache map[cacheKey]*Session
}

func NewIndexer(home string) *Indexer {
	return &Indexer{home: home, cache: map[cacheKey]*Session{}}
}

func (ix *Indexer) Scan() []Session {
	var out []Session
	out = append(out, ix.scanClaude()...)
	out = append(out, ix.scanCodex()...)
	sort.Slice(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	return out
}

func (ix *Indexer) cached(path string, fi os.FileInfo, parse func() *Session) *Session {
	k := cacheKey{path, fi.ModTime().UnixNano(), fi.Size()}
	ix.mu.Lock()
	s, ok := ix.cache[k]
	ix.mu.Unlock()
	if ok {
		return s
	}
	s = parse()
	if s != nil {
		s.Updated = fi.ModTime()
		s.Path = path
	}
	ix.mu.Lock()
	ix.cache[k] = s
	ix.mu.Unlock()
	return s
}

func (ix *Indexer) scanClaude() []Session {
	files, _ := filepath.Glob(filepath.Join(ix.home, ".claude", "projects", "*", "*.jsonl"))
	var out []Session
	for _, f := range files {
		fi, err := os.Stat(f)
		if err != nil {
			continue
		}
		s := ix.cached(f, fi, func() *Session { return parseClaude(f) })
		if s != nil {
			out = append(out, *s)
		}
	}
	return out
}

func readLines(r io.Reader, fn func(line []byte) bool) {
	br := bufio.NewReaderSize(r, 1<<16)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 && !fn(line) {
			return
		}
		if err != nil {
			return
		}
	}
}

func parseClaude(path string) *Session {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	s := &Session{Provider: "claude", ID: strings.TrimSuffix(filepath.Base(path), ".jsonl")}
	var aiTitle, firstUser string
	readLines(f, func(line []byte) bool {
		if s.Cwd == "" && bytes.Contains(line, []byte(`"cwd":"`)) {
			var m struct {
				Cwd string `json:"cwd"`
			}
			if json.Unmarshal(line, &m) == nil {
				s.Cwd = m.Cwd
			}
		}
		if bytes.HasPrefix(line, []byte(`{"type":"ai-title"`)) {
			var m struct {
				T string `json:"aiTitle"`
			}
			if json.Unmarshal(line, &m) == nil && m.T != "" {
				aiTitle = m.T
			}
		} else if firstUser == "" && bytes.Contains(line, []byte(`"type":"user"`)) && bytes.Contains(line, []byte(`"role":"user"`)) {
			firstUser = claudeUserText(line)
		}
		return true
	})
	s.Title = firstNonEmpty(aiTitle, firstUser)
	if s.Cwd == "" && s.Title == "" {
		return nil
	}
	if s.Title == "" {
		s.Title = filepath.Base(s.Cwd)
	}
	return s
}

func claudeUserText(line []byte) string {
	var m struct {
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &m) != nil {
		return ""
	}
	var str string
	if json.Unmarshal(m.Message.Content, &str) == nil {
		return cleanTitle(str)
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(m.Message.Content, &parts) == nil {
		for _, p := range parts {
			if p.Type == "text" {
				if t := cleanTitle(p.Text); t != "" {
					return t
				}
			}
		}
	}
	return ""
}

// cleanTitle drops injected pseudo-prompts (<system-reminder>, <command-...>) and clips.
func cleanTitle(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || strings.HasPrefix(s, "<") {
		return ""
	}
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > 80 {
		r = append(r[:80], '…')
	}
	return string(r)
}

func firstNonEmpty(a ...string) string {
	for _, s := range a {
		if s != "" {
			return s
		}
	}
	return ""
}

func (ix *Indexer) codexNames() map[string]string {
	names := map[string]string{}
	f, err := os.Open(filepath.Join(ix.home, ".codex", "session_index.jsonl"))
	if err != nil {
		return names
	}
	defer f.Close()
	readLines(f, func(line []byte) bool {
		var raw struct {
			ID         string `json:"id"`
			ThreadName string `json:"thread_name"`
		}
		if json.Unmarshal(line, &raw) == nil && raw.ID != "" {
			names[raw.ID] = raw.ThreadName
		}
		return true
	})
	return names
}

func (ix *Indexer) scanCodex() []Session {
	names := ix.codexNames()
	var out []Session
	root := filepath.Join(ix.home, ".codex", "sessions")
	_ = filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() || !strings.HasSuffix(p, ".jsonl") {
			return nil
		}
		s := ix.cached(p, fi, func() *Session { return parseCodex(p) })
		if s == nil {
			return nil
		}
		c := *s
		if n := names[c.ID]; n != "" {
			c.Title = n
		}
		out = append(out, c)
		return nil
	})
	return out
}

// parseCodex reads only the first line (session_meta). Subagent threads are skipped.
func parseCodex(path string) *Session {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	line, err := bufio.NewReaderSize(f, 1<<16).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return nil
	}
	var m struct {
		Type    string `json:"type"`
		Payload struct {
			ID           string `json:"id"`
			Cwd          string `json:"cwd"`
			ThreadSource string `json:"thread_source"`
		} `json:"payload"`
	}
	if json.Unmarshal(line, &m) != nil || m.Type != "session_meta" || m.Payload.ID == "" {
		return nil
	}
	if m.Payload.ThreadSource == "subagent" {
		return nil
	}
	return &Session{Provider: "codex", ID: m.Payload.ID, Cwd: m.Payload.Cwd, Title: filepath.Base(m.Payload.Cwd)}
}

func timeFromUnix(u int64) time.Time { return time.Unix(u, 0) }
