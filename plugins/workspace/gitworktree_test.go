package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func scratchRepo(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		c := exec.Command("git", append([]string{"-C", dir,
			"-c", "user.email=test@example.com", "-c", "user.name=test",
			"-c", "commit.gpgsign=false"}, args...)...)
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-q", "-m", "init")
	return dir, run("rev-parse", "HEAD")
}

func TestResolveCommitAndAddRemoveWorktree(t *testing.T) {
	repo, head := scratchRepo(t)
	got, err := resolveCommit(repo, "")
	if err != nil || got != head {
		t.Fatalf("resolveCommit HEAD = %q err=%v, want %q", got, err, head)
	}
	wt := filepath.Join(t.TempDir(), "wt")
	if err := addWorktree(repo, wt, "aeos/ws-test", head); err != nil {
		t.Fatalf("addWorktree: %v", err)
	}
	br := gitOut(t, wt, "rev-parse", "--abbrev-ref", "HEAD")
	if br != "aeos/ws-test" {
		t.Fatalf("worktree branch = %q", br)
	}
	if sha := gitOut(t, wt, "rev-parse", "HEAD"); sha != head {
		t.Fatalf("worktree HEAD = %q, want %q", sha, head)
	}
	if err := removeWorktree(repo, wt); err != nil {
		t.Fatalf("removeWorktree: %v", err)
	}
	if _, statErr := os.Stat(wt); !os.IsNotExist(statErr) {
		t.Fatalf("worktree dir still present after remove")
	}
}

func TestEnsureRepoRejectsNonRepo(t *testing.T) {
	if err := ensureRepo(t.TempDir()); err == nil {
		t.Fatal("ensureRepo should fail on a non-git directory")
	}
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
