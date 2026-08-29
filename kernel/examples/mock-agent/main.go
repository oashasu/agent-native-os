package main

import (
	"encoding/json"
	"fmt"
	"github.com/example/agent-native-microkernel/sdk/go/contracts/agentv1"
	"github.com/example/agent-native-microkernel/sdk/go/pluginhost"
	"github.com/example/agent-native-microkernel/sdk/go/protocol"
	"os"
	"path/filepath"
	"time"
)

func main() {
	h := pluginhost.New("org.vibe.agent.mock", "0.9.0", "")
	h.HandleContextCommand("agent.execute", 1, func(rc *pluginhost.RequestContext, e protocol.Envelope) (any, *protocol.Error) {
		var q agentv1.ExecuteRequest
		if json.Unmarshal(e.Payload, &q) != nil {
			return nil, &protocol.Error{Code: "INVALID", Message: "bad request"}
		}
		if q.Steps <= 0 {
			q.Steps = 5
		}
		if q.DelayMS <= 0 {
			q.DelayMS = 50
		}
		ch := make(chan any)
		go func() {
			defer close(ch)
			for i := 1; i <= q.Steps; i++ {
				select {
				case <-rc.Context().Done():
					if q.CancelMarker != "" {
						dir := os.Getenv("VIBE_DATA_DIR")
						_ = os.MkdirAll(dir, 0755)
						_ = os.WriteFile(filepath.Join(dir, q.CancelMarker), []byte("cancelled"), 0644)
					}
					return
				case <-time.After(time.Duration(q.DelayMS) * time.Millisecond):
					ch <- agentv1.Chunk{Kind: "stdout", Text: fmt.Sprintf("step-%d:%s", i, q.Prompt), Index: i, TraceID: e.TraceID, CorrelationID: e.CorrelationID}
				}
			}
		}()
		acc := rc.Stream(ch)
		return agentv1.ExecuteAccepted{StreamID: acc.StreamID, TraceID: e.TraceID, CorrelationID: e.CorrelationID}, nil
	})
	_ = h.Serve()
}
