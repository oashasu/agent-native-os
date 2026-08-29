package main

import (
	"os"

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
		panic("workspace load: " + err.Error())
	}
	h := pluginhost.New("org.vibe.workspace", "1.0.0", "")
	h.HandleContextCommand("workspace.allocate", 1, wrap(allocateHandler(s)))
	h.HandleContextCommand("workspace.release", 1, wrap(releaseHandler(s)))
	h.HandleQuery("workspace.get", 1, getHandler(s))
	_ = h.Serve()
}

func wrap(fn pluginhost.Handler) pluginhost.ContextHandler {
	return func(_ *pluginhost.RequestContext, e protocol.Envelope) (any, *protocol.Error) { return fn(e) }
}
