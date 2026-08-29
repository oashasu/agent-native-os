package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/example/agent-native-microkernel/sdk/go/fencing"
	"github.com/example/agent-native-microkernel/sdk/go/pluginhost"
	"github.com/example/agent-native-microkernel/sdk/go/protocol"
)

type sealDeps struct {
	Store   *Store
	Replay  func() ([]JournalRecord, error)
	BlobPut func([]byte) (string, error)
	Now     func() string
}

type JournalRecord struct {
	ID            string          `json:"id"`
	SHA256        string          `json:"sha256"`
	CorrelationID string          `json:"correlation_id"`
	Type          string          `json:"type"`
	Raw           json.RawMessage `json:"-"`
}

type sealRequest struct {
	WorkContextID   string   `json:"work_context_id"`
	AgentRunID      string   `json:"agent_run_id"`
	WorkspacePath   string   `json:"workspace_path"`
	CorrelationID   string   `json:"correlation_id"`
	EventIDs        []string `json:"event_ids"`
	EventSHA256s    []string `json:"event_sha256s"`
	DiffArtifactID  string   `json:"diff_artifact_id"`
	TaskID          string   `json:"task_id"`
	Provider        string   `json:"provider"`
	HarnessNativeID string   `json:"harness_native_id"`
}

type sessionIDRequest struct {
	SessionID string `json:"session_id"`
}
type sessionQueryRequest struct {
	WorkContextID string `json:"work_context_id"`
}

func sealOnce(d sealDeps, req sealRequest) (SessionRecord, *protocol.Error) {
	if req.WorkContextID == "" || req.AgentRunID == "" || req.WorkspacePath == "" {
		return SessionRecord{}, &protocol.Error{Code: "INVALID", Message: "work_context_id, agent_run_id, and workspace_path are required"}
	}
	if d.BlobPut == nil || d.Replay == nil {
		return SessionRecord{}, &protocol.Error{Code: "INVALID", Message: "seal dependencies are required"}
	}
	cp, patch, err := buildCheckpoint(req.WorkspacePath)
	if err != nil {
		return SessionRecord{}, &protocol.Error{Code: "GIT_ERROR", Message: err.Error()}
	}
	patchURI, err := d.BlobPut([]byte(patch))
	if err != nil {
		return SessionRecord{}, &protocol.Error{Code: "IO", Message: err.Error(), Retryable: true}
	}
	cp.TrackedPatchRef = patchURI

	records, err := d.Replay()
	if err != nil {
		return SessionRecord{}, &protocol.Error{Code: "IO", Message: err.Error(), Retryable: true}
	}
	corr := req.CorrelationID
	if corr == "" {
		corr = req.WorkContextID
	}
	selected := make([]JournalRecord, 0)
	if len(req.EventIDs) > 0 {
		if len(req.EventSHA256s) != len(req.EventIDs) {
			return SessionRecord{}, &protocol.Error{Code: "INTEGRITY_ERROR", Message: "event_ids and event_sha256s length mismatch"}
		}
		byID := make(map[string]JournalRecord, len(records))
		for _, r := range records {
			byID[r.ID] = r
		}
		for i, id := range req.EventIDs {
			r, ok := byID[id]
			if !ok {
				return SessionRecord{}, &protocol.Error{Code: "INTEGRITY_ERROR", Message: "requested event not found: " + id}
			}
			if r.SHA256 != req.EventSHA256s[i] {
				return SessionRecord{}, &protocol.Error{Code: "INTEGRITY_ERROR", Message: "event sha256 mismatch: " + id}
			}
			selected = append(selected, r)
		}
	} else {
		for _, r := range records {
			if r.CorrelationID == corr {
				selected = append(selected, r)
			}
		}
	}
	ids := make([]string, 0, len(selected))
	hashes := make([]string, 0, len(selected))
	raws := make([]json.RawMessage, 0, len(selected))
	for _, r := range selected {
		ids = append(ids, r.ID)
		hashes = append(hashes, r.SHA256)
		raws = append(raws, append(json.RawMessage(nil), r.Raw...))
	}
	sel := SessionEventSelection{CorrelationID: corr, EventIDs: ids, EventSHA256s: hashes, EventCount: len(selected)}
	cp.DiffArtifactID = req.DiffArtifactID
	cp.TaskID = req.TaskID
	cp.WorkContextID = req.WorkContextID
	cp.AgentRunID = req.AgentRunID
	cp.Provider = req.Provider
	cp.HarnessNativeID = req.HarnessNativeID
	cp.CanonicalEventSelection = sel
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if d.Now != nil {
		now = d.Now()
	}
	id := protocol.NewID("sess")
	partial := SessionRecord{ID: id, WorkContextID: req.WorkContextID, AgentRunID: req.AgentRunID, EventSelection: sel, RecoveryCheckpoint: cp, SealedAt: now}
	archive := struct {
		SessionRecord      SessionRecord      `json:"session_record"`
		RecoveryCheckpoint RecoveryCheckpoint `json:"recovery_checkpoint"`
		CanonicalEvents    []json.RawMessage  `json:"canonical_events"`
	}{partial, cp, raws}
	archiveBytes, err := json.Marshal(archive)
	if err != nil {
		return SessionRecord{}, &protocol.Error{Code: "IO", Message: err.Error()}
	}
	archiveURI, err := d.BlobPut(archiveBytes)
	if err != nil {
		return SessionRecord{}, &protocol.Error{Code: "IO", Message: err.Error(), Retryable: true}
	}
	sum := sha256.Sum256(archiveBytes)
	partial.ArchiveRef = archiveURI
	partial.ArchiveHash = hex.EncodeToString(sum[:])
	return partial, nil
}

func sealHandler(d sealDeps) pluginhost.ContextHandler {
	return func(_ *pluginhost.RequestContext, e protocol.Envelope) (any, *protocol.Error) {
		var req sealRequest
		if err := json.Unmarshal(e.Payload, &req); err != nil {
			return nil, &protocol.Error{Code: "INVALID", Message: "invalid request"}
		}
		sr, perr := sealOnce(d, req)
		if perr != nil {
			return nil, perr
		}
		if d.Store == nil {
			return nil, &protocol.Error{Code: "IO", Message: "session store unavailable"}
		}
		if err := fencing.WithWriteFence(e, func() error { return d.Store.Record(sr) }); err != nil {
			return nil, &protocol.Error{Code: "IO", Message: err.Error(), Retryable: true}
		}
		return map[string]any{"session_record": sr}, nil
	}
}

func getHandler(s *Store) pluginhost.Handler {
	return func(e protocol.Envelope) (any, *protocol.Error) {
		var req sessionIDRequest
		if err := json.Unmarshal(e.Payload, &req); err != nil || req.SessionID == "" {
			return nil, &protocol.Error{Code: "INVALID", Message: "session_id is required"}
		}
		sr, ok := s.GetByID(req.SessionID)
		if !ok {
			return nil, &protocol.Error{Code: "NOT_FOUND", Message: ErrNotFound.Error()}
		}
		return map[string]any{"session_record": sr}, nil
	}
}
func queryHandler(s *Store) pluginhost.Handler {
	return func(e protocol.Envelope) (any, *protocol.Error) {
		var req sessionQueryRequest
		if err := json.Unmarshal(e.Payload, &req); err != nil || req.WorkContextID == "" {
			return nil, &protocol.Error{Code: "INVALID", Message: "work_context_id is required"}
		}
		xs := s.QueryByContext(req.WorkContextID)
		if xs == nil {
			xs = []SessionRecord{}
		}
		return map[string]any{"session_records": xs}, nil
	}
}
