package main

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/example/agent-native-microkernel/sdk/go/fencing"
	"github.com/example/agent-native-microkernel/sdk/go/pluginhost"
	"github.com/example/agent-native-microkernel/sdk/go/protocol"
)

type requestReviewRequest struct {
	WorkContextID    string                 `json:"work_context_id"`
	AgentRunID       string                 `json:"agent_run_id"`
	DiffArtifactID   string                 `json:"diff_artifact_id"`
	EvidenceSnapshot []EvidenceSnapshotItem `json:"evidence_snapshot"`
}

type decideReviewRequest struct {
	ReviewID          string             `json:"review_id"`
	Decision          string             `json:"decision"`
	Reviewer          string             `json:"reviewer"`
	Notes             string             `json:"notes"`
	AcceptanceResults []AcceptanceResult `json:"acceptance_results"`
}

type reviewIDRequest struct {
	ReviewID string `json:"review_id"`
}
type reviewQueryRequest struct {
	WorkContextID string `json:"work_context_id"`
}

func requestHandler(s *Store) pluginhost.Handler {
	return func(e protocol.Envelope) (any, *protocol.Error) {
		var q requestReviewRequest
		if err := json.Unmarshal(e.Payload, &q); err != nil || q.WorkContextID == "" || q.DiffArtifactID == "" {
			return nil, &protocol.Error{Code: "INVALID", Message: "work_context_id and diff_artifact_id are required"}
		}
		r := Review{ID: protocol.NewID("rev"), WorkContextID: q.WorkContextID, AgentRunID: q.AgentRunID, DiffArtifactID: q.DiffArtifactID, Status: "PENDING", EvidenceSnapshot: q.EvidenceSnapshot, RequestedAt: time.Now().UTC().Format(time.RFC3339Nano)}
		if err := fencing.WithWriteFence(e, func() error { return s.RecordRequested(r) }); err != nil {
			return nil, &protocol.Error{Code: "IO", Message: err.Error(), Retryable: true}
		}
		return map[string]any{"review": r}, nil
	}
}

func decideHandler(s *Store) pluginhost.Handler {
	return func(e protocol.Envelope) (any, *protocol.Error) {
		var q decideReviewRequest
		if err := json.Unmarshal(e.Payload, &q); err != nil || q.ReviewID == "" || (q.Decision != "APPROVED" && q.Decision != "CHANGES_REQUESTED") {
			return nil, &protocol.Error{Code: "INVALID", Message: "review_id and valid decision are required"}
		}
		var r Review
		err := fencing.WithWriteFence(e, func() error {
			var err error
			r, err = s.RecordDecided(q.ReviewID, q.Decision, q.Reviewer, q.Notes, q.AcceptanceResults)
			return err
		})
		if err != nil {
			switch {
			case errors.Is(err, ErrNotFound):
				return nil, &protocol.Error{Code: "NOT_FOUND", Message: err.Error()}
			case errors.Is(err, ErrAlreadyDecided):
				return nil, &protocol.Error{Code: "CONFLICT", Message: err.Error()}
			default:
				return nil, &protocol.Error{Code: "IO", Message: err.Error(), Retryable: true}
			}
		}
		return map[string]any{"review": r}, nil
	}
}

func getHandler(s *Store) pluginhost.Handler {
	return func(e protocol.Envelope) (any, *protocol.Error) {
		var q reviewIDRequest
		if err := json.Unmarshal(e.Payload, &q); err != nil || q.ReviewID == "" {
			return nil, &protocol.Error{Code: "INVALID", Message: "review_id is required"}
		}
		r, ok := s.GetByID(q.ReviewID)
		if !ok {
			return nil, &protocol.Error{Code: "NOT_FOUND", Message: ErrNotFound.Error()}
		}
		return map[string]any{"review": r}, nil
	}
}
func queryHandler(s *Store) pluginhost.Handler {
	return func(e protocol.Envelope) (any, *protocol.Error) {
		var q reviewQueryRequest
		if err := json.Unmarshal(e.Payload, &q); err != nil || q.WorkContextID == "" {
			return nil, &protocol.Error{Code: "INVALID", Message: "work_context_id is required"}
		}
		rs := s.QueryByContext(q.WorkContextID)
		if rs == nil {
			rs = []Review{}
		}
		return map[string]any{"reviews": rs}, nil
	}
}
