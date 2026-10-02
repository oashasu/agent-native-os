package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Meta is the user's own annotation of a session. It lives in agentdeck's
// config dir, never in the agents' jsonl files.
type Meta struct {
	Status string   `json:"status,omitempty"` // "" | doing | wait | block | done
	Tags   []string `json:"tags,omitempty"`
	Pinned bool     `json:"pinned,omitempty"`
	Alias  string   `json:"alias,omitempty"`
}

var validStatus = map[string]bool{"": true, "doing": true, "wait": true, "block": true, "done": true}

type MetaStore struct {
	mu   sync.Mutex
	path string
	m    map[string]Meta
}

func NewMetaStore(dir string) *MetaStore {
	s := &MetaStore{path: filepath.Join(dir, "meta.json"), m: map[string]Meta{}}
	if b, err := os.ReadFile(s.path); err == nil {
		_ = json.Unmarshal(b, &s.m)
	}
	return s
}

func (s *MetaStore) Get(key string) Meta {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m[key]
}

func cleanTags(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range in {
		t = strings.TrimSpace(t)
		if r := []rune(t); len(r) > 24 {
			t = string(r[:24])
		}
		if t != "" && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	sort.Strings(out)
	return out
}

func (s *MetaStore) Set(key string, m Meta) error {
	if !validStatus[m.Status] {
		return errors.New("bad status")
	}
	m.Tags = cleanTags(m.Tags)
	if r := []rune(strings.TrimSpace(m.Alias)); len(r) > 80 {
		m.Alias = string(r[:80])
	} else {
		m.Alias = strings.TrimSpace(m.Alias)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if m.Status == "" && len(m.Tags) == 0 && !m.Pinned && m.Alias == "" {
		delete(s.m, key)
	} else {
		s.m[key] = m
	}
	b, err := json.MarshalIndent(s.m, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// ---- preview: last few messages of a session without starting any process ----

type PreviewMsg struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

func readTail(path string, n int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	off := fi.Size() - n
	if off < 0 {
		off = 0
	}
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return nil, err
	}
	b, err := io.ReadAll(f)
	if off > 0 { // first line is probably cut in half
		if i := bytes.IndexByte(b, '\n'); i >= 0 {
			b = b[i+1:]
		}
	}
	return b, err
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func msgText(content json.RawMessage) string {
	var str string
	if json.Unmarshal(content, &str) == nil {
		return str
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(content, &parts) == nil {
		var sb strings.Builder
		for _, p := range parts {
			if p.Type == "text" || p.Type == "input_text" || p.Type == "output_text" {
				sb.WriteString(p.Text)
			}
		}
		return sb.String()
	}
	return ""
}

func Preview(provider, path string, max int) []PreviewMsg {
	b, err := readTail(path, 1<<20)
	if err != nil {
		return nil
	}
	var all []PreviewMsg
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 1<<20), 8<<20)
	for sc.Scan() {
		line := sc.Bytes()
		var role, text string
		if provider == "claude" {
			var m struct {
				Type    string `json:"type"`
				Message struct {
					Role    string          `json:"role"`
					Content json.RawMessage `json:"content"`
				} `json:"message"`
			}
			if json.Unmarshal(line, &m) != nil || (m.Type != "user" && m.Type != "assistant") {
				continue
			}
			role, text = m.Message.Role, msgText(m.Message.Content)
		} else {
			var m struct {
				Type    string `json:"type"`
				Payload struct {
					Type    string          `json:"type"`
					Role    string          `json:"role"`
					Content json.RawMessage `json:"content"`
				} `json:"payload"`
			}
			if json.Unmarshal(line, &m) != nil || m.Type != "response_item" || m.Payload.Type != "message" {
				continue
			}
			role, text = m.Payload.Role, msgText(m.Payload.Content)
		}
		text = strings.TrimSpace(text)
		if (role != "user" && role != "assistant") || text == "" || strings.HasPrefix(text, "<") {
			continue
		}
		all = append(all, PreviewMsg{Role: role, Text: clip(text, 700)})
	}
	if len(all) > max {
		all = all[len(all)-max:]
	}
	return all
}
