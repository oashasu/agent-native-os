package main

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestRunRegistryStopCancelsOnceKeepsEntry(t *testing.T) {
	r := newRunRegistry()
	var calls int32
	done := make(chan struct{})
	r.register("a", func() { atomic.AddInt32(&calls, 1) }, done)

	type result struct {
		done <-chan struct{}
		live bool
	}
	start := make(chan struct{})
	results := make(chan result, 32)
	doneRO := (<-chan struct{})(done)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			d, live := r.stop("a")
			results <- result{done: d, live: live}
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	for got := range results {
		if !got.live || got.done != doneRO {
			t.Fatalf("concurrent stop result: live=%v done=%v", got.live, got.done)
		}
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("cancel called %d times", calls)
	}

	r.done("a")
	if _, live := r.stop("a"); live {
		t.Fatal("stop after done() must be non-live")
	}
}

func TestRunRegistryMissingID(t *testing.T) {
	r := newRunRegistry()
	if _, live := r.stop("ghost"); live {
		t.Fatal("missing id is not live")
	}
}
