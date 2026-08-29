package router

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/example/agent-native-microkernel/internal/authz"
	"github.com/example/agent-native-microkernel/internal/registry"
	"github.com/example/agent-native-microkernel/sdk/go/protocol"
	"sync"
	"time"
)

type delegationContext struct {
	principal  string
	actorChain []string
	runtimeID  string
	scope      []string
}

type streamRoute struct {
	streamID         string
	consumerRuntime  string
	externalIdentity string
	providerRuntime  string
	requestID        string
	external         chan protocol.Envelope
}
type inflightKey struct {
	consumer  string
	requestID string
}
type inflightRoute struct {
	providerRuntime   string
	providerRequestID string
}
type Router struct {
	reg         *registry.Registry
	auth        *authz.Engine
	mu          sync.RWMutex
	clients     map[string]*ProcessClient
	streams     map[string]streamRoute
	delegations map[string]delegationContext
	inflight    map[inflightKey]inflightRoute
}

func New(reg *registry.Registry, auth *authz.Engine) *Router {
	return &Router{reg: reg, auth: auth, clients: map[string]*ProcessClient{}, streams: map[string]streamRoute{}, delegations: map[string]delegationContext{}, inflight: map[inflightKey]inflightRoute{}}
}
func (r *Router) AddClient(runtimeID string, c *ProcessClient) {
	r.mu.Lock()
	r.clients[runtimeID] = c
	r.mu.Unlock()
	go r.serveInbound(c)
	go r.serveStreamInbound(c)
}
func (r *Router) RemoveClient(runtimeID string) {
	r.mu.Lock()
	delete(r.clients, runtimeID)
	affected := make([]streamRoute, 0)
	consumerLost := make([]streamRoute, 0)
	for id, sr := range r.streams {
		if sr.providerRuntime == runtimeID {
			affected = append(affected, sr)
			delete(r.streams, id)
		} else if sr.consumerRuntime == runtimeID {
			consumerLost = append(consumerLost, sr)
			delete(r.streams, id)
		}
	}
	r.mu.Unlock()
	for _, sr := range affected {
		closeEnv := protocol.Envelope{Protocol: 1, MessageID: protocol.NewID("sclose"), Kind: protocol.KindStreamClose, StreamID: sr.streamID, Error: &protocol.Error{Code: "PROVIDER_DISCONNECTED", Message: "stream provider disconnected", Retryable: true}}
		if sr.external != nil {
			go func(ch chan protocol.Envelope, e protocol.Envelope) { ch <- e; close(ch) }(sr.external, closeEnv)
		} else {
			r.mu.RLock()
			target := r.clients[sr.consumerRuntime]
			r.mu.RUnlock()
			if target != nil {
				_ = target.Send(closeEnv)
			}
		}
	}
	for _, sr := range consumerLost {
		r.mu.RLock()
		provider := r.clients[sr.providerRuntime]
		r.mu.RUnlock()
		if provider != nil {
			_ = provider.Send(protocol.Envelope{Protocol: 1, MessageID: protocol.NewID("cancel"), Kind: protocol.KindCancel, ReplyTo: sr.requestID, TargetRuntime: sr.providerRuntime})
		}
	}
}
func (r *Router) serveStreamInbound(c *ProcessClient) {
	for e := range c.StreamEvents() {
		r.forwardStream(c.runtimeID, e)
	}
}
func (r *Router) serveInbound(c *ProcessClient) {
	for msg := range c.Events() {
		e := msg.Envelope
		func() {
			defer msg.Ack()
			switch e.Kind {
			case protocol.KindCommand, protocol.KindQuery:
				resp, err := r.invoke(c.runtimeID, c.pluginID, true, e, nil)
				if err != nil {
					_ = c.Send(protocol.Envelope{Protocol: 1, MessageID: protocol.NewID("err"), Kind: protocol.KindError, ReplyTo: e.MessageID, TraceID: e.TraceID, CorrelationID: e.CorrelationID, CausationID: e.MessageID, Error: &protocol.Error{Code: "ROUTE_ERROR", Message: err.Error()}})
					return
				}
				resp.MessageID = protocol.NewID("reply")
				resp.ReplyTo = e.MessageID
				resp.TraceID = e.TraceID
				resp.CorrelationID = e.CorrelationID
				resp.CausationID = e.MessageID
				_ = c.Send(resp)
			case protocol.KindEvent:
				_ = r.publishEnvelope(c.runtimeID, c.pluginID, e)
			case protocol.KindStreamClose:
				// ProcessClient may synthesize a close on local backpressure overflow.
				r.forwardStream(c.runtimeID, e)
			case protocol.KindCancel:
				r.forwardCancel(c.runtimeID, e)
			}
		}()
	}
}
func (r *Router) forwardStream(providerRuntime string, e protocol.Envelope) {
	r.mu.RLock()
	sr, ok := r.streams[e.StreamID]
	var target *ProcessClient
	if sr.consumerRuntime != "" {
		target = r.clients[sr.consumerRuntime]
	}
	r.mu.RUnlock()
	if !ok || sr.providerRuntime != providerRuntime {
		return
	}
	if sr.external != nil {
		sr.external <- e
		if e.Kind == protocol.KindStreamClose {
			close(sr.external)
		}
	} else if target != nil {
		e.TargetRuntime = sr.consumerRuntime
		_ = target.Send(e)
	}
	if e.Kind == protocol.KindStreamClose {
		r.mu.Lock()
		delete(r.streams, e.StreamID)
		r.mu.Unlock()
	}
}

