package main

import (
	"encoding/json"
	"fmt"
	"github.com/example/agent-native-microkernel/sdk/go/contracts/workv1"
	"github.com/example/agent-native-microkernel/sdk/go/fencing"
	"github.com/example/agent-native-microkernel/sdk/go/pluginhost"
	"github.com/example/agent-native-microkernel/sdk/go/protocol"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type store struct {
	mu     sync.Mutex
	path   string
	Works  map[string]workv1.Work           `json:"works"`
	Dedupe map[string]workv1.CreateResponse `json:"dedupe"`
}

func load() *store {
	d := os.Getenv("VIBE_DATA_DIR")
	_ = os.MkdirAll(d, 0755)
	s := &store{path: filepath.Join(d, "work.json"), Works: map[string]workv1.Work{}, Dedupe: map[string]workv1.CreateResponse{}}
	b, _ := os.ReadFile(s.path)
	_ = json.Unmarshal(b, s)
	if s.Works == nil {
		s.Works = map[string]workv1.Work{}
	}
	if s.Dedupe == nil {
		s.Dedupe = map[string]workv1.CreateResponse{}
	}
	return s
}
func (s *store) refresh() {
	b, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	var disk store
	if json.Unmarshal(b, &disk) == nil {
		if disk.Works != nil {
			s.Works = disk.Works
		}
		if disk.Dedupe != nil {
			s.Dedupe = disk.Dedupe
		}
	}
}

func (s *store) save() error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	cerr := f.Close()
	if err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err = os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	dir, err := os.Open(filepath.Dir(s.path))
	if err == nil {
		err = dir.Sync()
		_ = dir.Close()
	}
	return err
}
func main() {
	s := load()
	pid := os.Getenv("VIBE_PLUGIN_ID")
	if pid == "" {
		pid = "org.vibe.work.registry"
	}
	h := pluginhost.New(pid, "0.9.0", "")
	h.HandleContextCommand("work.create", 1, func(_ *pluginhost.RequestContext, e protocol.Envelope) (any, *protocol.Error) {
		var q workv1.CreateRequest
		if json.Unmarshal(e.Payload, &q) != nil || q.ID == "" {
			return nil, &protocol.Error{Code: "INVALID", Message: "id required"}
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		var out workv1.CreateResponse
		err := fencing.WithWriteFence(e, func() error {
			s.refresh()
			if e.IdempotencyKey != "" {
				if old, ok := s.Dedupe[e.IdempotencyKey]; ok {
					old.IdempotentReplay = true
					out = old
					return nil
				}
			}
			if w, ok := s.Works[q.ID]; ok {
				out = workv1.CreateResponse{Work: w, IdempotentReplay: true}
				return nil
			}
			w := workv1.Work{ID: q.ID, Title: q.Title, Status: "PLANNED", Version: 1}
			s.Works[q.ID] = w
			out = workv1.CreateResponse{Work: w}
			if e.IdempotencyKey != "" {
				s.Dedupe[e.IdempotencyKey] = out
			}
			if err := s.save(); err != nil {
				delete(s.Works, q.ID)
				if e.IdempotencyKey != "" {
					delete(s.Dedupe, e.IdempotencyKey)
				}
				return err
			}
			return nil
		})
		if err != nil {
			return nil, &protocol.Error{Code: "FENCING_OR_IO", Message: err.Error(), Retryable: true}
		}
		return out, nil
	})
	h.HandleQuery("work.get", 1, func(e protocol.Envelope) (any, *protocol.Error) {
		var q workv1.GetRequest
		_ = json.Unmarshal(e.Payload, &q)
		s.mu.Lock()
		defer s.mu.Unlock()
		s.refresh()
		w, ok := s.Works[q.ID]
		if !ok {
			return nil, &protocol.Error{Code: "NOT_FOUND", Message: "work not found"}
		}
		return workv1.GetResponse{Work: w}, nil
	})
	h.HandleCommand("work.transition", 1, func(e protocol.Envelope) (any, *protocol.Error) {
		var q workv1.TransitionRequest
		_ = json.Unmarshal(e.Payload, &q)
		s.mu.Lock()
		defer s.mu.Unlock()
		var out workv1.TransitionResponse
		err := fencing.WithWriteFence(e, func() error {
			s.refresh()
			w, ok := s.Works[q.ID]
			if !ok {
				return fmt.Errorf("NOT_FOUND: work not found")
			}
			if q.ExpectedVersion > 0 && w.Version != q.ExpectedVersion {
				return fmt.Errorf("CONFLICT: version mismatch")
			}
			previous := w
			w.Status = q.To
			w.Version++
			s.Works[q.ID] = w
			if err := s.save(); err != nil {
				s.Works[q.ID] = previous
				return err
			}
			out = workv1.TransitionResponse{Work: w}
			return nil
		})
		if err != nil {
			code := "FENCING_OR_IO"
			if strings.HasPrefix(err.Error(), "NOT_FOUND:") {
				code = "NOT_FOUND"
			}
			if strings.HasPrefix(err.Error(), "CONFLICT:") {
				code = "CONFLICT"
			}
			return nil, &protocol.Error{Code: code, Message: err.Error(), Retryable: code == "FENCING_OR_IO"}
		}
		return out, nil
	})
	_ = h.Serve()
}
