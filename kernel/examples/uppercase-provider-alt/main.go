package main

import (
	"encoding/json"
	"strings"

	"github.com/example/agent-native-microkernel/sdk/go/pluginhost"
	"github.com/example/agent-native-microkernel/sdk/go/protocol"
)

type Request struct {
	Text string `json:"text"`
}
type Response struct {
	Text string `json:"text"`
}

func main() {
	h := pluginhost.New("org.vibe.demo.uppercase.alt", "1.0.0", "")
	h.Handle("demo.uppercase", 1, func(e protocol.Envelope) (any, *protocol.Error) {
		var r Request
		if err := json.Unmarshal(e.Payload, &r); err != nil {
			return nil, &protocol.Error{Code: "INVALID_REQUEST", Message: err.Error()}
		}
		return Response{Text: "[ALT]" + strings.ToUpper(r.Text)}, nil
	})
	if err := h.Serve(); err != nil {
		panic(err)
	}
}
