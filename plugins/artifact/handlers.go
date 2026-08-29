package main

import (
	"encoding/json"
	"time"

	"github.com/example/agent-native-microkernel/sdk/go/fencing"
	"github.com/example/agent-native-microkernel/sdk/go/pluginhost"
	"github.com/example/agent-native-microkernel/sdk/go/protocol"
)

type collectDiffRequest struct {
	WorkContextID string `json:"work_context_id"`
	WorkspacePath string `json:"workspace_path"`
	BaseRef       string `json:"base_ref"`
}

func collectDiffHandler(s *Store, blobPut func([]byte) (string, error)) pluginhost.Handler {
	return func(e protocol.Envelope) (any, *protocol.Error) {
		var q collectDiffRequest
		if err := json.Unmarshal(e.Payload, &q); err != nil || q.WorkContextID == "" || q.WorkspacePath == "" {
			return nil, &protocol.Error{Code: "INVALID", Message: "work_context_id and workspace_path are required"}
		}
		patch, sum, err := collectDiff(q.WorkspacePath, q.BaseRef)
		if err != nil {
			return nil, &protocol.Error{Code: "GIT_ERROR", Message: err.Error()}
		}
		var a Artifact
		err = fencing.WithWriteFence(e, func() error {
			uri, err := blobPut([]byte(patch))
			if err != nil {
				return err
			}
			a = Artifact{
				ID:            protocol.NewID("art"),
				WorkContextID: q.WorkContextID,
				Kind:          "diff",
				BlobURI:       uri,
				Summary:       sum,
				CreatedAt:     time.Now().UTC().Format(time.RFC3339Nano),
			}
			return s.Record(a)
		})
		if err != nil {
			return nil, &protocol.Error{Code: "IO", Message: err.Error(), Retryable: true}
		}
		return map[string]any{"artifact": a}, nil
	}
}

type artifactIDRequest struct {
	ArtifactID string `json:"artifact_id"`
}

type artifactQueryRequest struct {
	WorkContextID string `json:"work_context_id"`
}

func getHandler(s *Store) pluginhost.Handler {
	return func(e protocol.Envelope) (any, *protocol.Error) {
		var q artifactIDRequest
		if err := json.Unmarshal(e.Payload, &q); err != nil || q.ArtifactID == "" {
			return nil, &protocol.Error{Code: "INVALID", Message: "artifact_id is required"}
		}
		a, ok := s.GetByID(q.ArtifactID)
		if !ok {
			return nil, &protocol.Error{Code: "NOT_FOUND", Message: ErrNotFound.Error()}
		}
		return map[string]any{"artifact": a}, nil
	}
}

func queryHandler(s *Store) pluginhost.Handler {
	return func(e protocol.Envelope) (any, *protocol.Error) {
		var q artifactQueryRequest
		if err := json.Unmarshal(e.Payload, &q); err != nil || q.WorkContextID == "" {
			return nil, &protocol.Error{Code: "INVALID", Message: "work_context_id is required"}
		}
		artifacts := s.QueryByContext(q.WorkContextID)
		if artifacts == nil {
			artifacts = []Artifact{}
		}
		return map[string]any{"artifacts": artifacts}, nil
	}
}
