package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/example/agent-native-microkernel/sdk/go/fencing"
	fenceprobev1 "github.com/example/agent-native-microkernel/sdk/go/generated/fenceprobewritev1"
	"github.com/example/agent-native-microkernel/sdk/go/pluginhost"
	"github.com/example/agent-native-microkernel/sdk/go/protocol"
)

func main() {
	pid := os.Getenv("VIBE_PLUGIN_ID")
	if pid == "" {
		pid = "org.vibe.fence.probe.primary"
	}
	h := pluginhost.New(pid, "0.10.0", "")
	h.HandleContextCommand("fence.probe.write", 1, func(_ *pluginhost.RequestContext, e protocol.Envelope) (any, *protocol.Error) {
		var q fenceprobev1.Request
		if err := json.Unmarshal(e.Payload, &q); err != nil || q.Marker == "" {
			return nil, &protocol.Error{Code: "BAD_REQUEST", Message: "marker required"}
		}
		// Intentionally ignore request cancellation before attempting the write.
		// This simulates a stale/hung writer that continues after the caller timed out;
		// the host fencing epoch must still prevent it from mutating state.
		if q.DelayMS > 0 {
			time.Sleep(time.Duration(q.DelayMS) * time.Millisecond)
		}
		runtimeID := os.Getenv("VIBE_RUNTIME_ID")
		err := fencing.WithWriteFence(e, func() error {
			dir := os.Getenv("VIBE_DATA_DIR")
			if err := os.MkdirAll(dir, 0755); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(dir, q.Marker), []byte(runtimeID), 0644)
		})
		if err != nil {
			return nil, &protocol.Error{Code: "STALE_FENCE", Message: err.Error(), Retryable: false}
		}
		return fenceprobev1.Response{Committed: true, RuntimeID: runtimeID, FencingEpoch: int(e.FencingEpoch)}, nil
	})
	_ = h.Serve()
}
