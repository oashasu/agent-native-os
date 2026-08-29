// Package main is a copy-me template for an agent-native-os plugin.
//
// To make a new plugin:
//  1. cp -r plugins/_template plugins/<name>            (or plugins/foundation/<name>)
//  2. rename the package stays "main"; change the plugin id, version, capability.
//  3. write contracts/<capability>/v1/schema.json and add it to contracts/catalog.json.
//  4. write plugins/manifests/<name>.manifest.json (see manifest.json.tmpl).
//  5. keep business logic in constructor-returned handlers (func(env) (any, *Error))
//     so it is unit-testable without a running kernel — see main_test.go.
//
// A plugin talks to the kernel ONLY through versioned contracts. It never
// imports another plugin or anything under kernel/internal.
package main

import (
	"encoding/json"

	"github.com/example/agent-native-microkernel/sdk/go/pluginhost"
	"github.com/example/agent-native-microkernel/sdk/go/protocol"
)

type echoRequest struct {
	Message string `json:"message"`
}

type echoResponse struct {
	Echo string `json:"echo"`
}

// echoHandler is the unit under test. Real plugins put their domain logic here,
// often as a closure over a store handle: func newXHandler(s *store) pluginhost.Handler.
func echoHandler(e protocol.Envelope) (any, *protocol.Error) {
	var q echoRequest
	if json.Unmarshal(e.Payload, &q) != nil || q.Message == "" {
		return nil, &protocol.Error{Code: "INVALID", Message: "message is required"}
	}
	return echoResponse{Echo: q.Message}, nil
}

func main() {
	h := pluginhost.New("org.vibe.template", "0.0.0", "")
	// Use HandleContextCommand for kind=command, HandleQuery for kind=query.
	// A stateful command handler wraps its write in fencing.WithWriteFence(e, ...).
	h.HandleQuery("template.echo", 1, echoHandler)
	_ = h.Serve()
}
