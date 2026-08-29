package main

import (
	"encoding/json"

	emitv1 "github.com/example/agent-native-microkernel/sdk/go/generated/eventprobeemitv1"
	"github.com/example/agent-native-microkernel/sdk/go/pluginhost"
	"github.com/example/agent-native-microkernel/sdk/go/protocol"
)

func main() {
	h := pluginhost.New("org.vibe.event.probe.publisher", "0.10.0", "")
	h.HandleContextCommand("event.probe.emit", 1, func(rc *pluginhost.RequestContext, e protocol.Envelope) (any, *protocol.Error) {
		var q emitv1.Request
		if err := json.Unmarshal(e.Payload, &q); err != nil || q.Value == "" {
			return nil, &protocol.Error{Code: "BAD_REQUEST", Message: "value required"}
		}
		if err := rc.Publish("security.sensitive.changed", 1, map[string]string{"value": q.Value}); err != nil {
			return nil, &protocol.Error{Code: "PUBLISH_FAILED", Message: err.Error()}
		}
		return emitv1.Response{Published: true}, nil
	})
	_ = h.Serve()
}
