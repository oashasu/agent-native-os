package pluginhost

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"github.com/example/agent-native-microkernel/sdk/go/protocol"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Handler func(protocol.Envelope) (any, *protocol.Error)
type ContextHandler func(*RequestContext, protocol.Envelope) (any, *protocol.Error)
type EventHandler func(protocol.Envelope)

type Host struct {
	PluginID, PluginVersion, ManifestHash string
	handlers                              map[string]ContextHandler
	handlerKinds                          map[string]protocol.Kind
	events                                map[string]EventHandler
	in                                    io.Reader
	out                                   io.Writer
	encMu                                 sync.Mutex
	pendingMu                             sync.Mutex
	pending                               map[string]chan protocol.Envelope
	streamsMu                             sync.Mutex
	streams                               map[string]chan protocol.Envelope
	cancelMu                              sync.Mutex
	cancels                               map[string]context.CancelFunc
}
type RequestContext struct {
	host      *Host
	req       protocol.Envelope
	ctx       context.Context
	cancelKey string
	streaming bool
}
type Stream struct {
	ID            string
	host          *Host
	C             <-chan protocol.Envelope
	TraceID       string
	CorrelationID string
	RequestID     string
}

func New(pluginID, version, manifestHash string) *Host {
	return &Host{PluginID: pluginID, PluginVersion: version, ManifestHash: manifestHash, handlers: map[string]ContextHandler{}, handlerKinds: map[string]protocol.Kind{}, events: map[string]EventHandler{}, in: os.Stdin, out: os.Stdout, pending: map[string]chan protocol.Envelope{}, streams: map[string]chan protocol.Envelope{}, cancels: map[string]context.CancelFunc{}}
}
func key(name string, major int) string { return fmt.Sprintf("%s@%d", name, major) }
func (h *Host) Handle(name string, major int, fn Handler) {
	h.handlers[key(name, major)] = func(_ *RequestContext, e protocol.Envelope) (any, *protocol.Error) { return fn(e) }
}
func (h *Host) HandleContext(name string, major int, fn ContextHandler) {
	h.handlers[key(name, major)] = fn
}
func (h *Host) HandleCommand(name string, major int, fn Handler) {
	h.HandleContextCommand(name, major, func(_ *RequestContext, e protocol.Envelope) (any, *protocol.Error) { return fn(e) })
}
func (h *Host) HandleQuery(name string, major int, fn Handler) {
	h.HandleContextQuery(name, major, func(_ *RequestContext, e protocol.Envelope) (any, *protocol.Error) { return fn(e) })
}
func (h *Host) HandleContextCommand(name string, major int, fn ContextHandler) {
	h.handlers[key(name, major)] = fn
	h.handlerKinds[key(name, major)] = protocol.KindCommand
}
func (h *Host) HandleContextQuery(name string, major int, fn ContextHandler) {
	h.handlers[key(name, major)] = fn
	h.handlerKinds[key(name, major)] = protocol.KindQuery
}
func (h *Host) OnEvent(name string, major int, fn EventHandler) { h.events[key(name, major)] = fn }
func (h *Host) send(e protocol.Envelope) error {
	h.encMu.Lock()
	defer h.encMu.Unlock()
	return json.NewEncoder(h.out).Encode(e)
}

