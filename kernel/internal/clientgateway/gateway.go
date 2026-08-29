package clientgateway

import (
	"bufio"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"github.com/example/agent-native-microkernel/internal/router"
	"github.com/example/agent-native-microkernel/sdk/go/protocol"
	"net"
	"os"
	"sync"
)

type Gateway struct {
	path             string
	rt               *router.Router
	credentialHashes map[string]string
	ln               net.Listener
	mu               sync.Mutex
}

type WireRequest struct {
	Identity string            `json:"identity"`
	Token    string            `json:"token"`
	Envelope protocol.Envelope `json:"envelope"`
}

func New(path string, rt *router.Router, credentialHashes map[string]string) *Gateway {
	copied := make(map[string]string, len(credentialHashes))
	for k, v := range credentialHashes {
		copied[k] = v
	}
	return &Gateway{path: path, rt: rt, credentialHashes: copied}
}
func (g *Gateway) Start() error {
	_ = os.Remove(g.path)
	ln, err := net.Listen("unix", g.path)
	if err != nil {
		return err
	}
	g.ln = ln
	go g.accept()
	return nil
}
func (g *Gateway) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.ln != nil {
		return g.ln.Close()
	}
	return nil
}
func (g *Gateway) accept() {
	for {
		c, err := g.ln.Accept()
		if err != nil {
			return
		}
		go g.serve(c)
	}
}
func (g *Gateway) authenticate(identity, token string) bool {
	expectedHash, ok := g.credentialHashes[identity]
	if !ok || expectedHash == "" || token == "" {
		return false
	}
	sum := sha256.Sum256([]byte(token))
	actualHash := fmt.Sprintf("%x", sum[:])
	return subtle.ConstantTimeCompare([]byte(expectedHash), []byte(actualHash)) == 1
}
func (g *Gateway) serve(c net.Conn) {
	defer c.Close()
	s := bufio.NewScanner(c)
	s.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	enc := json.NewEncoder(c)
	for s.Scan() {
		var wire WireRequest
		if err := json.Unmarshal(s.Bytes(), &wire); err != nil {
			_ = enc.Encode(protocol.Envelope{Protocol: 1, MessageID: protocol.NewID("err"), Kind: protocol.KindError, Error: &protocol.Error{Code: "BAD_REQUEST", Message: err.Error()}})
			continue
		}
		if !g.authenticate(wire.Identity, wire.Token) {
			_ = enc.Encode(protocol.Envelope{Protocol: 1, MessageID: protocol.NewID("err"), Kind: protocol.KindError, ReplyTo: wire.Envelope.MessageID, Error: &protocol.Error{Code: "UNAUTHENTICATED", Message: "invalid client credentials"}})
			continue
		}
		req := wire.Envelope
		// Caller is host-derived. Never trust a client-supplied caller value inside the envelope.
		req.Caller = wire.Identity
		if req.Kind == protocol.KindCancel {
			cancelled := false
			if req.StreamID != "" {
				cancelled = g.rt.CancelExternalStream(wire.Identity, req.StreamID)
			} else if req.ReplyTo != "" {
				cancelled = g.rt.CancelExternalRequest(wire.Identity, req.ReplyTo)
			}
			code := "CANCELLED"
			if !cancelled {
				code = "CANCEL_NOT_FOUND"
			}
			_ = enc.Encode(protocol.Envelope{Protocol: 1, MessageID: protocol.NewID("cancel-result"), Kind: protocol.KindResult, ReplyTo: req.MessageID, Payload: protocol.NewPayload(map[string]any{"cancelled": cancelled, "code": code})})
			continue
		}
		if req.StreamID != "" {
			resp, framesCh, err := g.rt.InvokeExternalStream(wire.Identity, req)
			if err != nil {
				_ = enc.Encode(protocol.Envelope{Protocol: 1, MessageID: protocol.NewID("err"), Kind: protocol.KindError, ReplyTo: req.MessageID, TraceID: req.TraceID, CorrelationID: req.CorrelationID, Error: &protocol.Error{Code: "INVOKE_ERROR", Message: err.Error()}})
				continue
			}
			resp.MessageID = protocol.NewID("reply")
			resp.ReplyTo = req.MessageID
			if err := enc.Encode(resp); err != nil {
				return
			}
			for f := range framesCh {
				if err := enc.Encode(f); err != nil {
					g.rt.CancelExternalStream(wire.Identity, req.StreamID)
					return
				}
			}
			continue
		}
		resp, err := g.rt.InvokeExternal(wire.Identity, req)
		if err != nil {
			_ = enc.Encode(protocol.Envelope{Protocol: 1, MessageID: protocol.NewID("err"), Kind: protocol.KindError, ReplyTo: req.MessageID, TraceID: req.TraceID, CorrelationID: req.CorrelationID, Error: &protocol.Error{Code: "INVOKE_ERROR", Message: err.Error()}})
			continue
		}
		resp.MessageID = protocol.NewID("reply")
		resp.ReplyTo = req.MessageID
		if err := enc.Encode(resp); err != nil {
			return
		}
	}
}

