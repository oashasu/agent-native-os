# M1.1 — Blob store + Work Registry + `vibe` CLI — Implementation Plan

**Execution:** Work task-by-task, in order. Each task: write the failing test first, run it and watch it fail for the right reason, write the minimal implementation, run it green, then (for behavioural changes) briefly invert the production change to confirm the test goes red and restore it. Commit at the end of each task with the message given in the task.

**Goal:** Land the two plugins that hold the M1 domain's canonical state — `org.vibe.blob` (content-addressed bytes) and `org.vibe.work.registry` (Task / WorkContext / real lifecycle state machine / evidence refs) — plus a product `vibe` CLI that creates and inspects Tasks against a running kernel.

**Architecture:** Both plugins run as separate OS processes under the microkernel, reachable only through versioned contracts. `blob` is a plain content-addressed file store (`objects/sha256/<aa>/<hex>`), writes fenced. `work-registry` keeps an append-only JSONL command log (`work-log.jsonl`, fsync per append) and rebuilds an in-memory projection on start — the same durable pattern the `event-journal` plugin already uses, no database dependency. The `vibe` CLI is a third Go module that speaks the kernel's Unix-socket wire protocol directly (it reimplements the ~20-line dial because the kernel's client helper is in an `internal/` package).

**Tech Stack:** Go (standard library + the kernel's public `sdk/go/{protocol,pluginhost,fencing}` packages, resolved locally via the `go.work` workspace — **no new external Go dependencies**, the build environment has no module-proxy access), newline-delimited JSON over a Unix socket, Python 3 + `jsonschema` for contract checks.

**Spec:** `docs/M1-DESIGN.md` — §4 (DONE invariant / transition state machine), §5.1 (blob), §5.2 (work-registry), §6 (data model), §13 milestone M1.1.

**Base:** branch from `main` at `9b7aa32`. Everything M1.0 delivered is present: `go.work`, `plugins/` module, `contracts/` + `scripts/check-contracts.py`, `plugins/foundation/event-journal/`, `plugins/_template/`, `scripts/{build,dev-run,smoke,check-arch}.sh`, `config/m1-{policy,bindings}.json`, `architecture-tests/check_composition.py`, `fixtures/sample-java-project/`.

## Global Constraints

- **G1 Kernel Purity:** no task modifies `kernel/` source. M1 code only *consumes* `kernel/sdk/go/...`. The G1 check compares `kernel/` against the branch base `9b7aa32`; expected delta is empty. If a task seems to need a kernel change, stop and report.
- **No new external Go modules.** `plugins/go.mod` and the new `cli/go.mod` must require only `github.com/example/agent-native-microkernel` (resolved by the workspace, no `replace`, no pseudo-version that would hit `proxy.golang.org`). Storage is plain files + JSONL, not SQLite.
- **Module paths:** kernel `github.com/example/agent-native-microkernel`; plugins `github.com/example/agent-native-os/plugins`; CLI `github.com/example/agent-native-os/cli`.
- **`go` directive** in new `go.mod` files: `go 1.19`. `go.work` already says `go 1.23`; leave it.
- **Manifest rule:** an export/consume entry's `contract` field MUST equal `<capability>@<major>` exactly (kernel admission rejects mismatches). Stateful exports need `mode: "stateful"`, `service`, `authority`, and the manifest needs `runtime.data_namespace`.
- **Contract rule:** `contracts/<dotted.name>/v<major>/schema.json` with `contract` = identity, `kind` ∈ command|query|event, `version` starting `"<major>."`, `request`/`response` as Draft 2020-12 schemas. Register in `contracts/catalog.json`. Run `python3 scripts/check-contracts.py --root contracts` after every catalog change.
- **Commit trailer** — every commit message ends with exactly:
  ```
  Co-Authored-By: ChatGPT <chatgpt@users.noreply.github.com>
  Plan: docs/superpowers/plans/2026-08-29-m1-1-blob-work-registry.md
  ```
- **Commit identity:** author `ada <oashasu@gmail.com>`.
- **Number discipline:** any count in an expected-output string (contract count, manifest count, test count) is whatever the commands in this plan actually produce — if it differs from a literal here, trust the commands, adjust the assertion, and note it in the final report.

---

## File Structure

New:

- `contracts/blob.put/v1/schema.json`, `contracts/blob.get/v1/schema.json`, `contracts/blob.stat/v1/schema.json`
- `contracts/work.create/v1/schema.json`, `contracts/work.get/v1/schema.json`, `contracts/work.transition/v1/schema.json`, `contracts/work.attach-evidence/v1/schema.json`
- `plugins/foundation/blob/main.go`, `plugins/foundation/blob/blob_test.go`
- `plugins/manifests/blob.manifest.json`
- `plugins/work-registry/store.go`, `plugins/work-registry/store_test.go` — types + JSONL log + projection
- `plugins/work-registry/handlers.go`, `plugins/work-registry/handlers_test.go` — the four capability handlers
- `plugins/work-registry/main.go` — wiring
- `plugins/manifests/work-registry.manifest.json`
- `cli/go.mod`, `cli/vibe/main.go`, `cli/vibe/wire.go`, `cli/vibe/wire_test.go`
- `docs/PLUGIN-STORAGE-GUIDANCE.md`

Modified:

- `contracts/catalog.json` — +7 entries
- `config/m1-policy.json` — grants for `local-cli`
- `config/m1-bindings.json` — bindings for the 7 new stateful capabilities
- `go.work` — add `./cli`
- `scripts/build.sh` — build `cli/vibe` too
- `scripts/smoke.sh` — extend with blob + work + restart coverage

Responsibilities: `store.go` owns persistence + projection and knows nothing about the protocol; `handlers.go` maps envelopes ↔ store operations and owns validation + the state machine; `main.go` only wires. `cli/vibe/wire.go` is the socket client; `main.go` is arg parsing + output.

---

## Task 1: blob contracts

**Files:** create the three blob schemas; modify `contracts/catalog.json`.

- [ ] **Step 1: write the schemas**

`contracts/blob.put/v1/schema.json`:

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "contract": "blob.put@1",
  "version": "1.0.0",
  "kind": "command",
  "compatibility": "backward-within-major",
  "request": {
    "type": "object",
    "additionalProperties": false,
    "properties": { "content_base64": { "type": "string" } },
    "required": ["content_base64"]
  },
  "response": {
    "type": "object",
    "additionalProperties": true,
    "properties": {
      "uri": { "type": "string" },
      "sha256": { "type": "string" },
      "size": { "type": "integer" },
      "existed": { "type": "boolean" }
    },
    "required": ["uri", "sha256", "size", "existed"]
  }
}
```

`contracts/blob.get/v1/schema.json`:

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "contract": "blob.get@1",
  "version": "1.0.0",
  "kind": "query",
  "compatibility": "backward-within-major",
  "request": {
    "type": "object",
    "additionalProperties": false,
    "properties": { "uri": { "type": "string" } },
    "required": ["uri"]
  },
  "response": {
    "type": "object",
    "additionalProperties": true,
    "properties": {
      "content_base64": { "type": "string" },
      "size": { "type": "integer" }
    },
    "required": ["content_base64", "size"]
  }
}
```

`contracts/blob.stat/v1/schema.json`: same shape as `blob.get` but `contract` `"blob.stat@1"`, `kind` `"query"`, request `{ "uri": string }` required, response:

```json
  "response": {
    "type": "object",
    "additionalProperties": true,
    "properties": {
      "exists": { "type": "boolean" },
      "size": { "type": "integer" }
    },
    "required": ["exists", "size"]
  }
```

- [ ] **Step 2: register in the catalog**

Add to `contracts/catalog.json` (keep existing `event.journal.*` entries):

```json
  "blob.put@1": "blob.put/v1/schema.json",
  "blob.get@1": "blob.get/v1/schema.json",
  "blob.stat@1": "blob.stat/v1/schema.json"
```

