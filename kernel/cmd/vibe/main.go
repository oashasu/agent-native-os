package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/example/agent-native-microkernel/internal/clientgateway"
	"github.com/example/agent-native-microkernel/sdk/go/protocol"
	"log"
	"os"
	"time"
)

func main() {
	socket := flag.String("socket", "/tmp/vibe-kernel.sock", "kernel socket")
	identity := flag.String("identity", "local-cli", "client identity")
	token := flag.String("token", os.Getenv("VIBE_CLIENT_TOKEN"), "client authentication token (or VIBE_CLIENT_TOKEN)")
	cap := flag.String("cap", "", "capability")
	major := flag.Int("major", 1, "major version")
	kind := flag.String("kind", "query", "query|command")
	payload := flag.String("payload", "{}", "JSON payload")
	service := flag.String("service", "", "logical stateful service")
	authority := flag.String("authority", "", "state authority")
	provider := flag.String("provider", "", "explicit provider hint")
	idem := flag.String("idempotency", "", "idempotency key")
	timeout := flag.Duration("timeout", 10*time.Second, "deadline")
	stream := flag.Bool("stream", false, "consume external stream frames")
	cancelAfter := flag.Int("cancel-after", 0, "cancel a live stream after N data frames")
	flag.Parse()
	if *token == "" {
		log.Fatal("-token or VIBE_CLIENT_TOKEN required")
	}
	if *cap == "" {
		log.Fatal("-cap required")
	}
	var raw json.RawMessage
	if err := json.Unmarshal([]byte(*payload), &raw); err != nil {
		log.Fatal(err)
	}
	k := protocol.KindQuery
	if *kind == "command" {
		k = protocol.KindCommand
	}
	req := protocol.Envelope{Protocol: 1, MessageID: protocol.NewID("cli"), Kind: k, Capability: *cap, Major: *major, Service: *service, Authority: *authority, ProviderHint: *provider, IdempotencyKey: *idem, Deadline: time.Now().Add(*timeout).Format(time.RFC3339Nano), Payload: raw}
	if *stream {
		req.StreamID = protocol.NewID("stream")
		resp, live, err := clientgateway.DialOpenStream(*socket, *identity, *token, req)
		if err != nil {
			log.Fatal(err)
		}
		defer live.Close()
		fmt.Println(string(resp.Payload))
		dataFrames := 0
		cancelRequested := false
		for {
			f, err := live.Next()
			if err != nil {
				log.Fatal(err)
			}
			if f.Kind == protocol.KindStreamData {
				dataFrames++
				if *cancelAfter > 0 && dataFrames >= *cancelAfter && !cancelRequested {
					if err := live.Cancel(); err != nil {
						log.Fatal(err)
					}
					cancelRequested = true
				}
			}
			if f.Error != nil {
				fmt.Fprintf(os.Stderr, "%s %s: %s\n", f.Kind, f.Error.Code, f.Error.Message)
				if f.Kind == protocol.KindStreamClose && !(cancelRequested && f.Error.Code == "CANCELLED") {
					os.Exit(2)
				}
			} else {
				fmt.Printf("%s %s\n", f.Kind, string(f.Payload))
			}
			if f.Kind == protocol.KindStreamClose {
				break
			}
		}
		return
	}
	resp, err := clientgateway.DialInvoke(*socket, *identity, *token, req)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(resp.Payload))
}
