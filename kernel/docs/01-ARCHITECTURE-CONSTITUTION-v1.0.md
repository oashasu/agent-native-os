# Architecture Constitution v1.0

This document has higher priority than ordinary feature, plugin and implementation designs.

## Constitutional laws

### C01 — Kernel purity
Kernel public models and behavior contain mechanisms only, never domain semantics.

### C02 — Foundation is not kernel
A capability may be universally useful and still remain a replaceable plugin.

### C03 — No plugin-to-plugin implementation dependency
Plugins may not import/link another plugin implementation, read its private files/tables, or rely on its private RPC.

### C04 — No shared business library
Shared technical libraries are permitted; shared domain entities/services/repositories are not.

### C05 — Contract-only communication
Cross-plugin interaction is Command, Query, Event or Stream through a versioned public contract.

### C06 — Capability over provider
Consumers depend on `namespace.capability@major`, not plugin identity.

### C07 — Explicit declarations
Exports, consumes, publishes, subscribes, permissions and runtime requirements are declared before plugin execution.

### C08 — State has one Logical Authority
Each business state has exactly one Logical Authority. Multiple provider runtimes may implement that authority only under an explicit replication/fencing contract. Other components use public contracts and may not directly access the authority's private state.

### C09 — Private schema
A plugin owns its schema/migrations. Physical shared storage does not imply logical shared schema.

### C10 — No cross-plugin transaction
No transaction may mutate multiple plugins' private business stores.

### C11 — Idempotent retryable commands
Retryable side-effecting public commands define idempotency semantics.

### C12 — Versioned contracts
Public semantics carry explicit major compatibility identity. Semantic breaking change requires a new major version.

### C13 — Major versions may coexist
A whole system flag-day upgrade is not required.

### C14 — Failure isolation
A plugin failure must not directly crash the kernel or unrelated plugins.

### C15 — Resource and permission isolation
Plugins are subject to host-enforced resource and permission policy. Self-enforcement is insufficient.

### C16 — Default deny
Undeclared/unauthorized capability or host-resource access is denied.

### C17 — Context isolation
Data does not enter Agent context merely because it exists. Namespace, permission and context policy apply explicitly.

### C18 — Raw source immutability
Important source history is append/version/tombstone/delete-with-policy, never silently rewritten.

### C19 — Derived data is rebuildable
Summary/index/embedding/projection/cache are replaceable derived assets.

### C20 — View owns no canonical business state
CLI/Neovim/Desktop may cache presentation state, but a closed UI cannot destroy work/history.

### C21 — Runtime is disposable
PID, PTY, tab, process and provider session identifiers are runtime/external references, never durable business identity.

### C22 — No hidden background behavior
Scheduled/background work has identity, status, trigger, owner and observable result.

### C23 — Traceable effects
Important effects identify requester, capability, provider, result and causal trace.

### C24 — Local buildability/testability
Ordinary feature work does not require building/testing the entire product.

### C25 — Repository topology is not architecture
Monorepo or multi-repo is an organizational decision; plugin boundaries remain enforceable in either.

### C26 — Public contract ownership is independent
A contract is not owned inside a provider implementation repository/module.

### C27 — Packs have no privilege
A pack is a set of plugins/config/default wiring, never a new bypass channel.

### C28 — Domain independence
Adding Learning must not require Engineering business model changes, and vice versa.

### C29 — Avoid universal god objects
Do not create universal Task/Context/Item objects simply to host arbitrary custom fields from every domain.

### C30 — Extend through capability/relations
Prefer new capabilities/contracts/plugins/relations over central field growth.

### C31 — Claims are not evidence
Agent statements, tool observations and human decisions remain distinguishable.

### C32 — Vendor/UI identity is external metadata
Codex IDs, Claude IDs, window/tab/buffer IDs never become durable canonical IDs.

### C33 — Generalize mechanisms, not speculative business semantics
Transport, routing, permission and identity primitives may be generic. Unknown future “business objects” should not be predicted prematurely.

### C34 — Replaceability over perfect prediction
Architecture is successful when wrong future choices can be replaced locally.

### C35 — AI is not an architecture backdoor
An Agent may not bypass contracts by reading another plugin's private DB/files/source.

### C36 — Local comprehensibility
A plugin boundary is too large if a normal development Agent cannot understand its responsibility/contracts/tests within a bounded context.

### C37 — Coordination belongs to composition
Atomic plugins remain focused. Cross-capability orchestration belongs to composition plugins.

### C38 — Irreversible effects are explicit
Delete/publish/merge/release/send/production mutation and similar effects carry explicit risk semantics and policy gates.

### C39 — Infrastructure is replaceable
SQLite/Postgres/HTTP/gRPC/UDS/NATS/etc. are implementation choices unless a protocol explicitly requires otherwise.

### C40 — Constitution over convenience
A shorter direct call is not justification for breaking plugin boundaries.

## Amendment rule

Changing these laws requires an Architecture Amendment containing problem, why the current law prevents a valid requirement, alternatives, compatibility impact, migration, and updated fitness tests.