- [ ] **Step 3: run the contract check**

Run: `python3 scripts/check-contracts.py --root contracts`
Expected: `CONTRACT CHECK: PASSED` with the count = number of entries now in `catalog.json` (was 2, add 3 → 5).

- [ ] **Step 4: commit**

```
build(m1.1): blob.put/get/stat contracts
```

---

## Task 2: `org.vibe.blob` plugin

**Files:** `plugins/foundation/blob/main.go`, `plugins/foundation/blob/blob_test.go`, `plugins/manifests/blob.manifest.json`; modify `scripts/build.sh`.

**Interfaces:**
- Consumes: `sdk/go/{pluginhost,protocol,fencing}`.
- Produces: a binary `blob` exporting `blob.put@1` (stateful command, fenced), `blob.get@1` / `blob.stat@1` (stateful queries), service `default-blob`, authority `blob-main`. URI form `blob://sha256/<64-hex>`. On-disk layout `$VIBE_DATA_DIR/objects/sha256/<first-2-hex>/<64-hex>`.

- [ ] **Step 1: write the failing test**

`plugins/foundation/blob/blob_test.go`:

```go
package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/example/agent-native-microkernel/sdk/go/protocol"
)

func fenced(t *testing.T, dir string) protocol.Envelope {
	t.Helper()
	fenceRoot := filepath.Join(dir, ".fences")
	if err := os.MkdirAll(fenceRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VIBE_DATA_DIR", dir)
	t.Setenv("VIBE_RUNTIME_ID", "rt-test")
	t.Setenv("VIBE_FENCE_ROOT", fenceRoot)
	lease := map[string]any{"service": "default-blob", "authority": "blob-main", "runtime_id": "rt-test", "epoch": 1}
	b, _ := json.Marshal(lease)
	if err := os.WriteFile(filepath.Join(fenceRoot, "default-blob--blob-main.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	return protocol.Envelope{
		Protocol: 1, MessageID: "m", Kind: protocol.KindCommand,
		Service: "default-blob", Authority: "blob-main", FencingEpoch: 1,
	}
}

func TestPutIsContentAddressedAndIdempotent(t *testing.T) {
	dir := t.TempDir()
	s := &store{root: dir}
	env := fenced(t, dir)
	payload := []byte("hello M1.1")

	env.Payload = protocol.NewPayload(map[string]string{"content_base64": base64.StdEncoding.EncodeToString(payload)})
	out1, perr := putHandler(s)(env)
	if perr != nil {
		t.Fatalf("put: %+v", perr)
	}
	r1 := out1.(putResponse)
	if r1.Existed || r1.Size != len(payload) || r1.URI == "" {
		t.Fatalf("first put: %+v", r1)
	}

	out2, _ := putHandler(s)(env)
	r2 := out2.(putResponse)
	if !r2.Existed || r2.URI != r1.URI {
		t.Fatalf("second put of same bytes must report existed with the same uri: %+v", r2)
	}

	getEnv := protocol.Envelope{Payload: protocol.NewPayload(map[string]string{"uri": r1.URI})}
	gout, gperr := getHandler(s)(getEnv)
	if gperr != nil {
		t.Fatalf("get: %+v", gperr)
	}
	got, _ := base64.StdEncoding.DecodeString(gout.(getResponse).ContentBase64)
	if string(got) != string(payload) {
		t.Fatalf("round-trip mismatch: %q", got)
	}
}

func TestGetUnknownURIIsNotFound(t *testing.T) {
	dir := t.TempDir()
	s := &store{root: dir}
	_, perr := getHandler(s)(protocol.Envelope{Payload: protocol.NewPayload(map[string]string{"uri": "blob://sha256/" + "00"+string(make([]byte,0)) + "aa"})})
	if perr == nil || perr.Code != "NOT_FOUND" {
		t.Fatalf("want NOT_FOUND for unknown uri, got %+v", perr)
	}
}
```

- [ ] **Step 2: run it to verify it fails**

Run: `cd <repo-root> && go test ./plugins/foundation/blob/...`
Expected: FAIL — `undefined: store`, `undefined: putHandler`, etc.

- [ ] **Step 3: write the implementation**

`plugins/foundation/blob/main.go`:

```go
// org.vibe.blob — content-addressed byte store. Knows nothing about Task, Agent,
// Diff or Session; it maps bytes <-> blob://sha256/<hex> URIs.
package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/example/agent-native-microkernel/sdk/go/fencing"
	"github.com/example/agent-native-microkernel/sdk/go/pluginhost"
	"github.com/example/agent-native-microkernel/sdk/go/protocol"
)

const uriPrefix = "blob://sha256/"

type store struct{ root string }

func (s *store) objectsDir() string { return filepath.Join(s.root, "objects", "sha256") }

func (s *store) pathFor(hexsum string) string {
	return filepath.Join(s.objectsDir(), hexsum[:2], hexsum)
}

func hexFromURI(uri string) (string, bool) {
	if !strings.HasPrefix(uri, uriPrefix) {
		return "", false
	}
	h := strings.TrimPrefix(uri, uriPrefix)
	if len(h) != 64 {
		return "", false
	}
	if _, err := hex.DecodeString(h); err != nil {
		return "", false
	}
	return h, true
}

type putRequest struct {
	ContentBase64 string `json:"content_base64"`
}
type putResponse struct {
	URI     string `json:"uri"`
	SHA256  string `json:"sha256"`
	Size    int    `json:"size"`
	Existed bool   `json:"existed"`
}
type getRequest struct {
	URI string `json:"uri"`
}
type getResponse struct {
	ContentBase64 string `json:"content_base64"`
	Size          int    `json:"size"`
}
type statResponse struct {
	Exists bool `json:"exists"`
	Size   int  `json:"size"`
}

func putHandler(s *store) pluginhost.Handler {
	return func(e protocol.Envelope) (any, *protocol.Error) {
		var q putRequest
		if json.Unmarshal(e.Payload, &q) != nil {
			return nil, &protocol.Error{Code: "INVALID", Message: "bad request"}
		}
		raw, derr := base64.StdEncoding.DecodeString(q.ContentBase64)
		if derr != nil {
			return nil, &protocol.Error{Code: "INVALID", Message: "content_base64 is not valid base64"}
		}
		sum := sha256.Sum256(raw)
		hexsum := hex.EncodeToString(sum[:])
		dst := s.pathFor(hexsum)

		var resp putResponse
		err := fencing.WithWriteFence(e, func() error {
			if fi, statErr := os.Stat(dst); statErr == nil {
				resp = putResponse{URI: uriPrefix + hexsum, SHA256: hexsum, Size: int(fi.Size()), Existed: true}
				return nil
			}
			if mkErr := os.MkdirAll(filepath.Dir(dst), 0o755); mkErr != nil {
				return mkErr
			}
			tmp := dst + ".tmp"
			f, oErr := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
			if oErr != nil {
				return oErr
			}
			_, wErr := f.Write(raw)
			if wErr == nil {
				wErr = f.Sync()
			}
			cErr := f.Close()
			if wErr != nil {
				_ = os.Remove(tmp)
				return wErr
			}
			if cErr != nil {
				_ = os.Remove(tmp)
				return cErr
			}
			if rErr := os.Rename(tmp, dst); rErr != nil {
				_ = os.Remove(tmp)
				return rErr
			}
			resp = putResponse{URI: uriPrefix + hexsum, SHA256: hexsum, Size: len(raw), Existed: false}
			return nil
		})
		if err != nil {
			return nil, &protocol.Error{Code: "IO", Message: err.Error(), Retryable: true}
		}
		return resp, nil
	}
}

func getHandler(s *store) pluginhost.Handler {
	return func(e protocol.Envelope) (any, *protocol.Error) {
		var q getRequest
		_ = json.Unmarshal(e.Payload, &q)
		h, ok := hexFromURI(q.URI)
		if !ok {
			return nil, &protocol.Error{Code: "INVALID", Message: "uri must be blob://sha256/<64-hex>"}
		}
		raw, err := os.ReadFile(s.pathFor(h))
		if os.IsNotExist(err) {
			return nil, &protocol.Error{Code: "NOT_FOUND", Message: "no such blob"}
		}
		if err != nil {
			return nil, &protocol.Error{Code: "IO", Message: err.Error()}
		}
		return getResponse{ContentBase64: base64.StdEncoding.EncodeToString(raw), Size: len(raw)}, nil
	}
}

func statHandler(s *store) pluginhost.Handler {
	return func(e protocol.Envelope) (any, *protocol.Error) {
		var q getRequest
		_ = json.Unmarshal(e.Payload, &q)
		h, ok := hexFromURI(q.URI)
		if !ok {
			return nil, &protocol.Error{Code: "INVALID", Message: "uri must be blob://sha256/<64-hex>"}
		}
		fi, err := os.Stat(s.pathFor(h))
		if os.IsNotExist(err) {
			return statResponse{Exists: false}, nil
		}
		if err != nil {
			return nil, &protocol.Error{Code: "IO", Message: err.Error()}
		}
		return statResponse{Exists: true, Size: int(fi.Size())}, nil
	}
}

func main() {
	root := os.Getenv("VIBE_DATA_DIR")
	if err := os.MkdirAll(root, 0o755); err != nil {
		panic(err)
	}
	s := &store{root: root}
	h := pluginhost.New("org.vibe.blob", "1.0.0", "")
	h.HandleContextCommand("blob.put", 1, func(_ *pluginhost.RequestContext, e protocol.Envelope) (any, *protocol.Error) {
		return putHandler(s)(e)
	})
	h.HandleQuery("blob.get", 1, getHandler(s))
	h.HandleQuery("blob.stat", 1, statHandler(s))
	_ = fmt.Sprint
	_ = h.Serve()
}
```

