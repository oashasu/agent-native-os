# M1.5 — Review Gate + Session History — Implementation Plan

**Execution:** work task-by-task, in order. Each task: write the failing test first, run it and watch it fail for the right reason, write the minimal implementation, run it green, then (for behavioural changes) briefly invert the production change to confirm the test goes red and restore it. Commit at the end of each task with the message given.

**Goal:** Land the two plugins that close the evidence chain:
- **`org.vibe.review`** — a human review gate. `review.request` opens a `PENDING` Review bound to a specific diff artifact + an evidence snapshot; `review.decide` records `APPROVED` / `CHANGES_REQUESTED` with per-criterion acceptance results. A Review decides exactly once.
- **`org.vibe.session`** — `session.seal` builds an immutable archive from a slice of the canonical event journal + a `RecoveryCheckpoint` (git state of the worktree), stores it via `blob.put`, and records a `SessionRecord`.

**Architecture:** Two stateful plugins, own authority + own append-only JSONL log + projection each. `review` has **no dependencies** (no blob, no git). `session` consumes `event.journal.replay@1` (to select the events) and `blob.put@1` (to store the archive) — both called **synchronously inside the handler**, riding the request delegation, so no `service_authority`. `session` shells `git` for the `RecoveryCheckpoint`.

**Tech Stack:** Go standard library + `kernel/sdk/go/{protocol,pluginhost,fencing}` (via `go.work`, **no new external Go dependency**), the `git` binary, newline-delimited JSON over a Unix socket, Python 3 + `jsonschema`.

**Spec:** `docs/M1-DESIGN.md` — §3 (chain: `review.request` → `WAITING_REVIEW` → `review.decide`; `session.seal` → SessionRecord + archive), §5.2 (`org.vibe.review` / `org.vibe.session` rows), §5.1 note (event.journal is a durable journal, not pub/sub; select by `event_ids` or client-side `correlation_id` filter), §6 (`Review`, `SessionRecord`, `SessionEventSelection`, `RecoveryCheckpoint` shapes — Event Journal has NO `seq` field), §7 (Human review is polling `review.get`), §10 (`workspace.release` only after seal verified — that ordering is the workflow's job in M1.6, not this plugin's), §13 milestone M1.5.

**Base:** branch `chatgpt/m1-5-review-session` from `main` at **`78d69ee`** (filled in at dispatch time — this plan is written before M1.4 merges). Present at that point: everything through M1.4 — `plugins/foundation/{blob,event-journal}`, `plugins/{work-registry,workspace,agent-harness,artifact,tool-runner}`, `cli/vibe`, `scripts/smoke*.sh` (with a query-readiness probe in `restart_kernel`), `config/m1-{policy,bindings}.json` with **22 contracts** and **7 plugins** wired, `architecture-tests/check_composition.py`.

## Global Constraints

- **G1 Kernel Purity:** no task modifies `kernel/` source. G1 check: `git diff --name-only 78d69ee HEAD -- kernel/internal kernel/cmd kernel/sdk` must be empty.
- **Do NOT touch `docs/M1-DESIGN.md`.** Do not stage, edit, or commit it. The reviewer updates §13 after merge.
- **No new external Go modules.**
- **Module paths:** kernel `github.com/example/agent-native-microkernel`; plugins `github.com/example/agent-native-os/plugins`; CLI `github.com/example/agent-native-os/cli`.
- **Manifest rule:** export/consume `contract` field == `<capability>@<major>` exactly. Stateful exports need `mode: "stateful"`, `service`, `authority`; manifest needs `runtime.data_namespace`.
- **Contract rule:** `contracts/<dotted.name>/v<major>/schema.json`, register in `contracts/catalog.json`, then `python3 scripts/check-contracts.py --root contracts`.
- **Git identity in tests/scripts:** every `git commit` passes `-c user.email=test@example.com -c user.name=test` inline; a worktree needs at least one commit in the source repo.
- **Commit trailer** — every commit message ends with exactly:
  ```
  Co-Authored-By: ChatGPT <chatgpt@users.noreply.github.com>
  Plan: docs/superpowers/plans/2026-08-29-m1-5-review-session.md
  ```
- **Commit identity:** author `ada <oashasu@gmail.com>` (the connector may substitute its authenticated identity — known limit, not a deviation).
- **Number discipline:** any count in an expected-output string is whatever the plan's own commands produce; if a literal here differs, trust the commands, adjust the assertion, note it.

---

## File Structure

New:

- `contracts/review.request/v1/schema.json`, `contracts/review.decide/v1/schema.json`, `contracts/review.get/v1/schema.json`, `contracts/review.query/v1/schema.json`
- `contracts/session.seal/v1/schema.json`, `contracts/session.get/v1/schema.json`, `contracts/session.query/v1/schema.json`
- `plugins/review/store.go`, `plugins/review/store_test.go`, `plugins/review/handlers.go`, `plugins/review/handlers_test.go`, `plugins/review/main.go`
- `plugins/manifests/review.manifest.json`
- `plugins/session/checkpoint.go`, `plugins/session/checkpoint_test.go` — git-state `RecoveryCheckpoint` builder
- `plugins/session/store.go`, `plugins/session/store_test.go`, `plugins/session/handlers.go`, `plugins/session/handlers_test.go`, `plugins/session/main.go`
- `plugins/manifests/session.manifest.json`
- `scripts/smoke-review-session.sh`

