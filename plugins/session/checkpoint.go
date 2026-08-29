package main

import (
	"os/exec"
	"strings"
)

type SessionEventSelection struct {
	JournalCursorStart int      `json:"journal_cursor_start"`
	JournalCursorEnd   int      `json:"journal_cursor_end"`
	CorrelationID      string   `json:"correlation_id"`
	EventIDs           []string `json:"event_ids"`
	EventSHA256s       []string `json:"event_sha256s"`
	EventCount         int      `json:"event_count"`
}

type RecoveryCheckpoint struct {
	Repo                    string                `json:"repo"`
	BaseCommit              string                `json:"base_commit"`
	HeadCommit              string                `json:"head_commit"`
	Branch                  string                `json:"branch"`
	WorktreePathAtSeal      string                `json:"worktree_path_at_seal"`
	Dirty                   bool                  `json:"dirty"`
	TrackedPatchRef         string                `json:"tracked_patch_ref"`
	UntrackedManifest       []string              `json:"untracked_manifest"`
	DiffArtifactID          string                `json:"diff_artifact_id"`
	TaskID                  string                `json:"task_id"`
	WorkContextID           string                `json:"work_context_id"`
	AgentRunID              string                `json:"agent_run_id"`
	Provider                string                `json:"provider"`
	HarnessNativeID         string                `json:"harness_native_id"`
	CanonicalEventSelection SessionEventSelection `json:"canonical_event_selection"`
}

func git(dir string, args ...string) (string, error) {
	argv := append([]string{"-C", dir}, args...)
	c := exec.Command("git", argv...)
	b, err := c.CombinedOutput()
	if err != nil {
		return "", &gitError{args: args, out: strings.TrimSpace(string(b)), err: err}
	}
	return strings.TrimSpace(string(b)), nil
}

type gitError struct {
	args []string
	out  string
	err  error
}

func (e *gitError) Error() string {
	return "git " + strings.Join(e.args, " ") + ": " + e.err.Error() + ": " + e.out
}

func buildCheckpoint(workspacePath string) (cp RecoveryCheckpoint, patch string, err error) {
	head, err := git(workspacePath, "rev-parse", "HEAD")
	if err != nil {
		return cp, "", err
	}
	branch, err := git(workspacePath, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return cp, "", err
	}
	repo, _ := git(workspacePath, "rev-parse", "--path-format=absolute", "--git-common-dir")
	status, err := git(workspacePath, "status", "--porcelain")
	if err != nil {
		return cp, "", err
	}
	patch, err = git(workspacePath, "--no-pager", "diff", "HEAD")
	if err != nil {
		return cp, "", err
	}
	untracked, err := git(workspacePath, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return cp, "", err
	}
	files := []string{}
	if untracked != "" {
		files = strings.Split(untracked, "\n")
	}
	cp = RecoveryCheckpoint{Repo: repo, BaseCommit: head, HeadCommit: head, Branch: branch, WorktreePathAtSeal: workspacePath, Dirty: status != "", UntrackedManifest: files}
	return cp, patch, nil
}