Fix the `TestGetUnknownURIIsNotFound` URI in the test to a real 64-hex string, e.g. `"blob://sha256/" + strings.Repeat("0", 64)` (add `"strings"` to the test imports).

- [ ] **Step 4: run the test to verify it passes**

Run: `cd <repo-root> && go test ./plugins/foundation/blob/... -v`
Expected: both tests PASS.

- [ ] **Step 5: mutation check**

In `putHandler`, temporarily make the `os.Stat(dst)` "already exists" branch fall through (always rewrite, `Existed: false`). Run `go test ./plugins/foundation/blob/... -run TestPutIsContentAddressedAndIdempotent` — expect FAIL (`existed` false on the second put). Restore.

- [ ] **Step 6: manifest + build wiring**

`plugins/manifests/blob.manifest.json`:

```json
{
  "manifest_version": 1,
  "plugin": { "id": "org.vibe.blob", "version": "1.0.0" },
  "runtime": {
    "protocol": "vibe-plugin/1",
    "executable": "../bin/blob",
    "isolation": "process",
    "data_namespace": "state-authority/blob-main"
  },
  "exports": [
    { "capability": "blob.put", "major": 1, "contract": "blob.put@1", "mode": "stateful", "service": "default-blob", "authority": "blob-main", "priority": 100 },
    { "capability": "blob.get", "major": 1, "contract": "blob.get@1", "mode": "stateful", "service": "default-blob", "authority": "blob-main", "priority": 100 },
    { "capability": "blob.stat", "major": 1, "contract": "blob.stat@1", "mode": "stateful", "service": "default-blob", "authority": "blob-main", "priority": 100 }
  ],
  "restart": { "mode": "on_failure", "max_attempts": 2, "cooldown_ms": 100 },
  "resources": { "memory_mb": 128, "cpu_weight": 20 }
}
```

`scripts/build.sh` already loops `plugins/foundation/*` and `plugins/*/` for any dir with `main.go` (skipping `_template`), so `blob` and later `work-registry` are picked up with no change. Confirm by reading the script; if the loop does not in fact cover `plugins/foundation/*`, add it.

- [ ] **Step 7: commit**

```
feat(m1.1): org.vibe.blob content-addressed store
```

---

## Task 3: work-registry contracts

**Files:** create four schemas; modify `contracts/catalog.json`.

Data shapes used across all four (define once here, reference in each schema):

- `AcceptanceCriterion` = `{ "id": string, "text": string }`
- `EvidenceRef` = `{ "id": string, "kind": string, "source_capability": string, "source_id": string, "outcome": string, "observed_at": string, "content_hash": string, "invalidated_at": string|null }`
- `Task` = `{ "id": string, "title": string, "goal": string, "scope": string, "acceptance_criteria": [AcceptanceCriterion], "status": string, "version": integer, "work_context_id": string }`
- `WorkContext` = `{ "id": string, "task_id": string, "repo": string, "active_workspace_ref": object|null, "evidence_refs": [EvidenceRef], "version": integer }`

- [ ] **Step 1: `contracts/work.create/v1/schema.json`**

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "contract": "work.create@1",
  "version": "1.0.0",
  "kind": "command",
  "compatibility": "backward-within-major",
  "request": {
    "type": "object",
    "additionalProperties": false,
    "properties": {
      "title": { "type": "string" },
      "goal": { "type": "string" },
      "scope": { "type": "string" },
      "repo": { "type": "string" },
      "acceptance_criteria": {
        "type": "array",
        "items": {
          "type": "object",
          "additionalProperties": false,
          "properties": { "id": { "type": "string" }, "text": { "type": "string" } },
          "required": ["id", "text"]
        }
      }
    },
    "required": ["title", "goal", "repo"]
  },
  "response": {
    "type": "object",
    "additionalProperties": true,
    "properties": {
      "task": { "type": "object" },
      "work_context": { "type": "object" },
      "idempotent_replay": { "type": "boolean" }
    },
    "required": ["task", "work_context"]
  }
}
```

(Keep `task` / `work_context` as bare `{"type":"object"}` in the schema — the exact shape is documented above and enforced by the Go types + tests, not by the JSON Schema, to avoid a large nested schema. `additionalProperties: true` on the response.)

- [ ] **Step 2: `contracts/work.get/v1/schema.json`** — `kind` `"query"`, request:

```json
  "request": {
    "type": "object",
    "additionalProperties": false,
    "properties": { "task_id": { "type": "string" }, "work_context_id": { "type": "string" } },
    "required": []
  }
```

response: `{ "task": object, "work_context": object }` both required, `additionalProperties: true`. (Handler requires exactly one of `task_id` / `work_context_id`.)

- [ ] **Step 3: `contracts/work.transition/v1/schema.json`** — `kind` `"command"`, request:

```json
  "request": {
    "type": "object",
    "additionalProperties": false,
    "properties": {
      "work_context_id": { "type": "string" },
      "to": { "type": "string", "enum": ["IN_PROGRESS", "IN_REVIEW", "DONE", "FAILED"] },
      "expected_version": { "type": "integer" }
    },
    "required": ["work_context_id", "to", "expected_version"]
  }
```

response: `{ "task": object, "work_context": object }` both required.

- [ ] **Step 4: `contracts/work.attach-evidence/v1/schema.json`** — `contract` `"work.attach_evidence@1"`, `kind` `"command"`, request:

```json
  "request": {
    "type": "object",
    "additionalProperties": false,
    "properties": {
      "work_context_id": { "type": "string" },
      "kind": { "type": "string", "enum": ["build", "test", "review"] },
      "source_capability": { "type": "string" },
      "source_id": { "type": "string" },
      "outcome": { "type": "string", "enum": ["PASS", "FAIL"] },
      "content_hash": { "type": "string" },
      "expected_version": { "type": "integer" }
    },
    "required": ["work_context_id", "kind", "source_capability", "source_id", "outcome", "expected_version"]
  }
