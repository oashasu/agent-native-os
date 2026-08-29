package main

import (
	"github.com/example/agent-native-microkernel/sdk/go/pluginhost"
	"github.com/example/agent-native-microkernel/sdk/go/protocol"
	"os"
)

func main() {
	dir := os.Getenv("VIBE_DATA_DIR")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		panic(err)
	}
	s, err := Load(dir)
	if err != nil {
		panic("review load: " + err.Error())
	}
	h := pluginhost.New("org.vibe.review", "1.0.0", "")
	h.HandleContextCommand("review.request", 1, wrap(requestHandler(s)))
	h.HandleContextCommand("review.decide", 1, wrap(decideHandler(s)))
	h.HandleQuery("review.get", 1, getHandler(s))
	h.HandleQuery("review.query", 1, queryHandler(s))
	if err := h.Serve(); err != nil {
		panic(err)
	}
}
func wrap(fn pluginhost.Handler) pluginhost.ContextHandler {
	return func(_ *pluginhost.RequestContext, e protocol.Envelope) (any, *protocol.Error) { return fn(e) }
}