func runtimeConsumerKey(runtimeID string) string { return "runtime:" + runtimeID }
func externalConsumerKey(identity string) string { return "external:" + identity }

func (r *Router) forwardCancel(consumerRuntime string, e protocol.Envelope) {
	if e.StreamID != "" {
		r.mu.RLock()
		sr, ok := r.streams[e.StreamID]
		var p *ProcessClient
		if ok {
			p = r.clients[sr.providerRuntime]
		}
		r.mu.RUnlock()
		if !ok || sr.consumerRuntime != consumerRuntime || p == nil {
			return
		}
		e.TargetRuntime = sr.providerRuntime
		e.ReplyTo = sr.requestID
		_ = p.Send(e)
		return
	}
	if e.ReplyTo == "" {
		return
	}
	r.mu.RLock()
	route, ok := r.inflight[inflightKey{consumer: runtimeConsumerKey(consumerRuntime), requestID: e.ReplyTo}]
	p := r.clients[route.providerRuntime]
	r.mu.RUnlock()
	if !ok || p == nil {
		return
	}
	e.TargetRuntime = route.providerRuntime
	e.ReplyTo = route.providerRequestID
	_ = p.Send(e)
}

func (r *Router) Invoke(callerPlugin string, req protocol.Envelope) (protocol.Envelope, error) {
	return r.invoke("", callerPlugin, true, req, nil)
}
func (r *Router) InvokeExternal(identity string, req protocol.Envelope) (protocol.Envelope, error) {
	return r.invoke("", identity, false, req, nil)
}
func (r *Router) invoke(callerRuntime, caller string, isPlugin bool, req protocol.Envelope, external chan protocol.Envelope) (protocol.Envelope, error) {
	if err := protocol.ValidateEnvelope(req); err != nil {
		return protocol.Envelope{}, err
	}

	// Authorization identity is host-derived. A plugin may forward trace metadata,
	// but it may not choose the principal whose authority is being exercised.
	principal := caller
	actorChain := []string{caller}
	delegationScope := []string(nil)
	if isPlugin {
		if callerRuntime != "" {
			if req.DelegationID == "" {
				// A runtime may not silently drop delegation and fall back to its broader
				// plugin grant. Autonomous/background service calls require an explicit
				// host-policy service_authority grant.
				if err := r.auth.CanInvokeAsService(caller); err != nil {
					return protocol.Envelope{}, fmt.Errorf("delegation required for %s: %w", caller, err)
				}
			} else {
				r.mu.RLock()
				d, ok := r.delegations[req.DelegationID]
				r.mu.RUnlock()
				if !ok || d.runtimeID != callerRuntime {
					return protocol.Envelope{}, fmt.Errorf("invalid or cross-runtime delegation")
				}
				principal = d.principal
				actorChain = append(append([]string(nil), d.actorChain...), caller)
				delegationScope = append([]string(nil), d.scope...)
			}
		}
		m, ok := r.reg.Manifest(caller)
		if !ok {
			return protocol.Envelope{}, fmt.Errorf("unknown caller plugin %q", caller)
		}
		// The immediate plugin must be allowed to consume the capability.
		if err := r.auth.CanConsume(m, req.Capability, req.Major); err != nil {
			return protocol.Envelope{}, err
		}
		// A delegated call may never exceed the originating principal's grant.
		// This prevents a low-privilege user from borrowing a composition plugin's
		// broader authority (confused-deputy escalation).
		if principal != caller {
			directErr := r.auth.CanExternal(principal, req.Capability, req.Major)
			if directErr != nil && !authz.ScopeAllows(delegationScope, req.Capability, req.Major) {
				return protocol.Envelope{}, fmt.Errorf("principal %s has neither direct nor delegated authority for %s@%d", principal, req.Capability, req.Major)
			}
		}
	} else {
		if err := r.auth.CanExternal(caller, req.Capability, req.Major); err != nil {
			return protocol.Envelope{}, err
		}
		delegationScope = r.auth.DelegationScope(caller, req.Capability, req.Major)
	}
	p, err := r.reg.ResolveForKind(req.Capability, req.Major, req.ProviderHint, req.Service, req.Authority, req.Kind)
	if err != nil {
		return protocol.Envelope{}, err
	}
	if meta, ok := r.reg.ContractMetadata(p.Export.Contract); ok && meta.Kind != req.Kind {
		return protocol.Envelope{Protocol: 1, Kind: protocol.KindError, Error: &protocol.Error{Code: "KIND_MISMATCH", Message: fmt.Sprintf("%s requires %s, got %s", p.Export.Contract, meta.Kind, req.Kind), Retryable: false}}, nil
	}
	r.mu.RLock()
	pc := r.clients[p.RuntimeID]
	r.mu.RUnlock()
	if pc == nil {
		return protocol.Envelope{}, fmt.Errorf("runtime %s unavailable", p.RuntimeID)
	}
	timeout := 30 * time.Second
	if dl, has, err := protocol.ParseDeadline(req.Deadline); err != nil {
		return protocol.Envelope{}, err
	} else if has {
		timeout = time.Until(dl)
		if timeout <= 0 {
			return protocol.Envelope{}, fmt.Errorf("deadline already exceeded")
		}
	}
	// TCB metadata is overwritten by the host. Never trust values supplied by a
	// client or plugin process for these fields.
	req.Caller = caller
	req.Principal = principal
	req.ActorChain = append([]string(nil), actorChain...)
	req.ReplyRuntime = callerRuntime
	req.Contract = p.Export.Contract
	if p.Export.Mode == "stateful" {
		req.Service = p.Export.Service
		req.Authority = p.Export.Authority
		if req.Kind == protocol.KindCommand {
			req.FencingEpoch = p.WriterEpoch
		}
	}
	if req.StreamID != "" && (callerRuntime != "" || external != nil) {
		r.mu.Lock()
		r.streams[req.StreamID] = streamRoute{streamID: req.StreamID, consumerRuntime: callerRuntime, externalIdentity: func() string {
			if !isPlugin {
				return caller
			}
			return ""
		}(), providerRuntime: p.RuntimeID, requestID: req.MessageID, external: external}
		r.mu.Unlock()
	}
	// Issue a fresh opaque delegation for the selected provider runtime. The
	// provider may pass it back for child calls, but cannot mint a token bound to
	// another runtime or silently upgrade to service authority.
	delegationID := protocol.NewID("deleg")
	req.DelegationID = delegationID
	r.mu.Lock()
	r.delegations[delegationID] = delegationContext{principal: principal, actorChain: append([]string(nil), actorChain...), runtimeID: p.RuntimeID, scope: append([]string(nil), delegationScope...)}
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		delete(r.delegations, delegationID)
		r.mu.Unlock()
	}()
	consumer := externalConsumerKey(caller)
	if isPlugin {
		consumer = runtimeConsumerKey(callerRuntime)
	}
	if callerRuntime != "" || !isPlugin {
		ik := inflightKey{consumer: consumer, requestID: req.MessageID}
		r.mu.Lock()
		r.inflight[ik] = inflightRoute{providerRuntime: p.RuntimeID, providerRequestID: req.MessageID}
		r.mu.Unlock()
		defer func() { r.mu.Lock(); delete(r.inflight, ik); r.mu.Unlock() }()
	}
	resp, err := pc.Call(req, timeout)
	if err != nil {
		r.reg.RecordFailure(p.RuntimeID)
		if errors.Is(err, ErrDeadlineExceeded) {
			_ = pc.Send(protocol.Envelope{Protocol: 1, MessageID: protocol.NewID("cancel"), Kind: protocol.KindCancel, ReplyTo: req.MessageID, TargetRuntime: p.RuntimeID, TraceID: req.TraceID, CorrelationID: req.CorrelationID, CausationID: req.MessageID})
		}
		return protocol.Envelope{}, err
	}
	r.reg.RecordSuccess(p.RuntimeID)
	// A provider-produced KindError is still a successfully routed response.
	// Do not reinterpret business/provider errors as transport failures; callers
	// need the original code/retryable/details for retry and workflow policy.
	return resp, nil
}
func (r *Router) InvokeExternalStream(identity string, req protocol.Envelope) (protocol.Envelope, <-chan protocol.Envelope, error) {
	if req.StreamID == "" {
		req.StreamID = protocol.NewID("stream")
	}
	ch := make(chan protocol.Envelope, 64)
	resp, err := r.invoke("", identity, false, req, ch)
	if err != nil {
		close(ch)
		return resp, ch, err
	}
	return resp, ch, nil
}
func (r *Router) CancelExternalStream(identity, streamID string) bool {
	r.mu.RLock()
	sr, ok := r.streams[streamID]
	var p *ProcessClient
	if ok {
		p = r.clients[sr.providerRuntime]
	}
	r.mu.RUnlock()
	if !ok || sr.externalIdentity != identity || p == nil {
		return false
	}
	_ = p.Send(protocol.Envelope{Protocol: 1, MessageID: protocol.NewID("cancel"), Kind: protocol.KindCancel, StreamID: streamID, ReplyTo: sr.requestID, TargetRuntime: sr.providerRuntime})
	return true
}
func (r *Router) CancelExternalRequest(identity, requestID string) bool {
	r.mu.RLock()
	route, ok := r.inflight[inflightKey{consumer: externalConsumerKey(identity), requestID: requestID}]
	var p *ProcessClient
	if ok {
		p = r.clients[route.providerRuntime]
	}
	r.mu.RUnlock()
	if !ok || p == nil {
		return false
	}
	_ = p.Send(protocol.Envelope{Protocol: 1, MessageID: protocol.NewID("cancel"), Kind: protocol.KindCancel, ReplyTo: route.providerRequestID, TargetRuntime: route.providerRuntime})
	return true
}