```

response: `{ "evidence_ref": object, "work_context": object }` both required.

Note the directory is `work.attach-evidence` (hyphen; a `.` in the capability name maps to the directory name up to the last segment — keep it simple: use `work.attach-evidence/v1/schema.json` as the path and `work.attach_evidence@1` as the identity).

- [ ] **Step 5: catalog + check**

Add to `contracts/catalog.json`:

```json
  "work.create@1": "work.create/v1/schema.json",
  "work.get@1": "work.get/v1/schema.json",
  "work.transition@1": "work.transition/v1/schema.json",
  "work.attach_evidence@1": "work.attach-evidence/v1/schema.json"
```

Run: `python3 scripts/check-contracts.py --root contracts` → `CONTRACT CHECK: PASSED` (count = entries in catalog.json now: 5 + 4 = 9).

- [ ] **Step 6: commit**

```
build(m1.1): work.create/get/transition/attach_evidence contracts
```

---

## Task 4: work-registry store + projection

**Files:** `plugins/work-registry/store.go`, `plugins/work-registry/store_test.go`.

**Interfaces:**
- Produces: `type Store` with `Load(dir string) (*Store, error)` (opens `dir/work-log.jsonl`, replays it, returns the projection), and mutation methods `CreateTask`, `Transition`, `AttachEvidence`, plus read `GetByTask` / `GetByContext`. Every mutation appends one JSONL record (fsync) then applies it in memory; a partial/corrupt trailing line on replay is ignored (not fatal). Types: `Task`, `WorkContext`, `EvidenceRef`, `AcceptanceCriterion`, `Status` (string constants `StatusPlanned` … `StatusFailed`).

- [ ] **Step 1: write the failing test**

`plugins/work-registry/store_test.go`:

```go
package main

import (
	"path/filepath"
	"testing"
)

func newStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return s, dir
}

func TestCreateThenReadBack(t *testing.T) {
	s, _ := newStore(t)
	task, wc, replay, err := s.CreateTask(CreateInput{
		Title: "harden add", Goal: "reject illegal input", Repo: "fixtures/sample-java-project",
		Acceptance: []AcceptanceCriterion{{ID: "AC1", Text: "mvn test PASS"}},
		IdempotencyKey: "",
	})
	if err != nil || replay {
		t.Fatalf("create: err=%v replay=%v", err, replay)
	}
	if task.Status != StatusPlanned || task.Version != 1 || task.WorkContextID != wc.ID {
		t.Fatalf("task: %+v", task)
	}
	if wc.TaskID != task.ID || wc.Repo != "fixtures/sample-java-project" || wc.Version != 1 {
		t.Fatalf("wc: %+v", wc)
	}
	got, gwc, ok := s.GetByTask(task.ID)
	if !ok || got.ID != task.ID || gwc.ID != wc.ID {
		t.Fatalf("get: %+v %+v ok=%v", got, gwc, ok)
	}
}

func TestIdempotentCreate(t *testing.T) {
	s, _ := newStore(t)
	in := CreateInput{Title: "x", Goal: "y", Repo: "r", IdempotencyKey: "k1"}
	t1, _, _, _ := s.CreateTask(in)
	t2, _, replay, _ := s.CreateTask(in)
	if !replay || t2.ID != t1.ID {
		t.Fatalf("idempotent create should return the same task: replay=%v %s vs %s", replay, t1.ID, t2.ID)
	}
}

func TestProjectionRebuildsFromLog(t *testing.T) {
	s, dir := newStore(t)
	task, wc, _, _ := s.CreateTask(CreateInput{Title: "x", Goal: "y", Repo: "r"})
	if _, _, err := s.Transition(wc.ID, StatusInProgress, task.Version); err != nil {
		t.Fatalf("transition: %v", err)
	}

	reloaded, err := Load(dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	got, _, ok := reloaded.GetByTask(task.ID)
	if !ok || got.Status != StatusInProgress || got.Version != 2 {
		t.Fatalf("projection not rebuilt: %+v ok=%v", got, ok)
	}
	_ = filepath.Join
}
```

- [ ] **Step 2: run it to verify it fails**

Run: `cd <repo-root> && go test ./plugins/work-registry/...`
Expected: FAIL — `undefined: Store`, `undefined: Load`, etc.

- [ ] **Step 3: write `store.go`**

Implement:

- `Status` constants: `StatusPlanned="PLANNED"`, `StatusInProgress="IN_PROGRESS"`, `StatusInReview="IN_REVIEW"`, `StatusDone="DONE"`, `StatusFailed="FAILED"`.
- Structs `AcceptanceCriterion`, `EvidenceRef`, `Task`, `WorkContext` matching the JSON shapes in Task 3.
- `CreateInput { Title, Goal, Scope, Repo string; Acceptance []AcceptanceCriterion; IdempotencyKey string }`.
- `logRecord { Seq int64; TS string; Op string; Data json.RawMessage }`, `Op` ∈ `"task.created"`, `"work.transitioned"`, `"evidence.attached"`.
- `Store` fields: `mu sync.Mutex`, `path string`, `seq int64`, `tasks map[string]*Task`, `wcs map[string]*WorkContext`, `taskByWC map[string]string`, `idem map[string]string` (key → task id).
- `Load(dir)`: open `dir/work-log.jsonl` if it exists; scan line by line; `json.Unmarshal` each into `logRecord`; on unmarshal error for the **last** line only, stop (partial write) — for any earlier line, return an error; `apply(rec)` for each; track max seq.
- `apply(rec)`: switch on `Op`; build/mutate `tasks`/`wcs`/`idem`. This is the single reducer used by both replay and live mutation.
- `append(op string, data any) error`: `seq++`; marshal a `logRecord`; `OpenFile(O_APPEND|O_CREATE|O_WRONLY)`; write line + `\n`; `f.Sync()`; propagate any write/sync error even if `Close` succeeds (same pattern as `event-journal`); then `apply` the record in memory.
- `CreateTask(in)`: lock; if `in.IdempotencyKey != ""` and present in `idem` → return existing task/wc with `replay=true`; else generate ids (`protocol.NewID` is fine — import `sdk/go/protocol`; or a local `randID` using `crypto/rand`), build the `task.created` data `{task, work_context, idempotency_key}`, `append`, return `replay=false`.
- `Transition(wcID string, to Status, expectedVersion int) (*Task, *WorkContext, error)`: lock; look up task via `taskByWC`; if not found → `ErrNotFound`; if `task.Version != expectedVersion` → `ErrConflict`; if `!legalTransition(task.Status, to)` → `ErrIllegalTransition`; build `work.transitioned` data `{work_context_id, from, to, task_version_after: task.Version+1}`, `append`, return updated task/wc.
- `legalTransition(from, to Status) bool`: `PLANNED→IN_PROGRESS`, `IN_PROGRESS→IN_REVIEW`, `IN_REVIEW→DONE`, and `{PLANNED,IN_PROGRESS,IN_REVIEW}→FAILED`. Everything else false.
- `AttachEvidence(wcID string, ev EvidenceRef, expectedWCVersion int) (*EvidenceRef, *WorkContext, error)`: lock; look up wc; if `wc.Version != expectedWCVersion` → `ErrConflict`; assign `ev.ID`, `ev.ObservedAt = time.Now().UTC().RFC3339Nano`, `ev.InvalidatedAt = nil`; `append` `evidence.attached` data `{work_context_id, evidence_ref}`; in `apply`, append to `wc.EvidenceRefs` and `wc.Version++`.
- Exported sentinel errors: `ErrNotFound`, `ErrConflict`, `ErrIllegalTransition`.

`apply` for `task.created` must also set `idem[key]=task.ID` when the record carries a non-empty `idempotency_key`, so idempotency survives a restart.

- [ ] **Step 4: run the tests to verify they pass**

Run: `cd <repo-root> && go test ./plugins/work-registry/... -v`
Expected: `TestCreateThenReadBack`, `TestIdempotentCreate`, `TestProjectionRebuildsFromLog` all PASS.

- [ ] **Step 5: mutation check**

Change `append` to skip `f.Sync()`. `TestProjectionRebuildsFromLog` still passes (same process), so instead: change `Load` to start `seq` at 0 always (ignore replayed max). Run `go test ./plugins/work-registry/... -run TestProjectionRebuildsFromLog` — the reload's next `append` would collide seqs; assert the test still catches a wrong `Version`. If it does not, add an assertion to the test that `reloaded` can `Transition` again to `IN_REVIEW` and the version becomes 3. Restore.

- [ ] **Step 6: commit**

```
feat(m1.1): work-registry store — JSONL log + projection
```

---

## Task 5: work-registry `work.create` + `work.get` handlers

**Files:** `plugins/work-registry/handlers.go`, `plugins/work-registry/handlers_test.go`.

**Interfaces:**
- Produces: `createHandler(s *Store) pluginhost.Handler`, `getHandler(s *Store) pluginhost.Handler`. `create` is a fenced stateful command (service `default-work-registry`, authority `work-main`); `get` is a stateful query.

- [ ] **Step 1: write the failing test**

`plugins/work-registry/handlers_test.go`:

```go
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/example/agent-native-microkernel/sdk/go/protocol"
)

