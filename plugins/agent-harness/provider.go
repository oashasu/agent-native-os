package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Frame struct {
	Kind  string `json:"kind"`
	Text  string `json:"text"`
	Index int    `json:"index"`
}

type RunSpec struct {
	WorkspacePath    string
	Prompt           string
	MockSteps        int
	MockDelayMS      int
	MockFailAt       int
	MockWriteFile    string
	MockWriteContent string
}

type RunResult struct {
	Status       string
	NativeID     string
	ProviderMeta json.RawMessage
}

type Provider interface {
	Name() string
	Run(ctx context.Context, spec RunSpec, out chan<- Frame) RunResult
}

type MockProvider struct{}

func (MockProvider) Name() string { return "mock" }

func (MockProvider) Run(ctx context.Context, spec RunSpec, out chan<- Frame) RunResult {
	defer close(out)
	steps := spec.MockSteps
	if steps <= 0 {
		steps = 3
	}
	delayMS := spec.MockDelayMS
	if delayMS <= 0 {
		delayMS = 5
	}
	content := spec.MockWriteContent
	if content == "" {
		content = "// touched by mock agent\n"
	}
	meta, _ := json.Marshal(map[string]int{"steps": steps})
	nativeID := mockNativeID(spec, steps)
	prefix := spec.Prompt
	if len(prefix) > 40 {
		prefix = prefix[:40]
	}

	for i := 1; i <= steps; i++ {
		if i > 1 {
			select {
			case <-ctx.Done():
				return RunResult{Status: StatusCancelled, NativeID: nativeID, ProviderMeta: meta}
			case <-time.After(time.Duration(delayMS) * time.Millisecond):
			}
		}
		if spec.MockFailAt > 0 && i == spec.MockFailAt {
			out <- Frame{Kind: "stderr", Text: fmt.Sprintf("mock failure at step %d", i), Index: i}
			return RunResult{Status: StatusFailed, NativeID: nativeID, ProviderMeta: meta}
		}
		out <- Frame{Kind: "stdout", Text: fmt.Sprintf("step-%d: %s", i, prefix), Index: i}
		if i == 1 && spec.MockWriteFile != "" {
			path := filepath.Join(spec.WorkspacePath, filepath.Clean(spec.MockWriteFile))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return RunResult{Status: StatusFailed, NativeID: nativeID, ProviderMeta: meta}
			}
			f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
			if err != nil {
				return RunResult{Status: StatusFailed, NativeID: nativeID, ProviderMeta: meta}
			}
			_, werr := f.WriteString(content)
			cerr := f.Close()
			if werr != nil || cerr != nil {
				return RunResult{Status: StatusFailed, NativeID: nativeID, ProviderMeta: meta}
			}
		}
	}
	return RunResult{Status: StatusCompleted, NativeID: nativeID, ProviderMeta: meta}
}

func mockNativeID(spec RunSpec, steps int) string {
	h := sha256.Sum256([]byte(strings.Join([]string{spec.WorkspacePath, spec.Prompt, fmt.Sprint(steps)}, "\x00")))
	return "mock-" + hex.EncodeToString(h[:6])
}
