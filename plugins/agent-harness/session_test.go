package main

import (
	"context"
	"strings"
	"testing"
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
