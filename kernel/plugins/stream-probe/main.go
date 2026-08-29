package main

import (
	"encoding/json"
	"github.com/example/agent-native-microkernel/sdk/go/contracts/agentv1"
	"github.com/example/agent-native-microkernel/sdk/go/contracts/streamprobev1"
	"github.com/example/agent-native-microkernel/sdk/go/pluginhost"
	"github.com/example/agent-native-microkernel/sdk/go/protocol"
	"time"
)

func main() {
	h := pluginhost.New("org.vibe.stream.probe", "0.9.0", "")
	h.HandleContextCommand("stream.probe", 1, func(rc *pluginhost.RequestContext, e protocol.Envelope) (any, *protocol.Error) {
		var q streamprobev1.Request
		_ = json.Unmarshal(e.Payload, &q)
		s, accepted, err := rc.CommandStream("agent.execute", 1, agentv1.ExecuteRequest{Prompt: q.Prompt, Steps: 8, DelayMS: 20}, 2*time.Second)
		if err != nil {
			return nil, &protocol.Error{Code: "STREAM_START", Message: err.Error()}
		}
		var a agentv1.ExecuteAccepted
		_ = json.Unmarshal(accepted.Payload, &a)
		count := 0
		cancelled := false
		for frame := range s.C {
			if frame.Kind == protocol.KindStreamData {
				count++
				if q.CancelAfter > 0 && count >= q.CancelAfter && !cancelled {
					_ = s.Cancel()
					cancelled = true
				}
			}
		}
		return streamprobev1.Response{Chunks: count, Cancelled: cancelled, TraceID: a.TraceID, CorrelationID: a.CorrelationID}, nil
	})
	_ = h.Serve()
}