func childEnvelope(parent protocol.Envelope, kind protocol.Kind, cap string, major int, payload any, timeout time.Duration) protocol.Envelope {
	deadline := ""
	if timeout > 0 {
		d := time.Now().Add(timeout)
		if pd, has, _ := protocol.ParseDeadline(parent.Deadline); has && pd.Before(d) {
			d = pd
		}
		deadline = d.Format(time.RFC3339Nano)
	} else {
		deadline = parent.Deadline
	}
	trace := parent.TraceID
	if trace == "" {
		trace = protocol.NewID("trace")
	}
	corr := parent.CorrelationID
	if corr == "" {
		corr = protocol.NewID("corr")
	}
	return protocol.Envelope{Protocol: 1, MessageID: protocol.NewID("req"), Kind: kind, Capability: cap, Major: major, Caller: parent.Caller, TraceID: trace, CorrelationID: corr, CausationID: parent.MessageID, DelegationID: parent.DelegationID, Deadline: deadline, Payload: protocol.NewPayload(payload)}
}
func effectiveTimeout(e protocol.Envelope, requested time.Duration) (time.Duration, error) {
	if requested <= 0 {
		requested = 30 * time.Second
	}
	if dl, has, err := protocol.ParseDeadline(e.Deadline); err != nil {
		return 0, err
	} else if has {
		remaining := time.Until(dl)
		if remaining <= 0 {
			return 0, fmt.Errorf("deadline exceeded")
		}
		if remaining < requested {
			requested = remaining
		}
	}
	return requested, nil
}

func (h *Host) invokeEnvelope(e protocol.Envelope, timeout time.Duration) (protocol.Envelope, error) {
	effective, err := effectiveTimeout(e, timeout)
	if err != nil {
		return protocol.Envelope{}, err
	}
	ch := make(chan protocol.Envelope, 1)
	h.pendingMu.Lock()
	h.pending[e.MessageID] = ch
	h.pendingMu.Unlock()
	defer func() { h.pendingMu.Lock(); delete(h.pending, e.MessageID); h.pendingMu.Unlock() }()
	if err := h.send(e); err != nil {
		return protocol.Envelope{}, err
	}
	timer := time.NewTimer(effective)
	defer timer.Stop()
	select {
	case resp := <-ch:
		if resp.Kind == protocol.KindError && resp.Error != nil {
			return resp, protocol.NewRemoteError(resp.Error)
		}
		return resp, nil
	case <-timer.C:
		_ = h.send(protocol.Envelope{Protocol: 1, MessageID: protocol.NewID("cancel"), Kind: protocol.KindCancel, ReplyTo: e.MessageID})
		return protocol.Envelope{}, fmt.Errorf("deadline exceeded")
	}
}
func (h *Host) Invoke(kind protocol.Kind, capability string, major int, payload any, timeout time.Duration) (protocol.Envelope, error) {
	e := childEnvelope(protocol.Envelope{}, kind, capability, major, payload, timeout)
	e.Caller = h.PluginID
	return h.invokeEnvelope(e, timeout)
}
func (h *Host) Query(capability string, major int, payload any, timeout time.Duration) (protocol.Envelope, error) {
	return h.Invoke(protocol.KindQuery, capability, major, payload, timeout)
}
func (h *Host) Command(capability string, major int, payload any, timeout time.Duration) (protocol.Envelope, error) {
	return h.Invoke(protocol.KindCommand, capability, major, payload, timeout)
}
func (h *Host) Publish(event string, major int, payload any) error {
	return h.send(protocol.Envelope{Protocol: 1, MessageID: protocol.NewID("evt"), Kind: protocol.KindEvent, Capability: event, Major: major, Caller: h.PluginID, TraceID: protocol.NewID("trace"), CorrelationID: protocol.NewID("corr"), Payload: protocol.NewPayload(payload)})
}

