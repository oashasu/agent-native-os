package main

import (
	"encoding/json"
	"time"

	"github.com/example/agent-native-microkernel/sdk/go/pluginhost"
	"github.com/example/agent-native-microkernel/sdk/go/protocol"
)

type Input struct {
	Text string `json:"text"`
}
type Output struct {
	Text string `json:"text"`
	Via  string `json:"via"`
}

func main() {
	h := pluginhost.New("org.vibe.demo.consumer", "1.0.0", "")
	h.Handle("demo.consume", 1, func(e protocol.Envelope) (any, *protocol.Error) {
		var in Input
		if err := json.Unmarshal(e.Payload, &in); err != nil {
			return nil, &protocol.Error{Code: "INVALID_REQUEST", Message: err.Error()}
		}
		resp, err := h.Query("demo.uppercase", 1, in, 5*time.Second)
		if err != nil {
			return nil, &protocol.Error{Code: "DEPENDENCY_FAILED", Message: err.Error(), Retryable: true}
		}
		var upper struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(resp.Payload, &upper); err != nil {
			return nil, &protocol.Error{Code: "INVALID_DEPENDENCY_RESPONSE", Message: err.Error()}
		}
		return Output{Text: upper.Text, Via: "contract"}, nil
	})
	if err := h.Serve(); err != nil {
		panic(err)
	}
}