func (r *Router) Publish(callerPlugin, eventName string, major int, payload any) error {
	raw, _ := json.Marshal(payload)
	return r.publishEnvelope("", callerPlugin, protocol.Envelope{Protocol: 1, MessageID: protocol.NewID("evt"), Kind: protocol.KindEvent, Capability: eventName, Major: major, TraceID: protocol.NewID("trace"), CorrelationID: protocol.NewID("corr"), Payload: raw})
}
func (r *Router) PublishEnvelope(callerPlugin string, e protocol.Envelope) error {
	return r.publishEnvelope("", callerPlugin, e)
}
func (r *Router) publishEnvelope(callerRuntime, callerPlugin string, e protocol.Envelope) error {
	caller, ok := r.reg.Manifest(callerPlugin)
	if !ok {
		return fmt.Errorf("unknown caller plugin %q", callerPlugin)
	}
	if err := r.auth.CanPublish(caller, e.Capability, e.Major); err != nil {
		return err
	}
	principal := callerPlugin
	actorChain := []string{callerPlugin}
	if callerRuntime != "" {
		if e.DelegationID == "" {
			if err := r.auth.CanInvokeAsService(callerPlugin); err != nil {
				return fmt.Errorf("delegation required for event publish by %s: %w", callerPlugin, err)
			}
		} else {
			r.mu.RLock()
			d, found := r.delegations[e.DelegationID]
			r.mu.RUnlock()
			if !found || d.runtimeID != callerRuntime {
				return fmt.Errorf("invalid event delegation")
			}
			principal = d.principal
			actorChain = append(append([]string(nil), d.actorChain...), callerPlugin)
		}
	}
	// Actor/provenance metadata is TCB-owned and is always rewritten.
	e.Caller = callerPlugin
	e.Principal = principal
	e.ActorChain = actorChain
	r.mu.RLock()
	clients := make([]*ProcessClient, 0, len(r.clients))
	for _, c := range r.clients {
		clients = append(clients, c)
	}
	r.mu.RUnlock()
	for _, c := range clients {
		m, ok := r.reg.Manifest(c.pluginID)
		if !ok {
			continue
		}
		if err := r.auth.CanSubscribe(m, e.Capability, e.Major); err != nil {
			continue
		}
		if err := c.Send(e); err != nil {
			return err
		}
	}
	return nil
}
