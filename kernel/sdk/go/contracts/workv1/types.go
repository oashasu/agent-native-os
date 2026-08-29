package workv1

import (
	workcreatev1 "github.com/example/agent-native-microkernel/sdk/go/generated/workcreatev1"
	workgetv1 "github.com/example/agent-native-microkernel/sdk/go/generated/workgetv1"
	worktransitionv1 "github.com/example/agent-native-microkernel/sdk/go/generated/worktransitionv1"
)

type Work = workcreatev1.ResponseWork
type CreateRequest = workcreatev1.Request
type CreateResponse = workcreatev1.Response
type GetRequest = workgetv1.Request
type TransitionRequest = worktransitionv1.Request

type GetResponse struct {
	Work Work `json:"work"`
}
type TransitionResponse struct {
	Work Work `json:"work"`
}

// Compile-time structural checks ensure the compatibility wrappers cannot drift
// from schema-generated response types.
var _ = Work(workgetv1.ResponseWork{})
var _ = Work(worktransitionv1.ResponseWork{})
var _ = workgetv1.Response{Work: workgetv1.ResponseWork(Work{})}
var _ = worktransitionv1.Response{Work: worktransitionv1.ResponseWork(Work{})}
