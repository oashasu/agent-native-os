package main

import "github.com/example/agent-native-microkernel/sdk/go/pluginhost"

func main() {
	h := pluginhost.New("org.vibe.demo.cli", "1.0.0", "")
	if err := h.Serve(); err != nil {
		panic(err)
	}
}
