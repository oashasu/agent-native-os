package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/example/agent-native-microkernel/sdk/go/contracts/eventv1"
	"github.com/example/agent-native-microkernel/sdk/go/fencing"
	"github.com/example/agent-native-microkernel/sdk/go/pluginhost"
	"github.com/example/agent-native-microkernel/sdk/go/protocol"
)

type journal struct {
	mu       sync.Mutex
	path     string
	lastHash string
}

type hashMaterial struct {
	ID             string          `json:"id"`
	Type           string          `json:"type"`
	Timestamp      string          `json:"timestamp"`
	TraceID        string          `json:"trace_id"`
	CorrelationID  string          `json:"correlation_id"`
	CausationID    string          `json:"causation_id"`
	Caller         string          `json:"caller"`
	Principal      string          `json:"principal"`
	ActorChain     []string        `json:"actor_chain,omitempty"`
	Source         string          `json:"source"`
	Payload        json.RawMessage `json:"payload"`
	PreviousSHA256 string          `json:"previous_sha256,omitempty"`
}

func digest(r eventv1.Record) (string, error) {
	b, err := json.Marshal(hashMaterial{r.ID, r.Type, r.Timestamp, r.TraceID, r.CorrelationID, r.CausationID, r.Caller, r.Principal, r.ActorChain, r.Source, r.Payload, r.PreviousSHA256})
	if err != nil {
		return "", err
	}
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:]), nil
}

func (j *journal) verifyAndLoadTail() error {
	f, err := os.Open(j.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	prev := ""
	line := 0
	for s.Scan() {
		line++
		var r eventv1.Record
		if err := json.Unmarshal(s.Bytes(), &r); err != nil {
			return fmt.Errorf("line %d: %w", line, err)
		}
		if r.PreviousSHA256 != prev {
			return fmt.Errorf("line %d previous hash mismatch", line)
		}
		got, err := digest(r)
		if err != nil {
			return err
		}
		if got != r.SHA256 {
			return fmt.Errorf("line %d hash mismatch", line)
		}
		prev = r.SHA256
	}
	if err := s.Err(); err != nil {
		return err
	}
	j.lastHash = prev
	return nil
}

func appendHandler(j *journal) pluginhost.Handler {
	return func(e protocol.Envelope) (any, *protocol.Error) {
		var q eventv1.AppendRequest
		if json.Unmarshal(e.Payload, &q) != nil || q.Type == "" {
			return nil, &protocol.Error{Code: "INVALID", Message: "invalid event"}
		}
		rec := eventv1.Record{
			ID: protocol.NewID("event"), Type: q.Type,
			Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
			TraceID:   e.TraceID, CorrelationID: e.CorrelationID, CausationID: e.CausationID,
			Caller: e.Caller, Principal: e.Principal, ActorChain: append([]string(nil), e.ActorChain...),
			Source: q.Source, Payload: q.Payload,
		}
		j.mu.Lock()
		err := fencing.WithWriteFence(e, func() error {
			rec.PreviousSHA256 = j.lastHash
			sum, derr := digest(rec)
			if derr != nil {
				return derr
			}
			rec.SHA256 = sum
			line, merr := json.Marshal(rec)
			if merr != nil {
				return merr
			}
			f, oerr := os.OpenFile(j.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
			if oerr != nil {
				return oerr
			}
			_, werr := f.Write(append(line, '\n'))
			if werr == nil {
				werr = f.Sync()
			}
			cerr := f.Close()
			if werr != nil {
				return werr
			}
			if cerr != nil {
				return cerr
			}
			j.lastHash = rec.SHA256
			return nil
		})
		j.mu.Unlock()
		if err != nil {
			return nil, &protocol.Error{Code: "IO", Message: err.Error(), Retryable: true}
		}
		return eventv1.AppendResponse{Record: rec}, nil
	}
}

func replayHandler(j *journal) pluginhost.Handler {
	return func(e protocol.Envelope) (any, *protocol.Error) {
		var q eventv1.ReplayRequest
		_ = json.Unmarshal(e.Payload, &q)
		if q.Limit <= 0 || q.Limit > 1000 {
			q.Limit = 100
		}
		j.mu.Lock()
		defer j.mu.Unlock()
		f, err := os.Open(j.path)
		if os.IsNotExist(err) {
			return eventv1.ReplayResponse{Records: []eventv1.Record{}, Next: q.After}, nil
		}
		if err != nil {
			return nil, &protocol.Error{Code: "IO", Message: err.Error()}
		}
		defer f.Close()
		s := bufio.NewScanner(f)
		s.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
		idx := 0
		prev := ""
		out := []eventv1.Record{}
		for s.Scan() {
			var r eventv1.Record
			if err := json.Unmarshal(s.Bytes(), &r); err != nil {
				return nil, &protocol.Error{Code: "INTEGRITY_ERROR", Message: err.Error()}
			}
			if r.PreviousSHA256 != prev {
				return nil, &protocol.Error{Code: "INTEGRITY_ERROR", Message: "previous hash mismatch"}
			}
			got, derr := digest(r)
			if derr != nil || got != r.SHA256 {
				return nil, &protocol.Error{Code: "INTEGRITY_ERROR", Message: "record hash mismatch"}
			}
			prev = r.SHA256
			if idx >= q.After && len(out) < q.Limit {
				out = append(out, r)
			}
			idx++
		}
		if err := s.Err(); err != nil {
			return nil, &protocol.Error{Code: "IO", Message: err.Error()}
		}
		return eventv1.ReplayResponse{Records: out, Next: q.After + len(out)}, nil
	}
}

func main() {
	d := os.Getenv("VIBE_DATA_DIR")
	if err := os.MkdirAll(d, 0o755); err != nil {
		panic(err)
	}
	j := &journal{path: filepath.Join(d, "events.jsonl")}
	if err := j.verifyAndLoadTail(); err != nil {
		panic("journal integrity: " + err.Error())
	}
	h := pluginhost.New("org.vibe.event.journal", "1.0.0", "")
	h.HandleContextCommand("event.journal.append", 1, func(_ *pluginhost.RequestContext, e protocol.Envelope) (any, *protocol.Error) {
		return appendHandler(j)(e)
	})
	h.HandleQuery("event.journal.replay", 1, replayHandler(j))
	_ = h.Serve()
}