func (rc *RequestContext) Context() context.Context   { return rc.ctx }
func (rc *RequestContext) Request() protocol.Envelope { return rc.req }
func (rc *RequestContext) Query(cap string, major int, payload any, timeout time.Duration) (protocol.Envelope, error) {
	e := childEnvelope(rc.req, protocol.KindQuery, cap, major, payload, timeout)
	e.Caller = rc.host.PluginID
	return rc.host.invokeEnvelope(e, timeout)
}
func (rc *RequestContext) Command(cap string, major int, payload any, timeout time.Duration) (protocol.Envelope, error) {
	e := childEnvelope(rc.req, protocol.KindCommand, cap, major, payload, timeout)
	e.Caller = rc.host.PluginID
	return rc.host.invokeEnvelope(e, timeout)
}
func (rc *RequestContext) CommandStream(cap string, major int, payload any, timeout time.Duration) (*Stream, protocol.Envelope, error) {
	sid := protocol.NewID("stream")
	ch := make(chan protocol.Envelope, 64)
	rc.host.streamsMu.Lock()
	rc.host.streams[sid] = ch
	rc.host.streamsMu.Unlock()
	e := childEnvelope(rc.req, protocol.KindCommand, cap, major, payload, timeout)
	e.Caller = rc.host.PluginID
	e.StreamID = sid
	resp, err := rc.host.invokeEnvelope(e, timeout)
	if err != nil {
		rc.host.streamsMu.Lock()
		delete(rc.host.streams, sid)
		rc.host.streamsMu.Unlock()
		close(ch)
		return nil, resp, err
	}
	return &Stream{ID: sid, host: rc.host, C: ch, TraceID: e.TraceID, CorrelationID: e.CorrelationID, RequestID: e.MessageID}, resp, nil
}
func (rc *RequestContext) Publish(event string, major int, payload any) error {
	trace := rc.req.TraceID
	if trace == "" {
		trace = protocol.NewID("trace")
	}
	corr := rc.req.CorrelationID
	if corr == "" {
		corr = protocol.NewID("corr")
	}
	return rc.host.send(protocol.Envelope{Protocol: 1, MessageID: protocol.NewID("evt"), Kind: protocol.KindEvent, Capability: event, Major: major, Caller: rc.host.PluginID, TraceID: trace, CorrelationID: corr, CausationID: rc.req.MessageID, DelegationID: rc.req.DelegationID, Payload: protocol.NewPayload(payload)})
}
func (h *Host) finishCancel(key string) {
	h.cancelMu.Lock()
	cancel := h.cancels[key]
	delete(h.cancels, key)
	h.cancelMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (rc *RequestContext) Stream(data <-chan any) protocol.StreamAccepted {
	rc.streaming = true
	sid := rc.req.StreamID
	if sid == "" {
		sid = protocol.NewID("stream")
	}
	target := rc.req.ReplyRuntime
	go func() {
		defer rc.host.finishCancel(rc.cancelKey)
		_ = rc.host.send(protocol.Envelope{Protocol: 1, MessageID: protocol.NewID("sopen"), Kind: protocol.KindStreamOpen, StreamID: sid, TargetRuntime: target, TraceID: rc.req.TraceID, CorrelationID: rc.req.CorrelationID, CausationID: rc.req.MessageID})
		var seq int64
		for {
			select {
			case <-rc.ctx.Done():
				_ = rc.host.send(protocol.Envelope{Protocol: 1, MessageID: protocol.NewID("sclose"), Kind: protocol.KindStreamClose, StreamID: sid, TargetRuntime: target, TraceID: rc.req.TraceID, CorrelationID: rc.req.CorrelationID, CausationID: rc.req.MessageID, Error: &protocol.Error{Code: "CANCELLED", Message: "stream cancelled"}})
				return
			case v, ok := <-data:
				if !ok {
					_ = rc.host.send(protocol.Envelope{Protocol: 1, MessageID: protocol.NewID("sclose"), Kind: protocol.KindStreamClose, StreamID: sid, TargetRuntime: target, TraceID: rc.req.TraceID, CorrelationID: rc.req.CorrelationID, CausationID: rc.req.MessageID})
					return
				}
				seq++
				_ = rc.host.send(protocol.Envelope{Protocol: 1, MessageID: protocol.NewID("sdata"), Kind: protocol.KindStreamData, StreamID: sid, TargetRuntime: target, TraceID: rc.req.TraceID, CorrelationID: rc.req.CorrelationID, CausationID: rc.req.MessageID, DelegationID: rc.req.DelegationID, Payload: protocol.NewPayload(protocol.StreamFrame{Seq: seq, Data: protocol.NewPayload(v)})})
			}
		}
	}()
	return protocol.StreamAccepted{StreamID: sid}
}

func (h *Host) CommandStream(cap string, major int, payload any, timeout time.Duration) (*Stream, protocol.Envelope, error) {
	sid := protocol.NewID("stream")
	ch := make(chan protocol.Envelope, 64)
	h.streamsMu.Lock()
	h.streams[sid] = ch
	h.streamsMu.Unlock()
	e := childEnvelope(protocol.Envelope{}, protocol.KindCommand, cap, major, payload, timeout)
	e.Caller = h.PluginID
	e.StreamID = sid
	resp, err := h.invokeEnvelope(e, timeout)
	if err != nil {
		h.streamsMu.Lock()
		delete(h.streams, sid)
		h.streamsMu.Unlock()
		close(ch)
		return nil, resp, err
	}
	return &Stream{ID: sid, host: h, C: ch, TraceID: e.TraceID, CorrelationID: e.CorrelationID, RequestID: e.MessageID}, resp, nil
}
func (s *Stream) Cancel() error {
	return s.host.send(protocol.Envelope{
		Protocol:      1,
		MessageID:     protocol.NewID("cancel"),
		Kind:          protocol.KindCancel,
		StreamID:      s.ID,
		TraceID:       s.TraceID,
		CorrelationID: s.CorrelationID,
		CausationID:   s.RequestID,
	})
}

func (h *Host) Serve() error {
	s := bufio.NewScanner(h.in)
	s.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for s.Scan() {
		var e protocol.Envelope
		if json.Unmarshal(s.Bytes(), &e) != nil {
			continue
		}
		if e.ReplyTo != "" && (e.Kind == protocol.KindResult || e.Kind == protocol.KindError || e.Kind == protocol.KindReady || e.Kind == protocol.KindPong) {
			h.pendingMu.Lock()
			ch := h.pending[e.ReplyTo]
			h.pendingMu.Unlock()
			if ch != nil {
				ch <- e
			}
			continue
		}
		switch e.Kind {
		case protocol.KindPing:
			_ = h.send(protocol.Envelope{Protocol: 1, MessageID: protocol.NewID("pong"), Kind: protocol.KindPong, ReplyTo: e.MessageID})
		case protocol.KindHello:
			var hello protocol.Hello
			if json.Unmarshal(e.Payload, &hello) != nil || hello.RuntimeProtocol != protocol.RuntimeProtocol {
				_ = h.send(protocol.Envelope{Protocol: 1, MessageID: protocol.NewID("err"), Kind: protocol.KindError, ReplyTo: e.MessageID, Error: &protocol.Error{Code: "INCOMPATIBLE_PROTOCOL", Message: "runtime protocol mismatch"}})
				continue
			}
			handlers := make([]protocol.HandlerDescriptor, 0, len(h.handlerKinds))
			for hk, kind := range h.handlerKinds {
				idx := strings.LastIndex(hk, "@")
				if idx > 0 {
					maj, _ := strconv.Atoi(hk[idx+1:])
					handlers = append(handlers, protocol.HandlerDescriptor{Capability: hk[:idx], Major: maj, Kind: kind})
				}
			}
			ready := protocol.Ready{RuntimeProtocol: protocol.RuntimeProtocol, PluginID: h.PluginID, PluginVersion: h.PluginVersion, ManifestHash: h.ManifestHash, Handlers: handlers}
			_ = h.send(protocol.Envelope{Protocol: 1, MessageID: protocol.NewID("ready"), Kind: protocol.KindReady, ReplyTo: e.MessageID, Payload: protocol.NewPayload(ready)})
		case protocol.KindCommand, protocol.KindQuery:
			hk := key(e.Capability, e.Major)
			fn := h.handlers[hk]
			if expected, ok := h.handlerKinds[hk]; ok && expected != e.Kind {
				_ = h.send(protocol.Envelope{Protocol: 1, MessageID: protocol.NewID("err"), Kind: protocol.KindError, ReplyTo: e.MessageID, Error: &protocol.Error{Code: "KIND_MISMATCH", Message: fmt.Sprintf("handler requires %s, got %s", expected, e.Kind)}})
				continue
			}
			if fn == nil {
				_ = h.send(protocol.Envelope{Protocol: 1, MessageID: protocol.NewID("err"), Kind: protocol.KindError, ReplyTo: e.MessageID, Error: &protocol.Error{Code: "UNSUPPORTED", Message: "capability not handled"}})
				continue
			}
			base := context.Background()
			var cancel context.CancelFunc
			if d, has, _ := protocol.ParseDeadline(e.Deadline); has {
				base, cancel = context.WithDeadline(base, d)
			} else {
				base, cancel = context.WithCancel(base)
			}
			cancelKey := e.MessageID
			if e.StreamID != "" {
				cancelKey = e.StreamID
			}
			h.cancelMu.Lock()
			h.cancels[cancelKey] = cancel
			h.cancelMu.Unlock()
			go func(req protocol.Envelope, key string, callCtx context.Context) {
				rc := &RequestContext{host: h, req: req, ctx: callCtx, cancelKey: key}
				defer func() {
					if !rc.streaming {
						h.finishCancel(key)
					}
				}()
				result, perr := fn(rc, req)
				if perr != nil {
					_ = h.send(protocol.Envelope{Protocol: 1, MessageID: protocol.NewID("err"), Kind: protocol.KindError, ReplyTo: req.MessageID, TraceID: req.TraceID, CorrelationID: req.CorrelationID, CausationID: req.MessageID, Error: perr})
				} else {
					_ = h.send(protocol.Envelope{Protocol: 1, MessageID: protocol.NewID("res"), Kind: protocol.KindResult, ReplyTo: req.MessageID, TraceID: req.TraceID, CorrelationID: req.CorrelationID, CausationID: req.MessageID, Payload: protocol.NewPayload(result)})
				}
			}(e, cancelKey, base)
		case protocol.KindEvent:
			if fn := h.events[key(e.Capability, e.Major)]; fn != nil {
				go fn(e)
			}
		case protocol.KindStreamOpen, protocol.KindStreamData, protocol.KindStreamClose:
			h.streamsMu.Lock()
			ch := h.streams[e.StreamID]
			if e.Kind == protocol.KindStreamClose {
				delete(h.streams, e.StreamID)
			}
			h.streamsMu.Unlock()
			if ch != nil {
				select {
				case ch <- e:
					if e.Kind == protocol.KindStreamClose {
						close(ch)
					}
				default:
					// Stream data must not block result/error/cancel ingress. Remove this
					// stream from routing, cancel upstream, then deliver an explicit close
					// asynchronously once the consumer makes room.
					h.streamsMu.Lock()
					delete(h.streams, e.StreamID)
					h.streamsMu.Unlock()
					_ = h.send(protocol.Envelope{Protocol: 1, MessageID: protocol.NewID("cancel"), Kind: protocol.KindCancel, StreamID: e.StreamID})
					go func(out chan protocol.Envelope, frame protocol.Envelope) {
						out <- protocol.Envelope{Protocol: 1, MessageID: protocol.NewID("sclose"), Kind: protocol.KindStreamClose, StreamID: frame.StreamID, TraceID: frame.TraceID, CorrelationID: frame.CorrelationID, CausationID: frame.CausationID, Error: &protocol.Error{Code: "BACKPRESSURE_LIMIT", Message: "stream consumer queue exhausted", Retryable: true}}
						close(out)
					}(ch, e)
				}
			}
		case protocol.KindCancel:
			key := e.StreamID
			if key == "" {
				key = e.ReplyTo
			}
			h.cancelMu.Lock()
			cancel := h.cancels[key]
			h.cancelMu.Unlock()
			if cancel != nil {
				cancel()
			}
		}
	}
	return s.Err()
}