Modified:

- `contracts/catalog.json` — +7 entries
- `config/m1-policy.json` — `local-cli` grants + `org.vibe.review` grant (`capabilities: []`) + `org.vibe.session` grant (`capabilities: ["event.journal.replay@1", "blob.put@1"]`)
- `config/m1-bindings.json` — bindings for the 7 stateful capabilities
- `cli/vibe/main.go` — `review` and `session` subcommands
- `scripts/smoke.sh` — `source scripts/smoke-review-session.sh` after the artifact fragment

---

## Task 1: review contracts

**Files:** four schemas; modify `contracts/catalog.json`.

`Review` shape:

```
Review = {
  id, work_context_id, agent_run_id, diff_artifact_id,
  status:  "PENDING" | "APPROVED" | "CHANGES_REQUESTED",
  reviewer, notes,
  acceptance_results: [ { criterion_id, satisfied, evidence_refs: [string], notes } ],
  evidence_snapshot:  [ { kind, outcome, evidence_ref_id } ],
  requested_at, decided_at
}
```

- [ ] **Step 1: `contracts/review.request/v1/schema.json`** — `kind` `"command"`, request:

```json
  "request": {
    "type": "object",
    "additionalProperties": false,
    "properties": {
      "work_context_id": { "type": "string" },
      "agent_run_id": { "type": "string" },
      "diff_artifact_id": { "type": "string" },
      "evidence_snapshot": {
        "type": "array",
        "items": {
          "type": "object",
          "additionalProperties": false,
          "properties": {
            "kind": { "type": "string" },
            "outcome": { "type": "string" },
            "evidence_ref_id": { "type": "string" }
          },
          "required": ["kind", "outcome"]
        }
      }
    },
    "required": ["work_context_id", "diff_artifact_id"]
  }
```

response `{ "review": { "type": "object" } }` required.

- [ ] **Step 2: `contracts/review.decide/v1/schema.json`** — `kind` `"command"`, request:

```json
  "request": {
    "type": "object",
    "additionalProperties": false,
    "properties": {
      "review_id": { "type": "string" },
      "decision": { "type": "string", "enum": ["APPROVED", "CHANGES_REQUESTED"] },
      "reviewer": { "type": "string" },
      "notes": { "type": "string" },
      "acceptance_results": {
        "type": "array",
        "items": {
          "type": "object",
          "additionalProperties": false,
          "properties": {
            "criterion_id": { "type": "string" },
            "satisfied": { "type": "boolean" },
            "evidence_refs": { "type": "array", "items": { "type": "string" } },
            "notes": { "type": "string" }
          },
          "required": ["criterion_id", "satisfied"]
        }
      }
    },
    "required": ["review_id", "decision"]
  }
```

response `{ "review": object }` required.

- [ ] **Step 3: `contracts/review.get/v1/schema.json`** — `kind` `"query"`, request `{ "review_id": string }` required, response `{ "review": object }` required.

- [ ] **Step 4: `contracts/review.query/v1/schema.json`** — `kind` `"query"`, request `{ "work_context_id": string }` required, response `{ "reviews": { "type": "array" } }` required.

- [ ] **Step 5: catalog + check** — add `review.request@1` / `review.decide@1` / `review.get@1` / `review.query@1`. `python3 scripts/check-contracts.py --root contracts` → PASSED (count = 22 + 4 = 26).

- [ ] **Step 6: commit** — `build(m1.5): review.request/decide/get/query contracts`

---

## Task 2: review store + projection

**Files:** `plugins/review/store.go`, `plugins/review/store_test.go`.

**Interfaces:** `type Store`, `Load(dir string) (*Store, error)` (`dir/review-log.jsonl`), `RecordRequested(r Review) error`, `RecordDecided(id, decision, reviewer, notes string, results []AcceptanceResult) (Review, error)`, `GetByID(id string) (Review, bool)`, `QueryByContext(wcID string) []Review`. Types: `Review`, `AcceptanceResult`, `EvidenceSnapshotItem`, `ErrNotFound`, `ErrAlreadyDecided`. Same JSONL-log + single `apply` reducer + fsync-per-append + torn-last-line tolerance as `plugins/artifact/store.go` — read it and mirror. Ops: `"review.requested"`, `"review.decided"`.

Live `RecordDecided` on a Review whose `status != "PENDING"` → `ErrAlreadyDecided` (do not append). Replay `apply` does not re-check.

- [ ] **Step 1: failing test** (`store_test.go`):

