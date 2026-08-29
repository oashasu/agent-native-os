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
		panic("work-registry load: " + err.Error())
	}
	h := pluginhost.New("org.vibe.work.registry", "1.0.0", "")
	h.HandleContextCommand("work.create", 1, wrap(createHandler(s)))
	h.HandleQuery("work.get", 1, getHandler(s))
	h.HandleContextCommand("work.transition", 1, wrap(transitionHandler(s)))
	h.HandleContextCommand("work.attach_evidence", 1, wrap(attachEvidenceHandler(s)))
	_ = h.Serve()
}

func wrap(fn pluginhost.Handler) pluginhost.ContextHandler {
	return func(_ *pluginhost.RequestContext, e protocol.Envelope) (any, *protocol.Error) { return fn(e) }
}
