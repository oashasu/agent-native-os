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
		panic("artifact load: " + err.Error())
	}
	h := pluginhost.New("org.vibe.artifact", "1.0.0", "")
	h.HandleContextCommand("artifact.collect_diff", 1, func(rc *pluginhost.RequestContext, e protocol.Envelope) (any, *protocol.Error) {
		return collectDiffHandler(s, func(b []byte) (string, error) {
			resp, cerr := rc.Command("blob.put", 1, map[string]string{"content_base64": base64.StdEncoding.EncodeToString(b)}, 30*time.Second)
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
		})(e)
	})
	h.HandleQuery("artifact.get", 1, getHandler(s))
	h.HandleQuery("artifact.query", 1, queryHandler(s))
	if err := h.Serve(); err != nil {
		panic(err)
	}
}