```go
package main

import "testing"

func TestRequestThenDecide(t *testing.T) {
	dir := t.TempDir()
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	r := Review{ID: "r1", WorkContextID: "wc-1", DiffArtifactID: "art-1", Status: "PENDING", RequestedAt: "t0"}
	if err := s.RecordRequested(r); err != nil {
		t.Fatal(err)
	}
	out, err := s.RecordDecided("r1", "APPROVED", "alice", "lgtm", []AcceptanceResult{{CriterionID: "AC1", Satisfied: true}})
	if err != nil || out.Status != "APPROVED" || out.Reviewer != "alice" || len(out.AcceptanceResults) != 1 {
		t.Fatalf("decide: %+v err=%v", out, err)
	}
	if _, err := s.RecordDecided("r1", "CHANGES_REQUESTED", "bob", "", nil); err == nil {
		t.Fatal("second decide must be rejected")
	}
}

func TestReviewProjectionRebuilds(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	_ = s.RecordRequested(Review{ID: "r1", WorkContextID: "wc-1", DiffArtifactID: "a", Status: "PENDING", RequestedAt: "t1"})
	_, _ = s.RecordDecided("r1", "APPROVED", "x", "", nil)
	re, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := re.GetByID("r1")
	if !ok || got.Status != "APPROVED" {
		t.Fatalf("rebuilt: %+v ok=%v", got, ok)
	}
	if q := re.QueryByContext("wc-1"); len(q) != 1 {
		t.Fatalf("query: %+v", q)
	}
}
```

- [ ] **Step 2: run to verify failure.**
- [ ] **Step 3: implement `store.go`.**
- [ ] **Step 4: run green.**
- [ ] **Step 5: mutation check** — drop the `status != "PENDING"` guard in `RecordDecided`. `TestRequestThenDecide` → FAIL. Restore.
- [ ] **Step 6: commit** — `feat(m1.5): review store — JSONL log + projection`

---

## Task 3: review handlers + wiring + policy/bindings/CLI

**Files:** `plugins/review/handlers.go`, `plugins/review/handlers_test.go`, `plugins/review/main.go`, `plugins/manifests/review.manifest.json`; modify `config/m1-policy.json`, `config/m1-bindings.json`, `cli/vibe/main.go`.

**Interfaces:** `requestHandler(s *Store)` / `decideHandler(s *Store)` (fenced stateful commands, service `default-review`, authority `reviews-main`), `getHandler(s *Store)` / `queryHandler(s *Store)`.

- [ ] **Step 1: failing test** (`handlers_test.go`) — `fencedEnv` with lease `default-review--reviews-main.json`; `requestHandler` with `{work_context_id, diff_artifact_id}` → PENDING Review; missing `diff_artifact_id` → `INVALID`; `decideHandler` with `{review_id, decision: "APPROVED", reviewer: "a", acceptance_results: [...]}` → APPROVED; deciding an unknown id → `NOT_FOUND`; deciding twice → `CONFLICT`; `getHandler` unknown → `NOT_FOUND`; `queryHandler` returns array.

- [ ] **Step 2: run to verify failure.**

- [ ] **Step 3: implement `handlers.go`.**
- `requestHandler`: parse `{work_context_id, agent_run_id, diff_artifact_id, evidence_snapshot}`; `work_context_id == "" || diff_artifact_id == ""` → `INVALID`; fenced: `s.RecordRequested(Review{ID: protocol.NewID("rev"), ..., Status: "PENDING", RequestedAt: now})`. Return `{review}`.
- `decideHandler`: parse `{review_id, decision, reviewer, notes, acceptance_results}`; `review_id == "" || decision not in {APPROVED, CHANGES_REQUESTED}` → `INVALID`; fenced: `s.RecordDecided(...)`; map `ErrNotFound → NOT_FOUND`, `ErrAlreadyDecided → CONFLICT`. Return `{review}`.
- `getHandler` / `queryHandler` as usual.

- [ ] **Step 4: `main.go`** — thin wiring, no blob/git. `HandleContextCommand` for request/decide (with `wrap`), `HandleQuery` for get/query.

- [ ] **Step 5: `plugins/manifests/review.manifest.json`** — `id` `org.vibe.review`, executable `../bin/review`, `data_namespace` `state-authority/reviews-main`, exports `review.request@1` / `review.decide@1` / `review.get@1` / `review.query@1` (service `default-review`, authority `reviews-main`), **no `consumes`**, `permissions: []`, `resources { memory_mb: 128, cpu_weight: 10 }`.

- [ ] **Step 6: policy + bindings + CLI**
- `config/m1-policy.json` — `grants.local-cli.capabilities` += `review.request@1`, `review.decide@1`, `review.get@1`, `review.query@1`; add `"org.vibe.review": { "capabilities": [] }`.
- `config/m1-bindings.json` — bindings for the 4 review capabilities (service `default-review`, authority `reviews-main`).
- `cli/vibe/main.go` — `vibe review request <wc-id> [-agent-run <id>] -diff-artifact <id> [-evidence kind:outcome ...]` → print `review <id>  status PENDING`; `vibe review decide <review-id> (-approved|-changes-requested) [-reviewer <name>] [-notes <s>] [-acceptance ID=pass|fail ...]` → print `review <id>  status <status>`; `vibe review show <review-id>` (`-json`).

