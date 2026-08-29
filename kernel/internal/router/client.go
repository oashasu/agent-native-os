package router

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/example/agent-native-microkernel/sdk/go/protocol"
	"io"
	"sync"
	"time"
)

var ErrDeadlineExceeded = errors.New("deadline exceeded")

type InboundControl struct {
	Envelope protocol.Envelope
	ack      chan struct{}
}

func (m InboundControl) Ack() {
	if m.ack != nil {
		close(m.ack)
	}
}

const defaultWriteTimeout = 5 * time.Second

type ProcessClient struct {
	runtimeID    string
	pluginID     string
	encMu        sync.Mutex
	enc          *json.Encoder
	writeTimeout time.Duration
	pendingMu    sync.Mutex
	pending      map[string]chan protocol.Envelope
	events       chan InboundControl
	streamEvents chan protocol.Envelope
	closed       chan struct{}
	closeOnce    sync.Once
}

func NewProcessClient(runtimeID, pluginID string, stdin io.Writer, stdout io.Reader) *ProcessClient {
	c := &ProcessClient{runtimeID: runtimeID, pluginID: pluginID, enc: json.NewEncoder(stdin), writeTimeout: defaultWriteTimeout, pending: map[string]chan protocol.Envelope{}, events: make(chan InboundControl, 256), streamEvents: make(chan protocol.Envelope, 256), closed: make(chan struct{})}
	go c.readLoop(stdout)
	return c
}

// SetWriteTimeout bounds how long a single Send may block on a plugin that has
// stopped reading its stdin. Exceeding it disconnects the client.
func (c *ProcessClient) SetWriteTimeout(d time.Duration) { c.writeTimeout = d }

func (c *ProcessClient) markClosed() { c.closeOnce.Do(func() { close(c.closed) }) }

func (c *ProcessClient) readLoop(r io.Reader) {
	defer c.markClosed()
	defer close(c.events)
	defer close(c.streamEvents)
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for s.Scan() {
		var e protocol.Envelope
		if json.Unmarshal(s.Bytes(), &e) != nil {
			continue
		}
		if e.ReplyTo != "" && (e.Kind == protocol.KindResult || e.Kind == protocol.KindError || e.Kind == protocol.KindReady || e.Kind == protocol.KindPong) {
			c.pendingMu.Lock()
			ch := c.pending[e.ReplyTo]
			c.pendingMu.Unlock()
			if ch != nil {
				select {
				case ch <- e:
				case <-c.closed:
				}
				continue
			}
		}
		if e.Kind == protocol.KindStreamOpen || e.Kind == protocol.KindStreamData || e.Kind == protocol.KindStreamClose {
			select {
			case c.streamEvents <- e:
			default:
				// Never let bulk stream data block the control-plane scanner. Fail the
				// stream explicitly when the bounded ingress queue is exhausted.
				closeEnv := protocol.Envelope{Protocol: 1, MessageID: protocol.NewID("sclose"), Kind: protocol.KindStreamClose, StreamID: e.StreamID, TraceID: e.TraceID, CorrelationID: e.CorrelationID, CausationID: e.CausationID, Error: &protocol.Error{Code: "BACKPRESSURE_LIMIT", Message: "stream consumer ingress queue exhausted", Retryable: true}}
				ack := make(chan struct{})
				select {
				case c.events <- InboundControl{Envelope: closeEnv, ack: ack}:
					select {
					case <-ack:
					case <-c.closed:
						return
					}
				case <-c.closed:
					return
				}
			}
			continue
		}
		ack := make(chan struct{})
		select {
		case c.events <- InboundControl{Envelope: e, ack: ack}:
			// Preserve stdout control-plane ordering: an event/child request emitted
			// before a provider result must be admitted before that result can close
			// the request-scoped delegation that authorizes it.
			select {
			case <-ack:
			case <-c.closed:
				return
			}
		case <-c.closed:
			return
		}
	}
}
func (c *ProcessClient) Send(e protocol.Envelope) error {
	select {
	case <-c.closed:
		return fmt.Errorf("provider %s disconnected", c.pluginID)
	default:
	}
	c.encMu.Lock()
	defer c.encMu.Unlock()
	done := make(chan error, 1)
	go func() { done <- c.enc.Encode(e) }()
	timer := time.NewTimer(c.writeTimeout)
	defer timer.Stop()
	select {
	case err := <-done:
		return err
	case <-c.closed:
		return fmt.Errorf("provider %s disconnected", c.pluginID)
	case <-timer.C:
		// The plugin has stopped draining its stdin. Treat it as disconnected so
		// the router stops routing to it; the supervisor will reap the process.
		c.markClosed()
		return fmt.Errorf("write to provider %s timed out after %s", c.pluginID, c.writeTimeout)
	}
}
func (c *ProcessClient) Call(e protocol.Envelope, timeout time.Duration) (protocol.Envelope, error) {
	ch := make(chan protocol.Envelope, 1)
	c.pendingMu.Lock()
	c.pending[e.MessageID] = ch
	c.pendingMu.Unlock()
	defer func() { c.pendingMu.Lock(); delete(c.pending, e.MessageID); c.pendingMu.Unlock() }()
	if err := c.Send(e); err != nil {
		return protocol.Envelope{}, err
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case resp := <-ch:
		return resp, nil
	case <-timer.C:
		return protocol.Envelope{}, fmt.Errorf("%w waiting for %s", ErrDeadlineExceeded, c.pluginID)
	case <-c.closed:
		// The provider may close immediately after writing a final response. Prefer a
		// response already queued for this request over the disconnect signal.
		select {
		case resp := <-ch:
			return resp, nil
		default:
			return protocol.Envelope{}, fmt.Errorf("provider %s disconnected", c.pluginID)
		}
	}
}
func (c *ProcessClient) Ping(timeout time.Duration) error {
	req := protocol.Envelope{Protocol: 1, MessageID: protocol.NewID("ping"), Kind: protocol.KindPing}
	resp, err := c.Call(req, timeout)
	if err != nil {
		return err
	}
	if resp.Kind != protocol.KindPong {
		return fmt.Errorf("expected pong, got %s", resp.Kind)
	}
	return nil
}
func (c *ProcessClient) Events() <-chan InboundControl          { return c.events }
func (c *ProcessClient) StreamEvents() <-chan protocol.Envelope { return c.streamEvents }
