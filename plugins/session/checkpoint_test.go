package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runGitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", append([]string{"-C", dir}, args...)...)
	b, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, b)
	}
	return strings.TrimSpace(string(b))
}
func gitRepoWithChange(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	runGitT(t, d, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(d, "tracked.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitT(t, d, "add", "-A")
	runGitT(t, d, "-c", "user.email=test@example.com", "-c", "user.name=test", "commit", "-q", "-m", "init")
	if err := os.WriteFile(filepath.Join(d, "tracked.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "new.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return d
}
func gitCleanRepo(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	runGitT(t, d, "init", "-q", "-b", "main")
	_ = os.WriteFile(filepath.Join(d, "tracked.txt"), []byte("one\n"), 0o644)
	runGitT(t, d, "add", "-A")
	runGitT(t, d, "-c", "user.email=test@example.com", "-c", "user.name=test", "commit", "-q", "-m", "init")
	return d
}
func TestBuildCheckpointDirty(t *testing.T) {
	ws := gitRepoWithChange(t)
	head := runGitT(t, ws, "rev-parse", "HEAD")
	cp, patch, err := buildCheckpoint(ws)
	if err != nil {
		t.Fatal(err)
	}
	if cp.HeadCommit != head || cp.BaseCommit != head || cp.Branch == "" || !cp.Dirty {
		t.Fatalf("cp: %+v", cp)
	}
	if len(cp.UntrackedManifest) != 1 || cp.UntrackedManifest[0] != "new.txt" {
		t.Fatalf("untracked: %+v", cp.UntrackedManifest)
	}
	if !strings.Contains(patch, "two") {
		t.Fatalf("patch: %q", patch)
	}
}
func TestBuildCheckpointClean(t *testing.T) {
	ws := gitCleanRepo(t)
	cp, patch, err := buildCheckpoint(ws)
	if err != nil {
		t.Fatal(err)
	}
	if cp.Dirty || len(cp.UntrackedManifest) != 0 || patch != "" {
		t.Fatalf("cp=%+v patch=%q", cp, patch)
	}
}
