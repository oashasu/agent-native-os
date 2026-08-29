package main

import (
    "encoding/json"
    "os"
    "path/filepath"
    "time"

    "github.com/example/agent-native-microkernel/sdk/go/pluginhost"
    "github.com/example/agent-native-microkernel/sdk/go/protocol"
)

type request struct { DelayMS int `json:"delay_ms"`; Marker string `json:"marker"` }
type response struct { Committed bool `json:"committed"` }

func main() {
    h := pluginhost.New("org.vibe.cancel.probe", "0.10.0", "")
    h.HandleContextCommand("cancel.probe", 1, func(rc *pluginhost.RequestContext, e protocol.Envelope) (any, *protocol.Error) {
        var q request
        if err := json.Unmarshal(e.Payload, &q); err != nil { return nil, &protocol.Error{Code:"BAD_REQUEST",Message:err.Error()} }
        timer := time.NewTimer(time.Duration(q.DelayMS)*time.Millisecond)
        defer timer.Stop()
        select {
        case <-rc.Context().Done():
            return nil, &protocol.Error{Code:"CANCELLED",Message:"request cancelled",Retryable:false}
        case <-timer.C:
        }
        dir := os.Getenv("VIBE_DATA_DIR")
        if err := os.MkdirAll(dir,0755); err != nil { return nil,&protocol.Error{Code:"IO_ERROR",Message:err.Error(),Retryable:true} }
        if err := os.WriteFile(filepath.Join(dir,q.Marker),[]byte("committed"),0644); err != nil { return nil,&protocol.Error{Code:"IO_ERROR",Message:err.Error(),Retryable:true} }
        return response{Committed:true}, nil
    })
    _ = h.Serve()
}
