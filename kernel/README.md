# Agent-Native Microkernel v0.10.0 — M0.5 Adversarial Qualification

Status: **Microkernel Boundary Candidate / M0.5 Architecture Validation**

This repository contains a runnable Go reference implementation of the domain-neutral plugin host plus adversarial qualification fixtures. It does **not** claim production security certification and it does **not** declare the microkernel boundary permanently closed.

## What changed since v0.9.4

v0.10.0 closes the blocker class identified by the external adversarial review rather than adding new Work OS product features:

- authenticated external client identity and Host grants;
- opaque Host-issued delegation context to prevent confused-deputy privilege escalation;
- explicit delegated authority for high-level composition without handing the principal all low-level permissions;
- Contract `command/query/event` kind enforced at admission/routing/handler boundaries;
- provider protocol errors preserved separately from route/transport errors;
- ordinary request cancellation routed to the actual in-flight provider;
- live external streaming, client cancellation/disconnect propagation, provider-crash close, and separated stream/control ingress;
- Event publish/subscribe Host grants and Host-rewritten Caller/Principal/ActorChain provenance, including malicious raw-protocol tests;
- durable journal provenance plus fsync append, hash chain and replay integrity verification;
- stateful logical authority with active-writer lease and monotonically increasing fencing epoch;
- cross-process storage fencing rejects stale live writers after promotion;
- durable state acknowledgement: failed state persistence cannot return success;
- JSON Schema -> generated Go contract SDK, generated-code freshness check, and conservative recursive compatibility checks;
- runtime fail-closed admission for duplicate identities, contract mismatch, provider-mode conflicts and invalid stateful declarations;
- required dependency health propagation and explicit `INSTALLED/STARTING/READY/DEGRADED/BLOCKED/FAILED/STOPPED` Host states;
- heartbeat, request failure circuit breaking, restart reconciliation and actual memory/CPU resource-policy enforcement;
- abrupt Kernel death test proving managed delayed side effects do not continue after host SIGKILL.

See `docs/13-M0.5-ADVERSARIAL-QUALIFICATION-v0.10.0.md` for the audit-finding-to-test matrix.

## Quick validation

```bash
bash scripts/test.sh
```

This runs:

```text
go test ./...
go test -race ./...
Schema -> generated SDK freshness check
Contract compatibility tests
Contract schema/catalog checks
Architecture fitness checks
M0.5 adversarial integration qualification
```

Expected final line starts with:

```text
M0.5 ADVERSARIAL QUALIFICATION: PASSED
```

## External CLI

The test policy contains hashes for fixture credentials only. Supply the plaintext fixture token at runtime:

```bash
./scripts/build.sh
./bin/vibe-kernel \
  -plugins ./plugins \
  -policy ./policy.json \
  -bindings ./bindings.json \
  -socket /tmp/vibe-kernel.sock

./bin/vibe \
  -socket /tmp/vibe-kernel.sock \
  -identity local-cli \
  -token m05-local-cli-token \
  -cap work.get \
  -service default-work-registry \
  -authority workdb-main \
  -payload '{"id":"T17"}'
```

Do not reuse fixture credentials outside tests.

## Architectural boundary

The Kernel remains domain-neutral. It knows Plugin/Runtime/Capability/Contract/Caller/Principal/Delegation/Authority/Health/Permission, but not Task, Session, Agent, Workflow, Git, Java, Spring, Learning, or other product-domain objects.

Business state belongs to a **Logical Authority**, not a process. Multiple provider runtimes may implement one authority only under explicit storage identity and fencing semantics.

Plugins still depend on versioned Contracts, never provider implementation source.

## Important limitations

Passing M0.5 means the current adversarial reference suite passes. It is **not** a production security certification. Remaining hardening work deliberately outside this qualification includes:

- production-grade OS filesystem/network sandbox backends and secret isolation;
- package signing, publisher trust and supply-chain policy;
- remote/multi-host transport hardening and distributed authority consensus;
- production observability/exporter implementations;
- generated SDKs for languages other than Go;
- broader JSON Schema feature/codegen support (unsupported constructs fail closed today);
- performance/load qualification at hundreds or thousands of plugins/streams.

These limitations do not justify moving Work/Session/Workflow/etc. into the Kernel.

## Repository highlights

```text
cmd/kernel/                  reference microkernel host
cmd/vibe/                    real external Unix-socket client
internal/authz/              grants + delegation authority
internal/clientgateway/      authenticated client boundary
internal/contractmeta/       contract identity/kind metadata
internal/registry/           providers, authority, writer lease, health
internal/router/             routing, delegation, cancellation, streams/events
internal/supervisor/         runtime lifecycle, heartbeat, resources, dependency state
sdk/go/protocol/             public protocol primitives
sdk/go/pluginhost/           public provider SDK
sdk/go/fencing/              storage writer fencing helper
sdk/go/generated/            schema-generated Go contract bindings
contracts/                   versioned JSON Schema contracts/catalog
plugins/                     M0.5 adversarial providers/consumers
architecture-tests/          architecture fitness checks
tests/integration/           process-level adversarial qualification
tools/                       contract generation/compatibility/checking
docs/                        architecture history + current qualification report
```
