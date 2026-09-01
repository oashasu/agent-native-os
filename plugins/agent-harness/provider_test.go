package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func drain(out <-chan Frame) []Frame {
	var fs []Frame
	for f := range out {
		fs = append(fs, f)
	}
	return fs
}

func TestMockProviderEmitsFramesAndTouchesWorkspace(t *testing.T) {
	ws := t.TempDir()
	p := MockProvider{}
	out := make(chan Frame, 16)
	done := make(chan RunResult, 1)
	go func() {
		res := p.Run(context.Background(), RunSpec{WorkspacePath: ws, Prompt: "harden add", MockSteps: 4, MockDelayMS: 1, MockWriteFile: "src/Calc.java", MockWriteContent: "// x\n"}, out)
		_, _ = json.Marshal(res)
		done <- res
	}()
	fs := drain(out)
	if len(fs) != 4 || fs[0].Kind != "stdout" || fs[3].Index != 4 {
		t.Fatalf("frames: %+v", fs)
	}
	res := <-done
	if res.Status != StatusCompleted {
		t.Fatalf("result: %+v", res)
	}
	b, err := os.ReadFile(filepath.Join(ws, "src/Calc.java"))
	if err != nil || string(b) != "// x\n" {
		t.Fatalf("workspace file: %q err=%v", b, err)
	}
}

func TestMockProviderFailAt(t *testing.T) {
	p := MockProvider{}
	out := make(chan Frame, 16)
	res := p.Run(context.Background(), RunSpec{WorkspacePath: t.TempDir(), Prompt: "p", MockSteps: 5, MockDelayMS: 1, MockFailAt: 3}, out)
	if res.Status != StatusFailed {
		t.Fatalf("expected FAILED, got %+v", res)
	}
}

func TestMockProviderCancel(t *testing.T) {
	p := MockProvider{}
	ctx, cancel := context.WithCancel(context.Background())
	out := make(chan Frame, 1)
	done := make(chan RunResult, 1)
	go func() {
		done <- p.Run(ctx, RunSpec{WorkspacePath: t.TempDir(), Prompt: "p", MockSteps: 50, MockDelayMS: 20}, out)
	}()
	<-out
	cancel()
	select {
	case res := <-done:
		if res.Status != StatusCancelled {
			t.Fatalf("expected CANCELLED, got %+v", res)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("provider did not stop on cancel")
	}
}