func fencedEnv(t *testing.T, dir string) protocol.Envelope {
	t.Helper()
	fenceRoot := filepath.Join(dir, ".fences")
	_ = os.MkdirAll(fenceRoot, 0o755)
	t.Setenv("VIBE_DATA_DIR", dir)
	t.Setenv("VIBE_RUNTIME_ID", "rt-test")
	t.Setenv("VIBE_FENCE_ROOT", fenceRoot)
	lease := map[string]any{"service": "default-work-registry", "authority": "work-main", "runtime_id": "rt-test", "epoch": 1}
	b, _ := json.Marshal(lease)
	_ = os.WriteFile(filepath.Join(fenceRoot, "default-work-registry--work-main.json"), b, 0o644)
	return protocol.Envelope{
		Protocol: 1, MessageID: "m", Kind: protocol.KindCommand,
		Service: "default-work-registry", Authority: "work-main", FencingEpoch: 1,
	}
}

func TestCreateHandlerRejectsMissingRequired(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	env := fencedEnv(t, dir)
	env.Payload = protocol.NewPayload(map[string]any{"title": "x"}) // no goal, no repo
	_, perr := createHandler(s)(env)
	if perr == nil || perr.Code != "INVALID" {
		t.Fatalf("want INVALID, got %+v", perr)
	}
}

func TestCreateThenGetByTaskAndByContext(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	env := fencedEnv(t, dir)
	env.Payload = protocol.NewPayload(map[string]any{
		"title": "harden add", "goal": "reject bad input", "repo": "fixtures/sample-java-project",
		"acceptance_criteria": []map[string]string{{"id": "AC1", "text": "mvn test PASS"}},
	})
	out, perr := createHandler(s)(env)
	if perr != nil {
		t.Fatalf("create: %+v", perr)
	}
	var cr struct {
		Task        Task        `json:"task"`
		WorkContext WorkContext `json:"work_context"`
	}
	b, _ := json.Marshal(out)
	_ = json.Unmarshal(b, &cr)
	if cr.Task.Status != StatusPlanned || len(cr.Task.AcceptanceCriteria) != 1 {
		t.Fatalf("created task: %+v", cr.Task)
	}

	for _, q := range []map[string]string{{"task_id": cr.Task.ID}, {"work_context_id": cr.WorkContext.ID}} {
		gout, gperr := getHandler(s)(protocol.Envelope{Payload: protocol.NewPayload(q)})
		if gperr != nil {
			t.Fatalf("get %v: %+v", q, gperr)
		}
		gb, _ := json.Marshal(gout)
		var gr struct{ Task Task `json:"task"` }
		_ = json.Unmarshal(gb, &gr)
		if gr.Task.ID != cr.Task.ID {
			t.Fatalf("get %v returned %s", q, gr.Task.ID)
		}
	}
}

func TestGetRequiresExactlyOneSelector(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	_, perr := getHandler(s)(protocol.Envelope{Payload: protocol.NewPayload(map[string]string{})})
	if perr == nil || perr.Code != "INVALID" {
		t.Fatalf("want INVALID for no selector, got %+v", perr)
	}
	_, perr = getHandler(s)(protocol.Envelope{Payload: protocol.NewPayload(map[string]string{"task_id": "unknown"})})
	if perr == nil || perr.Code != "NOT_FOUND" {
		t.Fatalf("want NOT_FOUND for unknown task, got %+v", perr)
	}
}
```

- [ ] **Step 2: run it to verify it fails** — `undefined: createHandler`, `undefined: getHandler`.

- [ ] **Step 3: implement `handlers.go`** (create + get only for this task):

- `createRequest` struct with `Title, Goal, Scope, Repo string; AcceptanceCriteria []AcceptanceCriterion`.
- `createHandler`: unmarshal; if `Title==""||Goal==""||Repo==""` → `INVALID`; call `s.CreateTask(...)` inside `fencing.WithWriteFence(e, func() error { ... })` — capture task/wc/replay; on `err` → `{Code:"IO", Retryable:true}`; return `map[string]any{"task": task, "work_context": wc, "idempotent_replay": replay}` (or a typed struct). Use `e.IdempotencyKey` as the `CreateInput.IdempotencyKey`.
- `getRequest` struct `{ TaskID, WorkContextID string }`; if both empty or both set → `INVALID`; look up via `s.GetByTask` / `s.GetByContext`; not found → `NOT_FOUND`; return `{task, work_context}`.

- [ ] **Step 4: run the tests to verify they pass** — all three PASS.

- [ ] **Step 5: commit**

```
feat(m1.1): work.create + work.get handlers
```

---

## Task 6: work-registry `work.transition` state machine

**Files:** modify `plugins/work-registry/handlers.go`, `plugins/work-registry/handlers_test.go`.

- [ ] **Step 1: write the failing tests** (append to `handlers_test.go`):

```go
func createForTransition(t *testing.T, s *Store, dir string) (Task, WorkContext) {
	t.Helper()
	env := fencedEnv(t, dir)
	env.Payload = protocol.NewPayload(map[string]any{"title": "x", "goal": "y", "repo": "r"})
	out, perr := createHandler(s)(env)
	if perr != nil {
		t.Fatal(perr)
	}
	b, _ := json.Marshal(out)
	var cr struct {
		Task        Task        `json:"task"`
		WorkContext WorkContext `json:"work_context"`
	}
	_ = json.Unmarshal(b, &cr)
	return cr.Task, cr.WorkContext
}

func transition(t *testing.T, s *Store, dir, wcID, to string, expVer int) *protocol.Error {
	t.Helper()
	env := fencedEnv(t, dir)
	env.Payload = protocol.NewPayload(map[string]any{"work_context_id": wcID, "to": to, "expected_version": expVer})
	_, perr := transitionHandler(s)(env)
	return perr
}

func TestTransitionHappyPath(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	task, wc := createForTransition(t, s, dir)
	if perr := transition(t, s, dir, wc.ID, "IN_PROGRESS", task.Version); perr != nil {
		t.Fatalf("PLANNED->IN_PROGRESS: %+v", perr)
	}
	if perr := transition(t, s, dir, wc.ID, "IN_REVIEW", task.Version+1); perr != nil {
		t.Fatalf("IN_PROGRESS->IN_REVIEW: %+v", perr)
	}
	if perr := transition(t, s, dir, wc.ID, "DONE", task.Version+2); perr != nil {
		t.Fatalf("IN_REVIEW->DONE: %+v", perr)
	}
}

