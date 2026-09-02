package main

import (
	"context"
	"sync"
)

type runEntry struct {
	cancel context.CancelFunc
	once   sync.Once
	done   chan struct{}
}

type runRegistry struct {
	mu      sync.Mutex
	entries map[string]*runEntry
}

func newRunRegistry() *runRegistry {
	return &runRegistry{entries: map[string]*runEntry{}}
}

func (r *runRegistry) register(id string, cancel context.CancelFunc, done chan struct{}) {
	r.mu.Lock()
	r.entries[id] = &runEntry{cancel: cancel, done: done}
	r.mu.Unlock()
}

// stop cancels the run's context exactly once and returns its done channel.
// The entry stays registered until done(id) so concurrent cancels all join the
// same live run instead of falling through to the store-only fallback.
func (r *runRegistry) stop(id string) (<-chan struct{}, bool) {
	r.mu.Lock()
	e := r.entries[id]
	r.mu.Unlock()
	if e == nil {
		return nil, false
	}
	e.once.Do(e.cancel)
	return e.done, true
}

func (r *runRegistry) done(id string) {
	r.mu.Lock()
	delete(r.entries, id)
	r.mu.Unlock()
}
