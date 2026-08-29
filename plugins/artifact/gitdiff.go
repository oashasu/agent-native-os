package main

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

type DiffSummary struct {
	FilesChanged int      `json:"files_changed"`
	Insertions   int      `json:"insertions"`
	Deletions    int      `json:"deletions"`
	Files        []string `json:"files"`
}

func git(dir string, args ...string) (string, error) {
	c := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := c.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func collectDiff(workspacePath, base string) (patch string, summary DiffSummary, err error) {
	if base == "" {
		base = "HEAD"
	}
	patch, err = git(workspacePath, "--no-pager", "diff", base)
	if err != nil {
		return "", DiffSummary{}, err
	}
	numstat, err := git(workspacePath, "--no-pager", "diff", "--numstat", base)
	if err != nil {
		return "", DiffSummary{}, err
	}
	for _, line := range strings.Split(numstat, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		ins, del := 0, 0
		if parts[0] != "-" {
			ins, _ = strconv.Atoi(parts[0])
		}
		if parts[1] != "-" {
			del, _ = strconv.Atoi(parts[1])
		}
		summary.FilesChanged++
		summary.Insertions += ins
		summary.Deletions += del
		summary.Files = append(summary.Files, parts[2])
	}
	untracked, err := git(workspacePath, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return "", DiffSummary{}, err
	}
	for _, name := range strings.Split(untracked, "\n") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		summary.FilesChanged++
		summary.Files = append(summary.Files, name+" (untracked)")
	}
	return patch, summary, nil
}
