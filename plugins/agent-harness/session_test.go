package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRunProviderMirrorsAndCaptures(t *testing.T) {
	mirror := make(chan any, 16)
	var tr Transcript
	done := make(chan struct{})
	go func() {
		tr = runProvider(context.Background(), MockProvider{}, RunSpec{WorkspacePath: t.TempDir(), Prompt: "p", MockSteps: 3, MockDelayMS: 1}, mirror)
		close(done)
	}()
	var mirrored int
	for range mirror {
		mirrored++
	}
	<-done
	if mirrored != 3 {
		t.Fatalf("mirrored %d frames, want 3", mirrored)
	}
	if len(tr.Frames) != 3 || tr.Result.Status != StatusCompleted {
		t.Fatalf("transcript: %+v", tr)
	}
	body := string(tr.Bytes())
	if strings.Count(body, "\n") < 3 || !strings.Contains(body, "result: COMPLETED") {
		t.Fatalf("transcript bytes:\n%s", body)
	}
}

func TestRunProviderClosesMirrorEvenOnFailure(t *testing.T) {
	mirror := make(chan any, 16)
	tr := runProvider(context.Background(), MockProvider{}, RunSpec{WorkspacePath: t.TempDir(), Prompt: "p", MockSteps: 5, MockDelayMS: 1, MockFailAt: 2}, mirror)
	for range mirror {
	}
	if tr.Result.Status != StatusFailed {
		t.Fatalf("want FAILED, got %+v", tr.Result)
	}
}

func TestRunProviderStopsMirroringOnCancel(t *testing.T) {
	// A provider that emits one frame, then blocks until ctx is done.
	prov := blockingProvider{emitted: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	mirror := make(chan any) // unbuffered, no consumer
	doneCh := make(chan Transcript, 1)
	go func() { doneCh <- runProvider(ctx, prov, RunSpec{Prompt: "p"}, mirror) }()
	select {
	case <-prov.emitted:
	case <-time.After(1 * time.Second):
		t.Fatal("blocking provider did not emit")
	}
	cancel()
	select {
	case <-doneCh:
	case <-time.After(1 * time.Second):
		t.Fatal("runProvider did not return promptly after cancel")
	}
}

type blockingProvider struct {
	emitted chan struct{}
}

func (blockingProvider) Name() string { return "blocking" }
func (p blockingProvider) Run(ctx context.Context, _ RunSpec, out chan<- Frame) RunResult {
	defer close(out)
	out <- Frame{Kind: "stdout", Text: "one", Index: 1}
	close(p.emitted)
	<-ctx.Done()
	return RunResult{Status: StatusCancelled}
}