func TestTransitionRejectsIllegalJump(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	task, wc := createForTransition(t, s, dir)
	perr := transition(t, s, dir, wc.ID, "DONE", task.Version) // PLANNED -> DONE
	if perr == nil || perr.Code != "ILLEGAL_TRANSITION" {
		t.Fatalf("PLANNED->DONE must be rejected, got %+v", perr)
	}
}

func TestTransitionRejectsStaleVersion(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	task, wc := createForTransition(t, s, dir)
	_ = transition(t, s, dir, wc.ID, "IN_PROGRESS", task.Version) // version now 2
	perr := transition(t, s, dir, wc.ID, "IN_REVIEW", task.Version) // stale (1)
	if perr == nil || perr.Code != "CONFLICT" {
		t.Fatalf("stale expected_version must be CONFLICT, got %+v", perr)
	}
}

func TestTransitionRequiresExpectedVersion(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	_, wc := createForTransition(t, s, dir)
	env := fencedEnv(t, dir)
	env.Payload = protocol.NewPayload(map[string]any{"work_context_id": wc.ID, "to": "IN_PROGRESS"}) // no expected_version
	_, perr := transitionHandler(s)(env)
	if perr == nil || perr.Code != "INVALID" {
		t.Fatalf("missing expected_version must be INVALID, got %+v", perr)
	}
}
```

- [ ] **Step 2: run to verify failure** — `undefined: transitionHandler`.

- [ ] **Step 3: implement `transitionHandler`**

- `transitionRequest { WorkContextID, To string; ExpectedVersion *int }` — pointer so "absent" is distinguishable from `0`.
- validate: `WorkContextID==""` or `To` not one of the four statuses or `ExpectedVersion == nil` → `INVALID`.
- inside `fencing.WithWriteFence`: `s.Transition(WorkContextID, Status(To), *ExpectedVersion)`.
- map store errors: `ErrNotFound → NOT_FOUND`, `ErrConflict → CONFLICT` (not retryable), `ErrIllegalTransition → ILLEGAL_TRANSITION` (not retryable), other → `{IO, retryable}`.
- success → `{task, work_context}`.

- [ ] **Step 4: run to verify all pass.**

- [ ] **Step 5: mutation check** — in `legalTransition`, add `PLANNED→DONE` as legal. Run `go test ./plugins/work-registry/... -run TestTransitionRejectsIllegalJump` → FAIL. Restore. Then in `transitionHandler`, drop the `ExpectedVersion == nil` check → `TestTransitionRequiresExpectedVersion` FAIL. Restore.

- [ ] **Step 6: commit**

```
feat(m1.1): work.transition state machine + required expected_version
```

---

## Task 7: work-registry `work.attach_evidence` + plugin wiring

**Files:** modify `plugins/work-registry/handlers.go`, `plugins/work-registry/handlers_test.go`; create `plugins/work-registry/main.go`, `plugins/manifests/work-registry.manifest.json`.

- [ ] **Step 1: write the failing test** (append):

```go
func TestAttachEvidenceAppendsRefAndBumpsWCVersion(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	_, wc := createForTransition(t, s, dir)

	env := fencedEnv(t, dir)
	env.Payload = protocol.NewPayload(map[string]any{
		"work_context_id": wc.ID, "kind": "test", "source_capability": "tool.run@1",
		"source_id": "TR-1", "outcome": "PASS", "content_hash": "abc", "expected_version": wc.Version,
	})
	out, perr := attachEvidenceHandler(s)(env)
	if perr != nil {
		t.Fatalf("attach: %+v", perr)
	}
	b, _ := json.Marshal(out)
	var r struct {
		EvidenceRef EvidenceRef `json:"evidence_ref"`
		WorkContext WorkContext `json:"work_context"`
	}
	_ = json.Unmarshal(b, &r)
	if r.EvidenceRef.Kind != "test" || r.EvidenceRef.Outcome != "PASS" || r.EvidenceRef.ID == "" {
		t.Fatalf("evidence ref: %+v", r.EvidenceRef)
	}
	if len(r.WorkContext.EvidenceRefs) != 1 || r.WorkContext.Version != wc.Version+1 {
		t.Fatalf("work context: %+v", r.WorkContext)
	}
}

func TestAttachEvidenceStaleVersionConflicts(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	_, wc := createForTransition(t, s, dir)
	env := fencedEnv(t, dir)
	base := map[string]any{"work_context_id": wc.ID, "kind": "build", "source_capability": "tool.run@1", "source_id": "TR-x", "outcome": "PASS"}
	base["expected_version"] = wc.Version
	env.Payload = protocol.NewPayload(base)
	_, _ = attachEvidenceHandler(s)(env) // wc.Version now +1
	base["expected_version"] = wc.Version // stale
	env.Payload = protocol.NewPayload(base)
	_, perr := attachEvidenceHandler(s)(env)
	if perr == nil || perr.Code != "CONFLICT" {
		t.Fatalf("stale wc version must CONFLICT, got %+v", perr)
	}
}
```

- [ ] **Step 2: run to verify failure.**

- [ ] **Step 3: implement `attachEvidenceHandler`** — validate required fields + `kind ∈ {build,test,review}` + `outcome ∈ {PASS,FAIL}` + `expected_version` present; inside fence call `s.AttachEvidence(wcID, EvidenceRef{Kind, SourceCapability, SourceID, Outcome, ContentHash}, expectedVersion)`; map `ErrNotFound`/`ErrConflict`; return `{evidence_ref, work_context}`.

- [ ] **Step 4: write `main.go`**

```go
package main

import (
	"os"

	"github.com/example/agent-native-microkernel/sdk/go/pluginhost"
	"github.com/example/agent-native-microkernel/sdk/go/protocol"
)

func main() {
	dir := os.Getenv("VIBE_DATA_DIR")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		panic(err)
	}
	s, err := Load(dir)
	if err != nil {
		panic("work-registry load: " + err.Error())
	}
	h := pluginhost.New("org.vibe.work.registry", "1.0.0", "")
	h.HandleContextCommand("work.create", 1, wrap(createHandler(s)))
	h.HandleQuery("work.get", 1, getHandler(s))
	h.HandleContextCommand("work.transition", 1, wrap(transitionHandler(s)))
	h.HandleContextCommand("work.attach_evidence", 1, wrap(attachEvidenceHandler(s)))
	_ = h.Serve()
}

func wrap(fn pluginhost.Handler) pluginhost.ContextHandler {
	return func(_ *pluginhost.RequestContext, e protocol.Envelope) (any, *protocol.Error) { return fn(e) }
}
```

- [ ] **Step 5: `plugins/manifests/work-registry.manifest.json`**

```json
{
  "manifest_version": 1,
  "plugin": { "id": "org.vibe.work.registry", "version": "1.0.0" },
  "runtime": {
    "protocol": "vibe-plugin/1",
    "executable": "../bin/work-registry",
    "isolation": "process",
    "data_namespace": "state-authority/work-main"
  },
  "exports": [
    { "capability": "work.create", "major": 1, "contract": "work.create@1", "mode": "stateful", "service": "default-work-registry", "authority": "work-main", "priority": 100 },
    { "capability": "work.get", "major": 1, "contract": "work.get@1", "mode": "stateful", "service": "default-work-registry", "authority": "work-main", "priority": 100 },
    { "capability": "work.transition", "major": 1, "contract": "work.transition@1", "mode": "stateful", "service": "default-work-registry", "authority": "work-main", "priority": 100 },
    { "capability": "work.attach_evidence", "major": 1, "contract": "work.attach_evidence@1", "mode": "stateful", "service": "default-work-registry", "authority": "work-main", "priority": 100 }
  ],
  "restart": { "mode": "on_failure", "max_attempts": 2, "cooldown_ms": 100 },
  "resources": { "memory_mb": 128, "cpu_weight": 20 }
}
```

- [ ] **Step 6: build + composition check**

Run: `cd <repo-root> && bash scripts/build.sh` → expect `built plugin: blob`, `built plugin: work-registry`, `built plugin: event-journal`, `BUILD OK`.
Run: `python3 architecture-tests/check_composition.py` → `COMPOSITION FITNESS: PASSED` (manifest count now 3).

- [ ] **Step 7: commit**

```
feat(m1.1): work.attach_evidence + work-registry plugin wiring
```

---

## Task 8: `vibe` CLI

**Files:** `cli/go.mod`, `cli/vibe/wire.go`, `cli/vibe/wire_test.go`, `cli/vibe/main.go`; modify `go.work`, `scripts/build.sh`.

**Interfaces:**
- Produces: a binary `vibe` with subcommands `task create`, `task show`, `task transition`. It dials the kernel's Unix socket, sends `{identity, token, envelope}` as one JSON line, reads one JSON envelope back.

- [ ] **Step 1: workspace + module**

`cli/go.mod`:

```
module github.com/example/agent-native-os/cli

