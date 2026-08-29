package main

import (
	"encoding/json"
	"fmt"
	"github.com/example/agent-native-microkernel/sdk/go/contracts/agentv1"
	"github.com/example/agent-native-microkernel/sdk/go/contracts/eventv1"
	"github.com/example/agent-native-microkernel/sdk/go/contracts/workflowv1"
	"github.com/example/agent-native-microkernel/sdk/go/contracts/workv1"
	"github.com/example/agent-native-microkernel/sdk/go/pluginhost"
	"github.com/example/agent-native-microkernel/sdk/go/protocol"
	"time"
)

func main() {
	h := pluginhost.New("org.vibe.workflow.demo", "0.9.0", "")
	h.HandleContextCommand("workflow.demo.run", 1, func(rc *pluginhost.RequestContext, e protocol.Envelope) (any, *protocol.Error) {
		var q workflowv1.RunRequest
		if json.Unmarshal(e.Payload, &q) != nil {
			return nil, &protocol.Error{Code: "INVALID", Message: "bad workflow request"}
		}
		create, err := rc.Command("work.create", 1, workv1.CreateRequest{ID: q.WorkID, Title: q.Title}, 3*time.Second)
		if err != nil {
			return nil, &protocol.Error{Code: "WORK_CREATE", Message: err.Error()}
		}
		_ = create
		_, _ = rc.Command("event.journal.append", 1, eventv1.AppendRequest{Type: "workflow.started", Source: "org.vibe.workflow.demo", Payload: protocol.NewPayload(map[string]string{"work_id": q.WorkID})}, 3*time.Second)
		resp, err := rc.Command("agent.execute", 1, agentv1.ExecuteRequest{Prompt: q.Prompt, Steps: 4, DelayMS: 25}, 3*time.Second)
		if err != nil {
			return nil, &protocol.Error{Code: "AGENT", Message: err.Error()}
		}
		var accepted agentv1.ExecuteAccepted
		_ = json.Unmarshal(resp.Payload, &accepted)
		_, _ = rc.Command("event.journal.append", 1, eventv1.AppendRequest{Type: "agent.accepted", Source: "org.vibe.workflow.demo", Payload: protocol.NewPayload(accepted)}, 3*time.Second)
		get, err := rc.Query("work.get", 1, workv1.GetRequest{ID: q.WorkID}, 3*time.Second)
		if err != nil {
			return nil, &protocol.Error{Code: "WORK_GET", Message: err.Error()}
		}
		var gr workv1.GetResponse
		_ = json.Unmarshal(get.Payload, &gr)
		_, err = rc.Command("work.transition", 1, workv1.TransitionRequest{ID: q.WorkID, To: "DONE", ExpectedVersion: gr.Work.Version}, 3*time.Second)
		if err != nil {
			return nil, &protocol.Error{Code: "WORK_TRANSITION", Message: err.Error()}
		}
		_, _ = rc.Command("event.journal.append", 1, eventv1.AppendRequest{Type: "workflow.completed", Source: "org.vibe.workflow.demo", Payload: protocol.NewPayload(map[string]string{"work_id": q.WorkID})}, 3*time.Second)
		return workflowv1.RunResponse{WorkID: q.WorkID, Status: "DONE", TraceID: e.TraceID, CorrelationID: e.CorrelationID, Journaled: 3}, nil
	})
	_ = fmt.Sprint
	_ = h.Serve()
}
