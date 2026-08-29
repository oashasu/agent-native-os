package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const RuntimeProtocol = "vibe-plugin/1"

type Kind string

const (
	KindHello       Kind = "hello"
	KindReady       Kind = "ready"
	KindPing        Kind = "ping"
	KindPong        Kind = "pong"
	KindCommand     Kind = "command"
	KindQuery       Kind = "query"
	KindResult      Kind = "result"
	KindError       Kind = "error"
	KindEvent       Kind = "event"
	KindCancel      Kind = "cancel"
	KindStreamOpen  Kind = "stream.open"
	KindStreamData  Kind = "stream.data"
	KindStreamClose Kind = "stream.close"
)

type Envelope struct {
	Protocol       int             `json:"protocol"`
	MessageID      string          `json:"message_id"`
	Kind           Kind            `json:"kind"`
	Capability     string          `json:"capability,omitempty"`
	Major          int             `json:"major,omitempty"`
	Contract       string          `json:"contract,omitempty"`
	Caller         string          `json:"caller,omitempty"`
	Principal      string          `json:"principal,omitempty"`
	ActorChain     []string        `json:"actor_chain,omitempty"`
	DelegationID   string          `json:"delegation_id,omitempty"`
	ProviderHint   string          `json:"provider_hint,omitempty"`
	Service        string          `json:"service,omitempty"`
	Authority      string          `json:"authority,omitempty"`
	FencingEpoch   int64           `json:"fencing_epoch,omitempty"`
	ReplyTo        string          `json:"reply_to,omitempty"`
	ReplyRuntime   string          `json:"reply_runtime,omitempty"`
	TargetRuntime  string          `json:"target_runtime,omitempty"`
	TraceID        string          `json:"trace_id,omitempty"`
	CorrelationID  string          `json:"correlation_id,omitempty"`
	CausationID    string          `json:"causation_id,omitempty"`
	Deadline       string          `json:"deadline,omitempty"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
	StreamID       string          `json:"stream_id,omitempty"`
	Payload        json.RawMessage `json:"payload,omitempty"`
	Error          *Error          `json:"error,omitempty"`
}

type Error struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	Retryable bool           `json:"retryable"`
	Details   map[string]any `json:"details,omitempty"`
}

// RemoteError preserves a provider-produced protocol error when an SDK caller
// chooses idiomatic Go error handling. It is not a transport/router failure.
type RemoteError struct {
	Remote Error
}

func (e *RemoteError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Remote.Code == "" {
		return e.Remote.Message
	}
	return fmt.Sprintf("%s: %s", e.Remote.Code, e.Remote.Message)
}

func NewRemoteError(e *Error) error {
	if e == nil {
		return nil
	}
	return &RemoteError{Remote: *e}
}

type Hello struct {
	RuntimeProtocol string `json:"runtime_protocol"`
	RuntimeID       string `json:"runtime_id"`
	PluginID        string `json:"plugin_id"`
}

type HandlerDescriptor struct {
	Capability string `json:"capability"`
	Major      int    `json:"major"`
	Kind       Kind   `json:"kind"`
}

type Ready struct {
	RuntimeProtocol string              `json:"runtime_protocol"`
	PluginID        string              `json:"plugin_id"`
	PluginVersion   string              `json:"plugin_version"`
	ManifestHash    string              `json:"manifest_hash"`
	Handlers        []HandlerDescriptor `json:"handlers,omitempty"`
}

type StreamAccepted struct {
	StreamID string `json:"stream_id"`
}

type StreamFrame struct {
	Seq  int64           `json:"seq"`
	Data json.RawMessage `json:"data,omitempty"`
}

func NewPayload(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func ParseDeadline(s string) (time.Time, bool, error) {
	if s == "" {
		return time.Time{}, false, nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	return t, true, err
}

func ValidateEnvelope(e Envelope) error {
	if e.Protocol != 1 {
		return fmt.Errorf("unsupported protocol version %d", e.Protocol)
	}
	if e.MessageID == "" {
		return errors.New("message_id is required")
	}
	switch e.Kind {
	case KindCommand, KindQuery:
		if e.Capability == "" || e.Major <= 0 {
			return errors.New("capability and positive major are required")
		}
	case KindEvent:
		if e.Capability == "" || e.Major <= 0 {
			return errors.New("event name in capability and positive major are required")
		}
	case KindResult, KindError, KindPong:
		if e.ReplyTo == "" {
			return errors.New("reply_to is required for result/error")
		}
	case KindCancel:
		if e.StreamID == "" && e.ReplyTo == "" {
			return errors.New("cancel requires stream_id or reply_to")
		}
	case KindStreamOpen, KindStreamData, KindStreamClose:
		if e.StreamID == "" {
			return errors.New("stream messages require stream_id")
		}
	case KindHello, KindReady, KindPing:
	default:
		return fmt.Errorf("unsupported kind %q", e.Kind)
	}
	return nil
}
