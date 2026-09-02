package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const maxFrameText = 1 << 20 // 1 MiB per frame

const truncMark = "…[truncated]"

// codexArgv builds `codex exec` arguments AFTER the binary. §5 of the spec.
func codexArgv(spec RunSpec) []string {
	return []string{
		"exec",
		"--cd", spec.WorkspacePath,
		"--approve-for-me",
		"--skip-git-repo-check",
		"--json",
		"--color", "never",
		"--", spec.Prompt,
	}
}

type RealProvider struct {
	name    string
	bin     string
	argv    func(spec RunSpec) []string
	env     []string // NEVER nil at Run time; nil is rejected fail-closed
	timeout time.Duration
}

func (p RealProvider) Name() string { return p.name }

func redactedMeta(name string, exitCode *int) json.RawMessage {
	m := map[string]any{"provider": name}
	if exitCode != nil {
		m["exit_code"] = *exitCode
	} else {
		m["exit_code"] = nil
	}
	b, _ := json.Marshal(m)
	return b
}

func failClosed(name string) RunResult {
	return RunResult{Status: StatusFailed, ProviderMeta: redactedMeta(name, nil)}
}

func (p RealProvider) Run(ctx context.Context, spec RunSpec, out chan<- Frame) RunResult {
	defer close(out)

	if p.name == "" || p.bin == "" || p.argv == nil || p.env == nil {
		return failClosed(p.name)
	}
	args := p.argv(spec)
	if args == nil {
		return failClosed(p.name)
	}

	runCtx := ctx
	if p.timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, p.timeout)
		defer cancel()
	}
	if err := runCtx.Err(); err != nil {
		return statusFromCtx(runCtx, p.name)
	}

	cmd := exec.Command(p.bin, args...)
	cmd.Dir = spec.WorkspacePath
	cmd.Env = p.env

	// Ordinary OS pipes: assign the write ends to cmd, keep the read ends for
	// our readers. Do NOT use Cmd.StdoutPipe/StderrPipe — in Go 1.19 Cmd.Wait
	// closes those, and waiting for readers before Wait can deadlock if a
	// descendant inherits the fd.
	orp, owp, e1 := os.Pipe()
	erp, ewp, e2 := os.Pipe()
	if e1 != nil || e2 != nil {
		for _, f := range []*os.File{orp, owp, erp, ewp} {
			if f != nil {
				f.Close()
			}
		}
		return failClosed(p.name)
	}
	cmd.Stdout = owp
	cmd.Stderr = ewp

	if err := startProcess(cmd); err != nil {
		orp.Close()
		owp.Close()
		erp.Close()
		ewp.Close()
		return failClosed(p.name)
	}
	// The child holds its own copies now.
	owp.Close()
	ewp.Close()

	var (
		frameMu    sync.Mutex
		nextIdx    = 0
		readersWG  sync.WaitGroup
		stopOnce   sync.Once
		readerStop = make(chan struct{})
	)
	stopReaders := func() {
		stopOnce.Do(func() {
			close(readerStop)
			orp.Close()
			erp.Close()
		})
	}

	emit := func(kind, text string) bool {
		frameMu.Lock()
		defer frameMu.Unlock()
		nextIdx++
		f := Frame{Kind: kind, Text: text, Index: nextIdx}
		select {
		case out <- f:
			return true
		case <-runCtx.Done():
			return false
		case <-readerStop:
			return false
		}
	}

	readPipe := func(r io.Reader, kind string) {
		defer readersWG.Done()
		br := bufio.NewReader(r)
		var acc []byte
		flush := func() bool {
			if len(acc) == 0 {
				return true
			}
			t := strings.TrimSuffix(string(acc), "\r")
			acc = acc[:0]
			return emit(kind, t)
		}
		for {
			chunk, isPrefix, err := br.ReadLine()
			room := maxFrameText - len(acc)
			if room > len(chunk) {
				room = len(chunk)
			}
			if room > 0 {
				acc = append(acc, chunk[:room]...)
			}
			if len(chunk) > room {
				// The extra bytes prove that this logical line exceeds the cap.
				// Do not mark merely because ReadLine returned isPrefix at the
				// exact cap: the next read may contain only the line terminator.
				acc = acc[:maxFrameText-len(truncMark)]
				acc = append(acc, truncMark...)
				if !emit(kind, string(acc)) {
					return
				}
				acc = acc[:0]
				// Discard remaining prefix fragments, including the current
				// fragment's unretained bytes.
				for isPrefix {
					_, isPrefix, err = br.ReadLine()
					if err != nil {
						break
					}
				}
			}
			if err != nil {
				if err != io.EOF {
					select {
					case <-readerStop:
						// forced close — normal
					default:
						emit(kind, "[read error: "+err.Error()+"]")
					}
				}
				flush() // residual with no trailing newline
				return
			}
			if !isPrefix {
				if !flush() {
					return
				}
			}
		}
	}

	readersWG.Add(2)
	go readPipe(orp, "stdout")
	go readPipe(erp, "stderr")

	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()

	const grace = 3 * time.Second
	var processErr error
	select {
	case processErr = <-waitErr:
		// Child exited; give readers a bounded window to drain EOF.
		drained := make(chan struct{})
		go func() { readersWG.Wait(); close(drained) }()
		select {
		case <-drained:
		case <-runCtx.Done():
			stopReaders()
			<-drained
		case <-time.After(grace):
			stopReaders()
			<-drained
		}
	case <-runCtx.Done():
		killProcessGroup(cmd)
		select {
		case processErr = <-waitErr:
		case <-time.After(grace):
			stopReaders()
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			processErr = <-waitErr
		}
		stopReaders()
		readersWG.Wait()
	}
	stopReaders() // idempotent; releases descriptors on the normal path too

	return mapStatus(runCtx, p.name, processErr)
}

func statusFromCtx(ctx context.Context, name string) RunResult {
	switch ctx.Err() {
	case context.DeadlineExceeded:
		return RunResult{Status: StatusTimeout, ProviderMeta: redactedMeta(name, nil)}
	default:
		return RunResult{Status: StatusCancelled, ProviderMeta: redactedMeta(name, nil)}
	}
}

func mapStatus(ctx context.Context, name string, err error) RunResult {
	if ctx.Err() == context.DeadlineExceeded {
		return RunResult{Status: StatusTimeout, ProviderMeta: redactedMeta(name, nil)}
	}
	if ctx.Err() == context.Canceled {
		return RunResult{Status: StatusCancelled, ProviderMeta: redactedMeta(name, nil)}
	}
	if err == nil {
		z := 0
		return RunResult{Status: StatusCompleted, ProviderMeta: redactedMeta(name, &z)}
	}
	if ee, ok := err.(*exec.ExitError); ok {
		c := ee.ExitCode()
		return RunResult{Status: StatusFailed, ProviderMeta: redactedMeta(name, &c)}
	}
	return RunResult{Status: StatusFailed, ProviderMeta: redactedMeta(name, nil)}
}
