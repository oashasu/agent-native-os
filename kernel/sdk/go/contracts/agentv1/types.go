package agentv1

import agentexecutev1 "github.com/example/agent-native-microkernel/sdk/go/generated/agentexecutev1"

type ExecuteRequest = agentexecutev1.Request
type ExecuteAccepted = agentexecutev1.Response
type Chunk struct {
	Kind          string `json:"kind"`
	Text          string `json:"text"`
	Index         int    `json:"index"`
	TraceID       string `json:"trace_id"`
	CorrelationID string `json:"correlation_id"`
}