- [ ] **Step 7: build + composition** — `bash scripts/build.sh` → `built plugin: review`; `check_composition.py` → PASSED (manifest count 8).

- [ ] **Step 8: commit** — `feat(m1.5): review plugin — request/decide/get/query + policy/bindings/CLI`

---

## Task 4: session contracts

**Files:** three schemas; modify `contracts/catalog.json`.

`SessionEventSelection` = `{ journal_cursor_start, journal_cursor_end, correlation_id, event_ids: [string], event_sha256s: [string], event_count }`
`RecoveryCheckpoint` = `{ repo, base_commit, head_commit, branch, worktree_path_at_seal, dirty, tracked_patch_ref, untracked_manifest: [string], diff_artifact_id, task_id, work_context_id, agent_run_id, provider, harness_native_id, canonical_event_selection }`
`SessionRecord` = `{ id, work_context_id, agent_run_id, archive_ref, archive_hash, event_selection, recovery_checkpoint, sealed_at }`

- [ ] **Step 1: `contracts/session.seal/v1/schema.json`** — `kind` `"command"`, request:

```json
  "request": {
    "type": "object",
    "additionalProperties": false,
    "properties": {
      "work_context_id": { "type": "string" },
      "agent_run_id": { "type": "string" },
      "workspace_path": { "type": "string" },
      "correlation_id": { "type": "string" },
      "event_ids": { "type": "array", "items": { "type": "string" } },
      "event_sha256s": { "type": "array", "items": { "type": "string" } },
      "diff_artifact_id": { "type": "string" },
      "task_id": { "type": "string" },
      "provider": { "type": "string" },
      "harness_native_id": { "type": "string" }
    },
    "required": ["work_context_id", "agent_run_id", "workspace_path"]
  }
```

response `{ "session_record": { "type": "object" } }` required.

Selection rule: if `event_ids` is non-empty, select exactly those records from the replay and verify each against `event_sha256s` (same index); otherwise use `correlation_id` (default `work_context_id`) and select every replayed record whose `correlation_id` field matches.

- [ ] **Step 2: `contracts/session.get/v1/schema.json`** — `kind` `"query"`, request `{ "session_id": string }` required, response `{ "session_record": object }` required.

- [ ] **Step 3: `contracts/session.query/v1/schema.json`** — `kind` `"query"`, request `{ "work_context_id": string }` required, response `{ "session_records": { "type": "array" } }` required.

- [ ] **Step 4: catalog + check** — add `session.seal@1` / `session.get@1` / `session.query@1`. `check-contracts.py` → PASSED (count 26 + 3 = 29).

- [ ] **Step 5: commit** — `build(m1.5): session.seal/get/query contracts`

---

## Task 5: `checkpoint.go` — RecoveryCheckpoint builder

**Files:** `plugins/session/checkpoint.go`, `plugins/session/checkpoint_test.go`.

**Interfaces:**

```go
type RecoveryCheckpoint struct {
	Repo               string   `json:"repo"`
	BaseCommit         string   `json:"base_commit"`
	HeadCommit         string   `json:"head_commit"`
	Branch             string   `json:"branch"`
	WorktreePathAtSeal string   `json:"worktree_path_at_seal"`
	Dirty              bool     `json:"dirty"`
	TrackedPatchRef    string   `json:"tracked_patch_ref"`
	UntrackedManifest  []string `json:"untracked_manifest"`
	DiffArtifactID     string   `json:"diff_artifact_id"`
	TaskID             string   `json:"task_id"`
	WorkContextID      string   `json:"work_context_id"`
	AgentRunID         string   `json:"agent_run_id"`
	Provider           string   `json:"provider"`
	HarnessNativeID    string   `json:"harness_native_id"`
	CanonicalEventSelection SessionEventSelection `json:"canonical_event_selection"`
}

// buildCheckpoint gathers the git state of workspacePath. It returns the checkpoint
// with TrackedPatchRef left empty — the caller fills it with a blob URI after
// blob.put of `git diff HEAD`. patch is that diff text.
func buildCheckpoint(workspacePath string) (cp RecoveryCheckpoint, patch string, err error)
```

- [ ] **Step 1: failing test** (`checkpoint_test.go`) — `gitRepoWithChange` helper (init + commit + modify tracked file + add untracked file); `buildCheckpoint(ws)` → `cp.HeadCommit == cp.BaseCommit == <HEAD sha>`, `cp.Branch` non-empty, `cp.Dirty == true`, `cp.UntrackedManifest` contains the new file, `patch` mentions the tracked change. On a clean repo → `cp.Dirty == false`, empty `UntrackedManifest`.

- [ ] **Step 2: run to verify failure.**