go 1.19

require github.com/example/agent-native-microkernel v0.0.0
```

`go.work` — add `./cli` to the `use (...)` block.

Run: `cd <repo-root> && cd cli && go mod tidy` (no external deps; writes a minimal `go.sum`), then `cd .. && go build ./cli/...` (will fail until `main.go` exists — that's fine, this step just sets up the module).

- [ ] **Step 2: write the failing test for the wire client**

`cli/vibe/wire_test.go`:

```go
package main

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/example/agent-native-microkernel/sdk/go/protocol"
)

// A fake kernel gateway: accepts one line {identity,token,envelope}, echoes a result.
func fakeGateway(t *testing.T) string {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "k.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		var wr struct {
			Identity string            `json:"identity"`
			Token    string            `json:"token"`
			Envelope protocol.Envelope `json:"envelope"`
		}
		_ = json.NewDecoder(c).Decode(&wr)
		resp := protocol.Envelope{
			Protocol: 1, Kind: protocol.KindResult, ReplyTo: wr.Envelope.MessageID,
			Payload: protocol.NewPayload(map[string]any{"seen_identity": wr.Identity, "cap": wr.Envelope.Capability}),
		}
		_ = json.NewEncoder(c).Encode(resp)
	}()
	return sock
}

func TestInvokeSendsIdentityAndClearsTCBFields(t *testing.T) {
	sock := fakeGateway(t)
	req := protocol.Envelope{Kind: protocol.KindCommand, Capability: "work.create", Major: 1, Principal: "attacker", Caller: "attacker"}
	resp, err := invoke(sock, "local-cli", "tok", req)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	var p map[string]any
	_ = json.Unmarshal(resp.Payload, &p)
	if p["seen_identity"] != "local-cli" || p["cap"] != "work.create" {
		t.Fatalf("gateway saw: %v", p)
	}
	_ = os.Stdout
}
```

- [ ] **Step 3: run to verify failure** — `undefined: invoke`.

- [ ] **Step 4: implement `cli/vibe/wire.go`**

```go
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

// invoke sends one request to the kernel gateway and returns the single response
// envelope. TCB identity fields are the host's to set; the client must not send them.
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
```

- [ ] **Step 5: run to verify the test passes.**

- [ ] **Step 6: implement `cli/vibe/main.go`**

Behaviour:
- Global flags: `-socket` (default `/tmp/agent-native-os-m1.sock`), `-identity` (default `local-cli`), `-token` (default `$VIBE_CLIENT_TOKEN`). Parse these, then dispatch on `os.Args` positional `task <sub>`.
- `vibe task create -title T -goal G [-scope S] -repo R [-ac ID=TEXT ... repeatable]`
  → build `work.create@1` command payload `{title,goal,scope,repo,acceptance_criteria:[{id,text}]}`; `invoke`; on success print `task <task_id>  wc <work_context_id>  status <status>  version <version>`.
- `vibe task show <task-id> [-json]`
  → `work.get@1` query `{task_id}`; `-json` prints `resp.Payload` verbatim (indented); default prints: `id`, `title`, `status`, `version`, `repo`, `acceptance: N criteria`, `evidence: N refs`.
- `vibe task transition <work-context-id> -to STATUS -expected-version N`
  → `work.transition@1` command; print the new `status` / `version`.
- Non-zero exit on any error; print `error: <msg>` to stderr.
- Use `protocol.KindCommand` / `protocol.KindQuery` appropriately and set `Deadline` to `time.Now().Add(30*time.Second)` RFC3339Nano for commands.

No test is required for `main.go` arg parsing in this task (it is exercised end-to-end by the smoke in Task 9); keep the logic thin and delegate to `invoke`.

- [ ] **Step 7: build wiring**

In `scripts/build.sh`, after the kernel build lines, add:

```bash
( cd cli/vibe && go build -o "$OLDPWD/.bin/vibe" . )
echo "built cli: vibe"
```

Run: `cd <repo-root> && bash scripts/build.sh` → expect `built cli: vibe` and `.bin/vibe` exists. Confirm `.bin/vibe` does not collide with the kernel's `.bin/vibe` — **it does**: M1.0's build.sh builds the kernel's `cmd/vibe` to `.bin/vibe`. Rename the kernel client output to `.bin/vibe-raw` in `scripts/build.sh` (change the one line `go build -o "$OLDPWD/.bin/vibe" ./cmd/vibe` → `.bin/vibe-raw`), and update `scripts/smoke.sh`'s references from `.bin/vibe` to `.bin/vibe-raw` for the raw-protocol calls. The product CLI takes the `.bin/vibe` name.

- [ ] **Step 8: commit**

```
feat(m1.1): vibe CLI — task create / show / transition
```

---

## Task 9: policy, bindings, extended smoke

**Files:** modify `config/m1-policy.json`, `config/m1-bindings.json`, `scripts/smoke.sh`; create `docs/PLUGIN-STORAGE-GUIDANCE.md`.

- [ ] **Step 1: policy**

`config/m1-policy.json` — extend `grants.local-cli.capabilities` to:

```json
[
  "event.journal.append@1",
  "event.journal.replay@1",
  "blob.put@1",
  "blob.get@1",
  "blob.stat@1",
  "work.create@1",
  "work.get@1",
  "work.transition@1",
  "work.attach_evidence@1"
]
```

Add a top-level comment is not possible in JSON; instead note in `docs/PLUGIN-STORAGE-GUIDANCE.md` and in the commit message: **M1.1's `local-cli` holds `work.transition@1` for development convenience. M1.6 splits the policy so the qualification identity loses direct `work.transition@1` (design §4.2); only the workflow plugin + its delegation scope will reach it.**

Keep `grants."org.vibe.blob"` and `grants."org.vibe.work.registry"` present with `"capabilities": []` (they export, they don't consume anything yet).

- [ ] **Step 2: bindings**

`config/m1-bindings.json` — add:

```json
{ "capability": "blob.put", "major": 1, "service": "default-blob", "authority": "blob-main" },
{ "capability": "blob.get", "major": 1, "service": "default-blob", "authority": "blob-main" },
{ "capability": "blob.stat", "major": 1, "service": "default-blob", "authority": "blob-main" },
{ "capability": "work.create", "major": 1, "service": "default-work-registry", "authority": "work-main" },
{ "capability": "work.get", "major": 1, "service": "default-work-registry", "authority": "work-main" },
{ "capability": "work.transition", "major": 1, "service": "default-work-registry", "authority": "work-main" },
{ "capability": "work.attach_evidence", "major": 1, "service": "default-work-registry", "authority": "work-main" }
```

- [ ] **Step 3: write the failing smoke additions**

Extend `scripts/smoke.sh` — after the existing event.journal round-trip, before `echo "M1 SMOKE: PASSED"`, add a `work + blob + restart` section:

```bash
# --- M1.1: work-registry + blob + restart survival ---
create_out="$(.bin/vibe -socket "$SOCK" -identity local-cli -token "$TOKEN" \
  task create -title "smoke task" -goal "prove the slice" -repo fixtures/sample-java-project -ac AC1="mvn test PASS")"
