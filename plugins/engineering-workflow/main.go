package main

import (
	"github.com/example/agent-native-microkernel/sdk/go/pluginhost"
	"github.com/example/agent-native-microkernel/sdk/go/protocol"
)

func main() {
	h := pluginhost.New("org.vibe.workflow.engineering", "1.0.0", "")
	h.HandleContextCommand("workflow.engineering.run", 1, runHandler(realCaps))
	h.HandleContextQuery("workflow.engineering.get", 1, func(rc *pluginhost.RequestContext, e protocol.Envelope) (any, *protocol.Error) {
		return getHandler(func() ([]JournalRecord, error) { return replayVia(rc) })(e)
	})
	if err := h.Serve(); err != nil {
		panic(err)
	}
}
