package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"time"

	"github.com/example/agent-native-microkernel/sdk/go/pluginhost"
	"github.com/example/agent-native-microkernel/sdk/go/protocol"
)

func main() {
	dir := os.Getenv("VIBE_DATA_DIR")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		panic(err)
	}
	s, err := Load(dir)
	if err != nil {
		panic("agent-harness load: " + err.Error())
	}
	h := pluginhost.New("org.vibe.agent.harness", "1.0.0", "")
	deps := runDeps{
		Store:           s,
		Providers:       discoverProviders(candidatesFromEnv(), allowlistFromEnv(), os.Stderr),
		DefaultProvider: "mock",
		Runs:            newRunRegistry(),
		Now:             func() string { return time.Now().UTC().Format(time.RFC3339Nano) },
		BlobPut: func(payload []byte) (string, error) {
			resp, cerr := h.Command("blob.put", 1, map[string]string{"content_base64": b64(payload)}, 30*time.Second)
			if cerr != nil {
				return "", cerr
			}
			var r struct {
				URI string `json:"uri"`
			}
			if err := json.Unmarshal(resp.Payload, &r); err != nil {
				return "", err
			}
			return r.URI, nil
		},
	}
	h.HandleContextCommand("agent.run", 1, agentRunHandler(deps))
	h.HandleQuery("agent.run.get", 1, getHandler(s))
	h.HandleQuery("agent.run.query", 1, queryHandler(s))
	h.HandleContextCommand("agent.run.cancel", 1, wrap(cancelHandler(s)))
	if err := h.Serve(); err != nil {
		panic(err)
	}
}

func wrap(fn pluginhost.Handler) pluginhost.ContextHandler {
	return func(_ *pluginhost.RequestContext, e protocol.Envelope) (any, *protocol.Error) { return fn(e) }
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }
