package main

import (
	"encoding/json"

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
