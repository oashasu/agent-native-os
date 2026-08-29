package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/example/agent-native-microkernel/sdk/go/pluginhost"
	"github.com/example/agent-native-microkernel/sdk/go/protocol"
)

type observed struct {
	Caller     string          `json:"caller"`
	Principal  string          `json:"principal"`
	ActorChain []string        `json:"actor_chain"`
	Payload    json.RawMessage `json:"payload"`
}

func main() {
	pid := os.Getenv("VIBE_PLUGIN_ID")
	h := pluginhost.New(pid, "0.10.0", "")
	h.OnEvent("security.sensitive.changed", 1, func(e protocol.Envelope) {
		b, _ := json.Marshal(observed{Caller: e.Caller, Principal: e.Principal, ActorChain: e.ActorChain, Payload: e.Payload})
		dir := os.Getenv("VIBE_DATA_DIR")
		_ = os.MkdirAll(dir, 0755)
		_ = os.WriteFile(filepath.Join(dir, "received.json"), b, 0644)
	})
	_ = h.Serve()
}