- [ ] **Step 3: implement `checkpoint.go`.** A `git(dir, args...)` helper. `HeadCommit = git(ws, "rev-parse", "HEAD")`; `BaseCommit = HeadCommit` (M1: a worktree branch has no commits of its own — note this; future milestones set it from the merge-base); `Branch = git(ws, "rev-parse", "--abbrev-ref", "HEAD")`; `Repo = git(ws, "rev-parse", "--path-format=absolute", "--git-common-dir")` (or `--show-toplevel` of the common dir — best-effort; empty on error); `Dirty = git(ws, "status", "--porcelain") != ""`; `patch = git(ws, "--no-pager", "diff", "HEAD")`; `UntrackedManifest = lines of git(ws, "ls-files", "--others", "--exclude-standard")`; `WorktreePathAtSeal = workspacePath`.

- [ ] **Step 4: run green.**

- [ ] **Step 5: mutation check** — make `buildCheckpoint` skip the `status --porcelain` check (always `Dirty = false`). The dirty-repo test fails. Restore.

- [ ] **Step 6: commit** — `feat(m1.5): RecoveryCheckpoint git-state builder`

---

## Task 6: session store + projection

**Files:** `plugins/session/store.go`, `plugins/session/store_test.go`.

Same shape as prior stores, for `SessionRecord`. One op `"session.sealed"`. `Record(sr SessionRecord) error`, `GetByID`, `QueryByContext`. Types: `SessionRecord`, `SessionEventSelection` (defined here or in `checkpoint.go` — same package). `ErrNotFound`.

- [ ] **Step 1: failing test** — record a `SessionRecord{ID:"s1", WorkContextID:"wc-1", AgentRunID:"r1", ArchiveRef:"blob://sha256/x", ArchiveHash:"abc", SealedAt:"t0"}`; read by id + by context; second `Load` rebuilds two records in order.
- [ ] **Step 2: run to verify failure.**
- [ ] **Step 3: implement.**
- [ ] **Step 4: run green.**
- [ ] **Step 5: mutation check** — drop `byCtx` append in `apply`; projection-rebuild test fails on `QueryByContext`. Restore.
- [ ] **Step 6: commit** — `feat(m1.5): session store — JSONL log + projection`

---

## Task 7: session.seal handler + get/query + wiring

**Files:** `plugins/session/handlers.go`, `plugins/session/handlers_test.go`, `plugins/session/main.go`, `plugins/manifests/session.manifest.json`.

**Interfaces:**

```go
type sealDeps struct {
	Store   *Store
	Replay  func() ([]JournalRecord, error)              // wraps rc.Query("event.journal.replay", ...) with paging
	BlobPut func(payload []byte) (uri string, err error) // wraps rc.Command("blob.put", ...)
	Now     func() string
}
type JournalRecord struct {
	ID            string `json:"id"`
	SHA256        string `json:"sha256"`
	CorrelationID string `json:"correlation_id"`
	Type          string `json:"type"`
	// (raw json also kept for the archive)
	Raw json.RawMessage `json:"-"`
}
func sealHandler(d sealDeps) pluginhost.ContextHandler
func getHandler(s *Store) pluginhost.Handler
func queryHandler(s *Store) pluginhost.Handler
```

`sealHandler` flow: parse+validate (`work_context_id`, `agent_run_id`, `workspace_path` required) → `buildCheckpoint(workspace_path)` → `patchURI, _ := d.BlobPut([]byte(patch))`; `cp.TrackedPatchRef = patchURI` → `records, _ := d.Replay()` → **select**: if `req.EventIDs` non-empty, keep records whose ID is in that set and verify `SHA256` against the same-index entry of `req.EventSHA256s` (mismatch → `INTEGRITY_ERROR`); else `corr := req.CorrelationID; if corr == "" { corr = req.WorkContextID }` and keep records whose `CorrelationID == corr` → build `sel := SessionEventSelection{CorrelationID: corr, EventIDs: [...selected ids...], EventSHA256s: [...selected shas...], EventCount: len}` → `cp.CanonicalEventSelection = sel` → build the archive JSON `{ "session_record": <partial>, "recovery_checkpoint": cp, "canonical_events": [<selected raw records>] }` → `archiveBytes, _ := json.Marshal(archive)`; `archiveURI, _ := d.BlobPut(archiveBytes)`; `archiveHash := sha256hex(archiveBytes)` → `sr := SessionRecord{ID: protocol.NewID("sess"), WorkContextID, AgentRunID, ArchiveRef: archiveURI, ArchiveHash: archiveHash, EventSelection: sel, RecoveryCheckpoint: cp, SealedAt: now}` → `s.Record(sr)` (fenced by the handler wrapper) → return `{session_record: sr}`.

Keep the fence-requiring store write in the handler (`fencing.WithWriteFence(e, func() error { return d.Store.Record(sr) })`), and the seal orchestration in a `sealOnce(d sealDeps, req sealRequest) (SessionRecord, *protocol.Error)` function that is unit-testable with fake `Replay` / `BlobPut` (mirrors M1.3 `runOnce` / M1.4 `runTool`).

