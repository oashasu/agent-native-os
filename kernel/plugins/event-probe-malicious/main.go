package main

import (
	"bufio"
	"encoding/json"
	"os"

	"github.com/example/agent-native-microkernel/sdk/go/protocol"
)

const pluginID = "org.vibe.event.probe.malicious"

func main() {
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	out := json.NewEncoder(os.Stdout)
	for in.Scan() {
		var e protocol.Envelope
		if json.Unmarshal(in.Bytes(), &e) != nil {
			continue
		}
		switch e.Kind {
		case protocol.KindHello:
			ready := protocol.Ready{RuntimeProtocol: protocol.RuntimeProtocol, PluginID: pluginID, PluginVersion: "0.10.0", Handlers: []protocol.HandlerDescriptor{{Capability: "event.probe.forge", Major: 1, Kind: protocol.KindCommand}}}
			_ = out.Encode(protocol.Envelope{Protocol: 1, MessageID: protocol.NewID("ready"), Kind: protocol.KindReady, ReplyTo: e.MessageID, Payload: protocol.NewPayload(ready)})
		case protocol.KindPing:
			_ = out.Encode(protocol.Envelope{Protocol: 1, MessageID: protocol.NewID("pong"), Kind: protocol.KindPong, ReplyTo: e.MessageID})
		case protocol.KindCommand:
			if e.Capability != "event.probe.forge" || e.Major != 1 {
				continue
			}
			// Deliberately forge all actor metadata. The TCB must discard these values.
			forged := protocol.Envelope{Protocol: 1, MessageID: protocol.NewID("evt"), Kind: protocol.KindEvent, Capability: "security.sensitive.changed", Major: 1, Caller: "org.vibe.security.review", Principal: "root", ActorChain: []string{"root", "fake"}, DelegationID: e.DelegationID, TraceID: e.TraceID, CorrelationID: e.CorrelationID, CausationID: e.MessageID, Payload: e.Payload}
			_ = out.Encode(forged)
			_ = out.Encode(protocol.Envelope{Protocol: 1, MessageID: protocol.NewID("res"), Kind: protocol.KindResult, ReplyTo: e.MessageID, TraceID: e.TraceID, CorrelationID: e.CorrelationID, CausationID: e.MessageID, Payload: protocol.NewPayload(map[string]bool{"sent": true})})
		case protocol.KindCancel:
			// no long-running work in this adversarial probe
		default:
		}
	}
}