echo "$create_out" | grep -q 'status PLANNED' || { echo "FAIL: task create: $create_out"; cat "$DATA/kernel.log"; exit 1; }
TASK_ID="$(echo "$create_out" | sed -n 's/^task \([^ ]*\).*/\1/p')"
WC_ID="$(echo "$create_out" | sed -n 's/.*wc \([^ ]*\).*/\1/p')"

.bin/vibe -socket "$SOCK" -identity local-cli -token "$TOKEN" \
  task transition "$WC_ID" -to IN_PROGRESS -expected-version 1 | grep -q 'IN_PROGRESS' \
  || { echo "FAIL: transition"; exit 1; }

blob_out="$(.bin/vibe-raw -socket "$SOCK" -identity local-cli -token "$TOKEN" \
  -cap blob.put -kind command -service default-blob -authority blob-main \
  -payload "{\"content_base64\":\"$(printf 'diff-bytes' | base64)\"}")"
BLOB_URI="$(echo "$blob_out" | sed -n 's/.*"uri":"\([^"]*\)".*/\1/p')"
[ -n "$BLOB_URI" ] || { echo "FAIL: blob put: $blob_out"; exit 1; }

# Restart the kernel; the Task and its IN_PROGRESS status must survive.
kill $KPID; wait $KPID 2>/dev/null
.bin/vibe-kernel -plugins ./plugins/manifests -policy ./config/m1-policy.json \
  -bindings ./config/m1-bindings.json -contracts ./contracts -socket "$SOCK" \
  >>"$DATA/kernel.log" 2>&1 &
KPID=$!
for _ in $(seq 1 300); do [ -S "$SOCK" ] && break; sleep 0.03; done

show_out="$(.bin/vibe -socket "$SOCK" -identity local-cli -token "$TOKEN" task show "$TASK_ID")"
echo "$show_out" | grep -q 'status.*IN_PROGRESS' || { echo "FAIL: task did not survive restart: $show_out"; cat "$DATA/kernel.log"; exit 1; }

got_blob="$(.bin/vibe-raw -socket "$SOCK" -identity local-cli -token "$TOKEN" \
  -cap blob.get -kind query -service default-blob -authority blob-main -payload "{\"uri\":\"$BLOB_URI\"}")"
echo "$got_blob" | grep -q "$(printf 'diff-bytes' | base64)" || { echo "FAIL: blob did not survive restart: $got_blob"; exit 1; }
```

(Adjust the `-token` handling: `scripts/smoke.sh` already sets `TOKEN='m1-local-cli-token'`; export it as `VIBE_CLIENT_TOKEN` too so the CLI default picks it up, or always pass `-token "$TOKEN"` as shown.)

- [ ] **Step 4: run the smoke** — `bash scripts/smoke.sh` → `M1 SMOKE: PASSED`. Debug per the FAIL messages; common issues: manifest `executable` path, missing binding, `local-cli` missing a grant, `.bin/vibe` vs `.bin/vibe-raw` mixup.

- [ ] **Step 5: write `docs/PLUGIN-STORAGE-GUIDANCE.md`**

Short doc: M1 plugins persist with **an append-only JSONL log + an in-memory projection rebuilt on start** (see `event-journal`, `work-registry`); one `fsync` per append; a partial trailing line on replay is ignored; the reducer that applies a log record is the same code path for replay and live mutation. Content-addressed bytes go to `org.vibe.blob`, never a shared directory. SQLite/WAL is the intended M2 upgrade once the build environment has module-proxy access; the log format is designed to be replayable into a database later.

- [ ] **Step 6: commit**

```
feat(m1.1): M1 policy/bindings for blob+work, extended smoke with restart survival
```

---

## Task 10: acceptance gate + PR

**Files:** modify `docs/M1-DESIGN.md` (§13 milestone status).

- [ ] **Step 1: full build** — `cd <repo-root> && go build github.com/example/agent-native-microkernel/... github.com/example/agent-native-os/plugins/... github.com/example/agent-native-os/cli/...` → exit 0.

- [ ] **Step 2: all Go tests** — `go test ./plugins/... ./plugins/_template ./cli/... && (cd kernel && go test ./...)` → all `ok`.

- [ ] **Step 3: kernel regression untouched** — `cd kernel && python3 tests/integration/m05_qualification.py 2>&1 | tail -2` → `M0.5 ADVERSARIAL QUALIFICATION: PASSED`.

- [ ] **Step 4: architecture checks** — `cd <repo-root> && bash scripts/check-arch.sh` → `CONTRACT CHECK: PASSED (9 contracts, ...)`, `COMPOSITION FITNESS: PASSED (3 manifests)`, `ARCHITECTURE FITNESS: PASSED`, `ARCH CHECKS OK`.

- [ ] **Step 5: smoke** — `bash scripts/smoke.sh` → `M1 SMOKE: PASSED`.

- [ ] **Step 6: G1 kernel purity**

```bash
BASE=9b7aa32
git diff --stat "$BASE" HEAD -- kernel/
git diff --name-only "$BASE" HEAD -- kernel/internal kernel/cmd kernel/sdk
```

Expected: **both empty**. M1.1 touches no kernel file. If either shows output, stop and report.

- [ ] **Step 7: update the milestone status**

In `docs/M1-DESIGN.md` §13, mark `M1.1` done (append `— done <last commit short sha>`). Commit:

```
docs: M1.1 blob + work-registry + vibe CLI complete
```

- [ ] **Step 8: open the PR**

`chatgpt/m1-1-blob-work-registry` → `main`, title **M1.1 — Blob store + Work Registry + vibe CLI**, body summarising the 10 tasks, the acceptance output (verbatim tails of Steps 3–6), and any deviations.

---

## Self-Review

**Spec coverage (`docs/M1-DESIGN.md` §13 M1.1 = "org.vibe.blob + work-registry (Task/WorkContext/真实状态机/attach_evidence/expected_version 必填) + 产品 CLI vibe task create/show"):**
- `org.vibe.blob` → Tasks 1–2.
- work-registry Task/WorkContext/data model (§6) → Tasks 3–4 (types), 5 (create/get).
- real state machine + required `expected_version` (§4.1) → Task 6.
- `work.attach_evidence` + `EvidenceRef` (§6, D2) → Task 7.
- product CLI `vibe task create` / `show` → Task 8 (also `transition`, needed so the smoke can exercise the state machine before the workflow plugin exists — flagged as dev-only in policy, tightened in M1.6).
- G1 machine + manual → Task 10.
- Deferred correctly: workspace/agent/artifact/tool/review/session plugins (M1.2–M1.5), the fan-out `task show` projection, policy tightening (M1.6).

**Placeholder scan:** the four `work.*` response schemas keep `task`/`work_context` as bare `{"type":"object"}` — deliberate (shape is in the Go types + tests), not a gap. `config/m1-policy.json`'s dev-convenience `work.transition@1` grant is explicitly called out with its removal milestone. No `TBD` / "handle errors" / "similar to Task N".

**Type consistency:** `Store`, `Load`, `CreateInput`, `CreateTask` (returns `task, wc, replay, err`), `Transition(wcID, to Status, expectedVersion int)`, `AttachEvidence(wcID, EvidenceRef, expectedVersion int)`, `GetByTask`/`GetByContext`, sentinel errors `ErrNotFound`/`ErrConflict`/`ErrIllegalTransition` — defined in Task 4, used with those exact signatures in Tasks 5–7. Handlers `createHandler`/`getHandler`/`transitionHandler`/`attachEvidenceHandler` all `func(*Store) pluginhost.Handler`. `invoke(socket, identity, token string, req protocol.Envelope)` — Task 8, used in `wire_test.go` and `main.go`. Status constants `StatusPlanned…StatusFailed` consistent across store + handlers + tests. Blob URI `blob://sha256/<64-hex>` consistent across `main.go` + tests + smoke.