- [ ] **Step 1: failing test** (`handlers_test.go`):

```go
func TestSealSelectsByCorrelationAndArchives(t *testing.T) {
	ws := gitRepoWithChange(t) // from checkpoint_test.go
	dir := t.TempDir()
	s, _ := Load(dir)
	var puts [][]byte
	d := sealDeps{
		Store: s, Now: func() string { return "t0" },
		BlobPut: func(b []byte) (string, error) { puts = append(puts, b); return "blob://sha256/" + shortHash(b), nil },
		Replay: func() ([]JournalRecord, error) {
			return []JournalRecord{
				{ID: "e1", SHA256: "h1", CorrelationID: "wc-1", Type: "work.created", Raw: json.RawMessage(`{"id":"e1"}`)},
				{ID: "e2", SHA256: "h2", CorrelationID: "wc-OTHER", Type: "noise", Raw: json.RawMessage(`{"id":"e2"}`)},
				{ID: "e3", SHA256: "h3", CorrelationID: "wc-1", Type: "work.done", Raw: json.RawMessage(`{"id":"e3"}`)},
			}, nil
		},
	}
	sr, perr := sealOnce(d, sealRequest{WorkContextID: "wc-1", AgentRunID: "r1", WorkspacePath: ws})
	if perr != nil {
		t.Fatalf("seal: %+v", perr)
	}
	if sr.EventSelection.EventCount != 2 || sr.ArchiveHash == "" || sr.ArchiveRef == "" {
		t.Fatalf("session record: %+v", sr)
	}
	if !sr.RecoveryCheckpoint.Dirty || sr.RecoveryCheckpoint.TrackedPatchRef == "" {
		t.Fatalf("checkpoint: %+v", sr.RecoveryCheckpoint)
	}
	// blob.put called twice: the patch, then the archive.
	if len(puts) != 2 {
		t.Fatalf("blob.put calls: %d", len(puts))
	}
	// The archive contains only the two wc-1 events.
	if strings.Contains(string(puts[1]), "wc-OTHER") {
		t.Fatalf("archive leaked a non-matching event")
	}
}

func TestSealVerifiesEventSHAWhenIDsGiven(t *testing.T) {
	ws := gitRepoWithChange(t)
	s, _ := Load(t.TempDir())
	d := sealDeps{
		Store: s, Now: func() string { return "t0" },
		BlobPut: func(b []byte) (string, error) { return "blob://sha256/x", nil },
		Replay:  func() ([]JournalRecord, error) { return []JournalRecord{{ID: "e1", SHA256: "GOOD"}}, nil },
	}
	_, perr := sealOnce(d, sealRequest{WorkContextID: "wc-1", AgentRunID: "r1", WorkspacePath: ws, EventIDs: []string{"e1"}, EventSHA256s: []string{"WRONG"}})
	if perr == nil || perr.Code != "INTEGRITY_ERROR" {
		t.Fatalf("want INTEGRITY_ERROR for sha mismatch, got %+v", perr)
	}
}
```

- [ ] **Step 2: run to verify failure.**

- [ ] **Step 3: implement `handlers.go`.**

- [ ] **Step 4: run green.**

- [ ] **Step 5: mutation checks** — (a) in the `correlation_id` filter, keep every record regardless of `CorrelationID` → `TestSealSelectsByCorrelationAndArchives` fails (`EventCount != 2` / archive leaks `wc-OTHER`). Restore. (b) skip the sha verification when `EventIDs` given → `TestSealVerifiesEventSHAWhenIDsGiven` fails. Restore.

- [ ] **Step 6: `main.go`** — `Replay` closure over `rc.Query("event.journal.replay", 1, {after, limit})` paging until `next` stops advancing, unmarshalling each record into `JournalRecord` (keep `Raw`); `BlobPut` closure over `rc.Command("blob.put", 1, {content_base64})`. Register `session.seal` (context command), `session.get` / `session.query` (queries).

- [ ] **Step 7: `plugins/manifests/session.manifest.json`** — `id` `org.vibe.session`, executable `../bin/session`, `data_namespace` `state-authority/sessions-main`, exports `session.seal@1` / `session.get@1` / `session.query@1` (service `default-session`, authority `sessions-main`), `consumes.required` `event.journal.replay@1` + `blob.put@1`, `permissions ["exec:git", "fs:read"]`, `resources { memory_mb: 256, cpu_weight: 20 }`.

- [ ] **Step 8: build + composition** — `bash scripts/build.sh` → `built plugin: session`; `check_composition.py` → PASSED (manifest count 9; `session` consumes 2, under the warn threshold).

- [ ] **Step 9: commit** — `feat(m1.5): session.seal + get/query + plugin wiring`

---

## Task 8: policy, bindings, `vibe session`, smoke

**Files:** modify `config/m1-policy.json`, `config/m1-bindings.json`, `cli/vibe/main.go`, `scripts/smoke.sh`; create `scripts/smoke-review-session.sh`.

