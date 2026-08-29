package main

import (
	"encoding/json"
	"errors"

	"github.com/example/agent-native-microkernel/sdk/go/fencing"
	"github.com/example/agent-native-microkernel/sdk/go/pluginhost"
	"github.com/example/agent-native-microkernel/sdk/go/protocol"
)

type createRequest struct {
	Title              string                `json:"title"`
	Goal               string                `json:"goal"`
	Scope              string                `json:"scope"`
	Repo               string                `json:"repo"`
	AcceptanceCriteria []AcceptanceCriterion `json:"acceptance_criteria"`
}

type getRequest struct {
	TaskID        string `json:"task_id"`
	WorkContextID string `json:"work_context_id"`
}

func createHandler(s *Store) pluginhost.Handler {
	return func(e protocol.Envelope) (any, *protocol.Error) {
		var q createRequest
		if err := json.Unmarshal(e.Payload, &q); err != nil || q.Title == "" || q.Goal == "" || q.Repo == "" {
			return nil, &protocol.Error{Code: "INVALID", Message: "title, goal and repo are required"}
		}
		var task *Task
		var wc *WorkContext
		var replay bool
		err := fencing.WithWriteFence(e, func() error {
			var err error
			task, wc, replay, err = s.CreateTask(CreateInput{
				Title: q.Title, Goal: q.Goal, Scope: q.Scope, Repo: q.Repo,
				Acceptance: q.AcceptanceCriteria, IdempotencyKey: e.IdempotencyKey,
			})
			return err
		})
		if err != nil {
			return nil, &protocol.Error{Code: "IO", Message: err.Error(), Retryable: true}
		}
		return map[string]any{"task": task, "work_context": wc, "idempotent_replay": replay}, nil
	}
}

func getHandler(s *Store) pluginhost.Handler {
	return func(e protocol.Envelope) (any, *protocol.Error) {
		var q getRequest
		if err := json.Unmarshal(e.Payload, &q); err != nil {
			return nil, &protocol.Error{Code: "INVALID", Message: "bad request"}
		}
		if (q.TaskID == "") == (q.WorkContextID == "") {
			return nil, &protocol.Error{Code: "INVALID", Message: "exactly one of task_id or work_context_id is required"}
		}
		var task Task
		var wc WorkContext
		var ok bool
		if q.TaskID != "" {
			task, wc, ok = s.GetByTask(q.TaskID)
		} else {
			task, wc, ok = s.GetByContext(q.WorkContextID)
		}
		if !ok {
			return nil, &protocol.Error{Code: "NOT_FOUND", Message: "work item not found"}
		}
		return map[string]any{"task": task, "work_context": wc}, nil
	}
}

type transitionRequest struct {
	WorkContextID   string `json:"work_context_id"`
	To              string `json:"to"`
	ExpectedVersion *int   `json:"expected_version"`
}

func transitionHandler(s *Store) pluginhost.Handler {
	return func(e protocol.Envelope) (any, *protocol.Error) {
		var q transitionRequest
		if err := json.Unmarshal(e.Payload, &q); err != nil {
			return nil, &protocol.Error{Code: "INVALID", Message: "bad request"}
		}
		validStatus := q.To == string(StatusInProgress) || q.To == string(StatusInReview) || q.To == string(StatusDone) || q.To == string(StatusFailed)
		if q.WorkContextID == "" || !validStatus || q.ExpectedVersion == nil {
			return nil, &protocol.Error{Code: "INVALID", Message: "work_context_id, valid to, and expected_version are required"}
		}
		var task *Task
		var wc *WorkContext
		err := fencing.WithWriteFence(e, func() error {
			var err error
			task, wc, err = s.Transition(q.WorkContextID, Status(q.To), *q.ExpectedVersion)
			return err
		})
		if err != nil {
			switch {
			case errors.Is(err, ErrNotFound):
				return nil, &protocol.Error{Code: "NOT_FOUND", Message: err.Error()}
			case errors.Is(err, ErrConflict):
				return nil, &protocol.Error{Code: "CONFLICT", Message: err.Error()}
			case errors.Is(err, ErrIllegalTransition):
				return nil, &protocol.Error{Code: "ILLEGAL_TRANSITION", Message: err.Error()}
			default:
				return nil, &protocol.Error{Code: "IO", Message: err.Error(), Retryable: true}
			}
		}
		return map[string]any{"task": task, "work_context": wc}, nil
	}
}

type attachEvidenceRequest struct {
	WorkContextID    string `json:"work_context_id"`
	Kind             string `json:"kind"`
	SourceCapability string `json:"source_capability"`
	SourceID         string `json:"source_id"`
	Outcome          string `json:"outcome"`
	ContentHash      string `json:"content_hash"`
	ExpectedVersion  *int   `json:"expected_version"`
}

func attachEvidenceHandler(s *Store) pluginhost.Handler {
	return func(e protocol.Envelope) (any, *protocol.Error) {
		var q attachEvidenceRequest
		if err := json.Unmarshal(e.Payload, &q); err != nil {
			return nil, &protocol.Error{Code: "INVALID", Message: "bad request"}
		}
		validKind := q.Kind == "build" || q.Kind == "test" || q.Kind == "review"
		validOutcome := q.Outcome == "PASS" || q.Outcome == "FAIL"
		if q.WorkContextID == "" || !validKind || q.SourceCapability == "" || q.SourceID == "" || !validOutcome || q.ExpectedVersion == nil {
			return nil, &protocol.Error{Code: "INVALID", Message: "work_context_id, kind, source_capability, source_id, outcome and expected_version are required"}
		}
		var ev *EvidenceRef
		var wc *WorkContext
		err := fencing.WithWriteFence(e, func() error {
			var err error
			ev, wc, err = s.AttachEvidence(q.WorkContextID, EvidenceRef{
				Kind: q.Kind, SourceCapability: q.SourceCapability, SourceID: q.SourceID,
				Outcome: q.Outcome, ContentHash: q.ContentHash,
			}, *q.ExpectedVersion)
			return err
		})
		if err != nil {
			switch {
			case errors.Is(err, ErrNotFound):
				return nil, &protocol.Error{Code: "NOT_FOUND", Message: err.Error()}
			case errors.Is(err, ErrConflict):
				return nil, &protocol.Error{Code: "CONFLICT", Message: err.Error()}
			default:
				return nil, &protocol.Error{Code: "IO", Message: err.Error(), Retryable: true}
			}
		}
		return map[string]any{"evidence_ref": ev, "work_context": wc}, nil
	}
}
