package main

import (
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/example/agent-native-microkernel/sdk/go/protocol"
)

type wireRequest struct {
	Identity string            `json:"identity"`
	Token    string            `json:"token"`
	Envelope protocol.Envelope `json:"envelope"`
}

func invoke(socket, identity, token string, req protocol.Envelope) (protocol.Envelope, error) {
	c, err := net.DialTimeout("unix", socket, 5*time.Second)
	if err != nil {
		return protocol.Envelope{}, fmt.Errorf("dial %s: %w", socket, err)
	}
	defer c.Close()

	req.Caller = ""
	req.Principal = ""
	req.ActorChain = nil
	req.DelegationID = ""
	if req.Protocol == 0 {
		req.Protocol = 1
	}
	if req.MessageID == "" {
		req.MessageID = protocol.NewID("cli")
	}
	if req.TraceID == "" {
		req.TraceID = protocol.NewID("trace")
	}
	if req.CorrelationID == "" {
		req.CorrelationID = protocol.NewID("corr")
	}
	if err := json.NewEncoder(c).Encode(wireRequest{Identity: identity, Token: token, Envelope: req}); err != nil {
		return protocol.Envelope{}, err
	}
	var resp protocol.Envelope
	if err := json.NewDecoder(c).Decode(&resp); err != nil {
		return protocol.Envelope{}, err
	}
	if resp.Kind == protocol.KindError && resp.Error != nil {
		return resp, fmt.Errorf("%s: %s", resp.Error.Code, resp.Error.Message)
	}
	return resp, nil
}