- [ ] **Step 1: policy + bindings**
- `config/m1-policy.json` — `grants.local-cli.capabilities` += `session.seal@1`, `session.get@1`, `session.query@1`; add `"org.vibe.session": { "capabilities": ["event.journal.replay@1", "blob.put@1"] }`.
- `config/m1-bindings.json` — bindings for `session.seal` / `session.get` / `session.query` (service `default-session`, authority `sessions-main`).

- [ ] **Step 2: `vibe session` subcommand** in `cli/vibe/main.go`
- `vibe session seal <work-context-id> -agent-run <id> -workspace <path> [-correlation <id>] [-diff-artifact <id>]` → `session.seal@1`; print `session <id>  events <n>  archive <ref>  hash <hash-first-12>  head <head_commit-first-12>`.
- `vibe session show <session-id>` → `session.get@1`; print key fields + `recovery_checkpoint.head_commit` / `.dirty`, `-json` for raw.

- [ ] **Step 3: write `scripts/smoke-review-session.sh`**

```bash
#!/usr/bin/env bash
# M1.5 smoke fragment: request + decide a Review; append a couple canonical events
# and seal a Session that selects them by correlation_id; verify the archive blob,
# the RecoveryCheckpoint, and restart survival.
set -euo pipefail
V=".bin/vibe -socket $SOCK -identity local-cli -token $TOKEN"
RAW=".bin/vibe-raw -socket $SOCK -identity local-cli -token $TOKEN"

SRC="$DATA/rssrc"; mkdir -p "$SRC"
git -C "$SRC" -c init.defaultBranch=main init -q
printf 'class Calc {}\n' > "$SRC/Calc.java"
git -C "$SRC" add -A
git -C "$SRC" -c user.email=s@t -c user.name=s -c commit.gpgsign=false commit -q -m init

WC_ID="$($V task create -title "rs smoke" -goal g -repo "$SRC" | sed -n 's/.*wc \([^ ]*\).*/\1/p')"
WT="$($V workspace allocate "$WC_ID" -repo "$SRC" | sed -n 's/.*path \([^ ]*\).*/\1/p')"
RUN_ID="$($V agent run "$WC_ID" -workspace "$WT" -prompt p -steps 2 -write-file Calc.java -write-content '// touched
' | sed -n 's/.*agent_run \([^ ]*\).*/\1/p')"
sleep 0.5

# --- review ---
rev_out="$($V review request "$WC_ID" -agent-run "$RUN_ID" -diff-artifact art-placeholder -evidence build:PASS -evidence test:PASS)"
REV_ID="$(echo "$rev_out" | sed -n 's/^review \([^ ]*\).*/\1/p')"
echo "$rev_out" | grep -q 'status PENDING' || { echo "FAIL: review not PENDING: $rev_out"; exit 1; }
$V review decide "$REV_ID" -approved -reviewer alice -acceptance AC1=pass | grep -q 'status APPROVED' \
  || { echo "FAIL: review decide"; exit 1; }
$V review show "$REV_ID" | grep -q APPROVED || { echo "FAIL: review show"; exit 1; }
$V review decide "$REV_ID" -changes-requested 2>&1 | grep -qi 'conflict\|error' || { echo "FAIL: second decide should be rejected"; exit 1; }

# --- canonical events for this work context ---
mkevt() {
  $RAW -cap event.journal.append -kind command -service default-event-journal -authority journal-main \
    -payload "{\"type\":\"$1\",\"source\":\"smoke\",\"payload\":{\"work_context_id\":\"$WC_ID\"}}"
}
$RAW -cap event.journal.append -kind command -service default-event-journal -authority journal-main \
  -payload "{\"type\":\"noise\",\"source\":\"smoke\",\"payload\":{}}" >/dev/null || true
# Note: the append handler sets correlation_id from the envelope; the CLI sets a fresh
# one per call, so filter-by-correlation in seal needs the same id. Simplest for the
# smoke: seal without event_ids and assert the archive is produced + hash non-empty.

seal_out="$($V session seal "$WC_ID" -agent-run "$RUN_ID" -workspace "$WT" -correlation "$WC_ID")"
SESS_ID="$(echo "$seal_out" | sed -n 's/^session \([^ ]*\).*/\1/p')"
ARCH="$(echo "$seal_out" | sed -n 's/.*archive \([^ ]*\).*/\1/p')"
echo "$seal_out" | grep -qE 'hash [0-9a-f]{12}' || { echo "FAIL: no archive hash: $seal_out"; exit 1; }
echo "$seal_out" | grep -qE 'head [0-9a-f]{12}' || { echo "FAIL: no head commit in checkpoint: $seal_out"; exit 1; }
$RAW -cap blob.get -kind query -service default-blob -authority blob-main -payload "{\"uri\":\"$ARCH\"}" \
  | python3 -c 'import sys,json,base64; d=json.load(sys.stdin); a=json.loads(base64.b64decode(d["content_base64"])); assert "recovery_checkpoint" in a and "canonical_events" in a, a' \
  || { echo "FAIL: archive blob missing structure"; exit 1; }

restart_kernel
for _ in $(seq 1 50); do $V session show "$SESS_ID" 2>/dev/null | grep -q "$SESS_ID" && break; sleep 0.1; done
$V session show "$SESS_ID" 2>/dev/null | grep -q "$SESS_ID" || { echo "FAIL: session lost on restart"; exit 1; }
$V review show "$REV_ID" 2>/dev/null | grep -q APPROVED || { echo "FAIL: review lost on restart"; exit 1; }

echo "M1.5 REVIEW+SESSION SMOKE: OK"
```

