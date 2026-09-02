// Command fake-agent-cli is a deterministic stand-in for a real coding CLI,
// used only by agent-harness tests. Everything is argv — no environment — so a
// test knob can never leak into a production allowlist.
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

func main() {
	args := os.Args[1:]
	// Version forms (for discovery probing).
	for i, a := range args {
		switch a {
		case "--version":
			fmt.Println("fake-agent-cli 0.0.1")
			os.Exit(0)
		case "--version-exit":
			code := 0
			if i+1 < len(args) {
				code, _ = strconv.Atoi(args[i+1])
			}
			fmt.Println("fake-agent-cli 0.0.1")
			os.Exit(code)
		}
	}

	var cd, write, line, pidFile string
	var emitBytes, exitCode, sleepMS int
	var prompt []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--cd":
			i++
			cd = get(args, i)
		case "--write":
			i++
			write = get(args, i)
		case "--line":
			i++
			line = get(args, i)
		case "--pid-file":
			i++
			pidFile = get(args, i)
		case "--emit-bytes":
			i++
			emitBytes, _ = strconv.Atoi(get(args, i))
		case "--exit":
			i++
			exitCode, _ = strconv.Atoi(get(args, i))
		case "--sleep":
			i++
			sleepMS, _ = strconv.Atoi(get(args, i))
		case "--":
			prompt = args[i+1:]
			i = len(args)
		default:
			// ignore unknown flags so a test argv template can pass extras
		}
	}

	if cd != "" {
		if err := os.Chdir(cd); err != nil {
			fmt.Fprintln(os.Stderr, "chdir:", err)
			os.Exit(1)
		}
	}
	if pidFile != "" {
		_ = os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644)
	}

	p := strings.Join(prompt, " ")
	fmt.Println("fake-agent-cli: start")
	fmt.Println("fake-agent-cli: prompt=" + p)
	fmt.Fprintln(os.Stderr, "fake-agent-cli: working")

	if emitBytes > 0 {
		buf := make([]byte, emitBytes)
		for i := range buf {
			buf[i] = 'x'
		}
		os.Stdout.Write(buf)
		os.Stdout.Write([]byte{'\n'})
	}
	if write != "" {
		f, err := os.OpenFile(write, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err == nil {
			f.WriteString(line + "\n")
			f.Close()
		}
	}
	fmt.Println("fake-agent-cli: done")

	// Sleep in short slices so SIGKILL/SIGTERM from a process-group kill takes
	// effect promptly (the test observes process-group termination; we do not
	// install a handler).
	for left := sleepMS; left > 0; left -= 20 {
		d := 20
		if left < d {
			d = left
		}
		time.Sleep(time.Duration(d) * time.Millisecond)
	}
	os.Exit(exitCode)
}

func get(args []string, i int) string {
	if i < len(args) {
		return args[i]
	}
	return ""
}