func prepare(req *protocol.Envelope) {
	// The gateway, not the external client, owns Caller identity.
	req.Caller = ""
	req.Principal = ""
	req.ActorChain = nil
	if req.MessageID == "" {
		req.MessageID = protocol.NewID("client")
	}
	if req.Protocol == 0 {
		req.Protocol = 1
	}
	if req.TraceID == "" {
		req.TraceID = protocol.NewID("trace")
	}
	if req.CorrelationID == "" {
		req.CorrelationID = protocol.NewID("corr")
	}
}
func DialInvoke(socket, identity, token string, req protocol.Envelope) (protocol.Envelope, error) {
	c, err := net.Dial("unix", socket)
	if err != nil {
		return protocol.Envelope{}, err
	}
	defer c.Close()
	prepare(&req)
	if err := json.NewEncoder(c).Encode(WireRequest{Identity: identity, Token: token, Envelope: req}); err != nil {
		return protocol.Envelope{}, err
	}
	var resp protocol.Envelope
	if err := json.NewDecoder(c).Decode(&resp); err != nil {
		return protocol.Envelope{}, err
	}
	if resp.Kind == protocol.KindError && resp.Error != nil {
		return resp, protocol.NewRemoteError(resp.Error)
	}
	return resp, nil
}

type ClientStream struct {
	conn      net.Conn
	dec       *json.Decoder
	socket    string
	identity  string
	token     string
	RequestID string
	StreamID  string
}

func DialOpenStream(socket, identity, token string, req protocol.Envelope) (protocol.Envelope, *ClientStream, error) {
	c, err := net.Dial("unix", socket)
	if err != nil {
		return protocol.Envelope{}, nil, err
	}
	prepare(&req)
	if req.StreamID == "" {
		req.StreamID = protocol.NewID("stream")
	}
	if err := json.NewEncoder(c).Encode(WireRequest{Identity: identity, Token: token, Envelope: req}); err != nil {
		c.Close()
		return protocol.Envelope{}, nil, err
	}
	dec := json.NewDecoder(c)
	var resp protocol.Envelope
	if err := dec.Decode(&resp); err != nil {
		c.Close()
		return protocol.Envelope{}, nil, err
	}
	if resp.Kind == protocol.KindError && resp.Error != nil {
		c.Close()
		return resp, nil, protocol.NewRemoteError(resp.Error)
	}
	return resp, &ClientStream{conn: c, dec: dec, socket: socket, identity: identity, token: token, RequestID: req.MessageID, StreamID: req.StreamID}, nil
}
func (s *ClientStream) Next() (protocol.Envelope, error) {
	var f protocol.Envelope
	if err := s.dec.Decode(&f); err != nil {
		return protocol.Envelope{}, err
	}
	return f, nil
}
func (s *ClientStream) Cancel() error {
	_, err := DialCancel(s.socket, s.identity, s.token, protocol.Envelope{Protocol: 1, MessageID: protocol.NewID("cancel"), Kind: protocol.KindCancel, StreamID: s.StreamID, ReplyTo: s.RequestID})
	return err
}
func (s *ClientStream) Close() error { return s.conn.Close() }
func DialCancel(socket, identity, token string, req protocol.Envelope) (protocol.Envelope, error) {
	c, err := net.Dial("unix", socket)
	if err != nil {
		return protocol.Envelope{}, err
	}
	defer c.Close()
	prepare(&req)
	req.Kind = protocol.KindCancel
	if err := json.NewEncoder(c).Encode(WireRequest{Identity: identity, Token: token, Envelope: req}); err != nil {
		return protocol.Envelope{}, err
	}
	var resp protocol.Envelope
	if err := json.NewDecoder(c).Decode(&resp); err != nil {
		return protocol.Envelope{}, err
	}
	return resp, nil
}
func DialInvokeStream(socket, identity, token string, req protocol.Envelope) (protocol.Envelope, []protocol.Envelope, error) {
	resp, stream, err := DialOpenStream(socket, identity, token, req)
	if err != nil {
		return resp, nil, err
	}
	defer stream.Close()
	frames := []protocol.Envelope{}
	for {
		f, err := stream.Next()
		if err != nil {
			return resp, frames, err
		}
		frames = append(frames, f)
		if f.Kind == protocol.KindStreamClose {
			return resp, frames, nil
		}
	}
}