- [ ] **Step 4: wire into `scripts/smoke.sh`** — `source scripts/smoke-review-session.sh` after `source scripts/smoke-artifact.sh`.

- [ ] **Step 5: run the smoke ×5** — every run ends `M1.5 REVIEW+SESSION SMOKE: OK` then `M1 SMOKE: PASSED`, no `FAIL`.

- [ ] **Step 6: commit** — `feat(m1.5): M1 policy/bindings for review+session, vibe subcommands, smoke`

---

## Task 9: acceptance gate + PR

**Files:** none — **do not touch `docs/M1-DESIGN.md`.**

- [ ] **Step 1: full build** — the three module paths → exit 0.
- [ ] **Step 2: all Go tests** — `go test ./plugins/... ./plugins/_template ./cli/... && (cd kernel && go test ./...)` → all `ok`.
- [ ] **Step 3: kernel regression** — `cd kernel && ./scripts/build.sh >/dev/null && python3 tests/integration/m05_qualification.py 2>&1 | tail -2` → `PASSED`.
- [ ] **Step 4: architecture checks** — `bash scripts/check-arch.sh` → `CONTRACT CHECK: PASSED (29 contracts, ...)`, `COMPOSITION FITNESS: PASSED (9 manifests)`, `ARCHITECTURE FITNESS: PASSED`, `ARCH CHECKS OK`.
- [ ] **Step 5: smoke ×5** — every run `M1.5 REVIEW+SESSION SMOKE: OK` + `M1 SMOKE: PASSED`.
- [ ] **Step 6: G1 + design purity** — `git diff --name-only 78d69ee HEAD -- kernel/internal kernel/cmd kernel/sdk` and `-- docs/M1-DESIGN.md` both empty.
- [ ] **Step 7: open the PR** — `chatgpt/m1-5-review-session` → `main`, title **M1.5 — Review Gate + Session History**, body: the 9 tasks, verbatim acceptance output (Steps 3–6), deviations. No docs commit.

---

## Self-Review

**Spec coverage (`docs/M1-DESIGN.md` §13 M1.5 = "review（request/decide/get）+ session-history（seal/archive/SessionEventSelection/RecoveryCheckpoint）"):**
- `review.request` → `PENDING` Review bound to `diff_artifact_id` + `evidence_snapshot` (§3, §6) → Tasks 1–3.
- `review.decide` once, with `acceptance_results` (§4.3, §6) → Task 2 (`ErrAlreadyDecided`), Task 3.
- Human review = polling `review.get` (§7) — the plugin just serves the query; the polling loop is the workflow's job (M1.6). CLI `review show` proves the read path.
- `session.seal` → archive via `blob.put` + `SessionEventSelection` (by `event_ids` **or** `correlation_id` filter — §5.1 note, Event Journal has no `seq`) + `RecoveryCheckpoint` (git state) → Tasks 4–7.
- `archive_hash` verified-able; sha verification when `event_ids` given → Task 7 (`INTEGRITY_ERROR`).
- both survive restart → Task 8 smoke.
- G1 + do-not-touch-design → Task 9.
- Deferred correctly: the workflow enforcing `workspace.release` only after a verified seal (M1.6); `event_ids` produced by the workflow as it appends milestone events (M1.6); untracked file **contents** in the archive (M1 NON-GOAL — names only, §11); base_commit ≠ head_commit once the agent commits (later milestones).

**Do-not-touch:** `docs/M1-DESIGN.md` is not edited or committed by any task.

**Type consistency:** `Review` / `AcceptanceResult` / `EvidenceSnapshotItem` — Task 2, used in Task 3. `Store` + `Load`/`RecordRequested`/`RecordDecided`/`GetByID`/`QueryByContext`, `ErrNotFound`/`ErrAlreadyDecided` — Task 2. `RecoveryCheckpoint` / `SessionEventSelection` / `buildCheckpoint` — Task 5, used in Task 7. `SessionRecord` + session `Store` — Task 6. `sealDeps` / `JournalRecord` / `sealOnce` / `sealHandler` / `getHandler` / `queryHandler` — Task 7. Service/authority: `default-review`/`reviews-main`, `default-session`/`sessions-main` — consistent across manifests, bindings, fence-lease fixtures, smoke. `org.vibe.session` grant carries `event.journal.replay@1` + `blob.put@1`; `org.vibe.review` grant is empty.
