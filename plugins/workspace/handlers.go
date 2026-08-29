package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/example/agent-native-microkernel/sdk/go/fencing"
	"github.com/example/agent-native-microkernel/sdk/go/pluginhost"
	"github.com/example/agent-native-microkernel/sdk/go/protocol"
)

type allocateRequest struct {
	WorkContextID string `json:"work_context_id"`
	Repo          string `json:"repo"`
	BaseRef       string `json:"base_ref"`
}

func allocateHandler(s *Store) pluginhost.Handler {
	return func(e protocol.Envelope) (any, *protocol.Error) {
		var q allocateRequest
		if err := json.Unmarshal(e.Payload, &q); err != nil || q.WorkContextID == "" || q.Repo == "" {
			return nil, &protocol.Error{Code: "INVALID", Message: "work_context_id and repo are required"}
		}
		if err := ensureRepo(q.Repo); err != nil {
			return nil, &protocol.Error{Code: "GIT_ERROR", Message: err.Error()}
		}
		commit, err := resolveCommit(q.Repo, q.BaseRef)
		if err != nil {
			return nil, &protocol.Error{Code: "GIT_ERROR", Message: err.Error()}
		}
		id := protocol.NewID("ws")
		branch := "aeos/ws-" + last8(id)
		path := filepath.Join(os.Getenv("VIBE_DATA_DIR"), "worktrees", id)
		ref := WorkspaceRef{
			ID: id, WorkContextID: q.WorkContextID, Repo: q.Repo, Path: path, Branch: branch,
			BaseCommit: commit, Status: StatusAllocated, AllocatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		}
		failureCode := "IO"
		err = fencing.WithWriteFence(e, func() error {
			if err := addWorktree(q.Repo, path, branch, commit); err != nil {
				failureCode = "GIT_ERROR"
				_ = removeWorktree(q.Repo, path)
				return err
			}
			if err := s.RecordAllocated(ref); err != nil {
				_ = removeWorktree(q.Repo, path)
				_ = deleteBranch(q.Repo, branch)
				return err
			}
			return nil
		})
		if err != nil {
			return nil, &protocol.Error{Code: failureCode, Message: err.Error(), Retryable: failureCode == "IO"}
		}
		return map[string]any{"workspace": ref}, nil
	}
}

func last8(s string) string {
	r := []rune(s)
	if len(r) <= 8 {
		return s
	}
	return string(r[len(r)-8:])
}
