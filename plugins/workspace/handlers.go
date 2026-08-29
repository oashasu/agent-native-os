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

type releaseRequest struct {
	WorkspaceID string `json:"workspace_id"`
	Policy      string `json:"policy"`
}

func releaseHandler(s *Store) pluginhost.Handler {
	return func(e protocol.Envelope) (any, *protocol.Error) {
		var q releaseRequest
		if err := json.Unmarshal(e.Payload, &q); err != nil || q.WorkspaceID == "" || (q.Policy != "preserve" && q.Policy != "delete") {
			return nil, &protocol.Error{Code: "INVALID", Message: "workspace_id and policy preserve|delete are required"}
		}
		ref, ok := s.GetByID(q.WorkspaceID)
		if !ok {
			return nil, &protocol.Error{Code: "NOT_FOUND", Message: ErrNotFound.Error()}
		}
		if ref.Status == StatusReleased {
			if ref.ReleasePolicy == q.Policy {
				return map[string]any{"workspace": ref}, nil
			}
			return nil, &protocol.Error{Code: "CONFLICT", Message: "workspace already released with different policy"}
		}
		var updated WorkspaceRef
		failureCode := "IO"
		err := fencing.WithWriteFence(e, func() error {
			if q.Policy == "delete" {
				if err := removeWorktree(ref.Repo, ref.Path); err != nil {
					failureCode = "GIT_ERROR"
					return err
				}
				_ = deleteBranch(ref.Repo, ref.Branch)
			}
			var err error
			updated, err = s.RecordReleased(q.WorkspaceID, q.Policy)
			return err
		})
		if err != nil {
			return nil, &protocol.Error{Code: failureCode, Message: err.Error(), Retryable: failureCode == "IO"}
		}
		return map[string]any{"workspace": updated}, nil
	}
}

type getRequest struct {
	WorkspaceID   string `json:"workspace_id"`
	WorkContextID string `json:"work_context_id"`
}

func getHandler(s *Store) pluginhost.Handler {
	return func(e protocol.Envelope) (any, *protocol.Error) {
		var q getRequest
		if err := json.Unmarshal(e.Payload, &q); err != nil {
			return nil, &protocol.Error{Code: "INVALID", Message: "bad request"}
		}
		if (q.WorkspaceID == "") == (q.WorkContextID == "") {
			return nil, &protocol.Error{Code: "INVALID", Message: "exactly one of workspace_id or work_context_id is required"}
		}
		var ref WorkspaceRef
		var ok bool
		if q.WorkspaceID != "" {
			ref, ok = s.GetByID(q.WorkspaceID)
		} else {
			ref, ok = s.GetActiveByContext(q.WorkContextID)
		}
		if !ok {
			return nil, &protocol.Error{Code: "NOT_FOUND", Message: ErrNotFound.Error()}
		}
		return map[string]any{"workspace": ref}, nil
	}
}
