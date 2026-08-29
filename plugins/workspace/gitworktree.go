package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func git(repo string, args ...string) (string, error) {
	c := exec.Command("git", append([]string{"-C", repo}, args...)...)
	out, err := c.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func ensureRepo(repo string) error {
	_, err := git(repo, "rev-parse", "--git-dir")
	return err
}

func resolveCommit(repo, ref string) (string, error) {
	if ref == "" {
		ref = "HEAD"
	}
	return git(repo, "rev-parse", "--verify", ref+"^{commit}")
}

func addWorktree(repo, path, branch, commit string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	_, err := git(repo, "worktree", "add", "-b", branch, path, commit)
	return err
}

func removeWorktree(repo, path string) error {
	if _, err := git(repo, "worktree", "remove", "--force", path); err != nil {
		return err
	}
	_, _ = git(repo, "worktree", "prune")
	return nil
}

func deleteBranch(repo, branch string) error {
	_, err := git(repo, "branch", "-D", branch)
	return err
}
