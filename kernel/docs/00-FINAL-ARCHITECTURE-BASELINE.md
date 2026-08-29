> **SUPERSEDED STATUS NOTICE (v0.9.4):** The earlier “Architecture Closed” conclusion was withdrawn after adversarial qualification review. This file remains historical design input. Current status is **Microkernel Boundary Candidate / M0.5 Architecture Validation**. See patches `09` through `13` in this `docs/` directory.

# Agent-Native Work System — Final Architecture Baseline v1.0

Historical status at time of writing: **Architecture Closed for v1 implementation — WITHDRAWN by M0.5 adversarial review**  
Scope: Microkernel, plugin runtime protocol, architectural constraints, reference implementation boundary.

## 1. Final decision

The system is not an application core with plugins around it. It is a **domain-neutral microkernel hosting independently evolvable capability plugins**.

The architectural invariant is:

> Plugins depend on versioned contracts, never on other plugin implementations.

The microkernel owns no Task, WorkContext, AgentRun, Session, Workflow, Git, Knowledge, Learning, Weather, Review or other domain object. Those are plugin/domain concerns.

## 2. Why this reset exists

The original requirements correctly demanded replaceable Agent providers, persistent work/history independent from UI/runtime, deterministic evidence, large-scale session retrieval, and future non-engineering extensions. The previous overall design nevertheless concentrated Work Registry, Runtime Supervisor, Session Service, Workflow Engine, Scheduler, Notification, Search/Memory and Connector logic in a single application Core. That is a modular monolith and would eventually force large-context/global-understanding development.

v1.0 therefore optimizes for a stronger requirement:

> System size may grow without making the cognitive scope of a local feature grow proportionally.

## 3. Layer model

```text
Human Interfaces / External Callers
                |
        Versioned Contracts
                |
+---------------------------------------+
|              Microkernel              |
| Manifest / Supervisor / Resolver      |
| Router / Authorization / Health       |
| Runtime protocol / trace propagation  |
+---------------------------------------+
        |          |           |
     Plugin A   Plugin B     Plugin C
        |          |           |
        +---- contract only ----+
```

Above the kernel:

- Foundation capability plugins: state, blob, event journal, scheduler, notification, search, secret provider, artifact, identity, etc.
- Domain capability plugins: engineering, learning, time, running, etc.
- Composition plugins: coordinate capabilities without importing providers.
- Adapter plugins: Codex, Claude, GitHub, Calendar, Weather, CI and other external systems.
- View plugins/clients: CLI, Neovim, TUI, Desktop, Web.
- Packs: installation/configuration sets; packs have no privileged runtime channel.

## 4. Kernel membership test

A mechanism belongs in the microkernel only if all are true:

1. The plugin system cannot bootstrap safely without it.
2. The mechanism must be enforced outside plugins.
3. It is provider/domain neutral.
4. Moving it to an ordinary plugin would break constitutional isolation.
5. It belongs to the trusted computing boundary rather than product semantics.

This test deliberately excludes many “important” services from the kernel.

## 5. Kernel v1 owns

- Manifest loading and validation.
- Plugin process/runtime lifecycle.
- Capability registry and provider resolution.
- Contract-addressed request/query/event routing.
- Capability authorization enforcement.
- Runtime identity and health.
- Failure isolation and provider availability.
- Deadline/trace/correlation propagation primitives.
- Host-side resource/sandbox enforcement hooks.

## 6. Kernel v1 explicitly does not own

- Application current state.
- Shared business database/schema.
- Work/Task/Session/Agent semantics.
- Workflow semantics.
- Durable event history.
- Blob/artifact semantics.
- Search/Memory/Knowledge semantics.
- Scheduler/Notification semantics.
- Git/Build/Test/Review semantics.
- Any specific AI provider.
- Any UI state beyond host/runtime metadata.

## 7. State ownership

Every business state has exactly one Logical Authority. Multiple runtime/provider replicas may implement that authority only under an explicit replication and fencing contract. Other plugins interact only through public contracts; physical storage co-location never grants cross-domain ownership or direct table access.

Cross-plugin ACID transactions are prohibited. Cross-plugin consistency uses idempotency, messages/events, retry, reconciliation and compensation.

## 8. Capability model

A consumer declares:

```text
consumes: artifact.read@1
```

not:

```text
depends_on: artifact-plugin
```

Multiple providers may export the same capability/major version. Provider selection is runtime policy. Consumers may provide an explicit provider hint for user-requested pinning, but implementation identity never becomes a source dependency.

## 9. Independent development

A valid plugin must be independently:

- understandable,
- buildable,
- testable,
- packageable,
- startable/stoppable,
- versionable,
- replaceable.

A new Agent development session should need only the Constitution, relevant contracts/SDK, plugin requirements/design/source/tests, and the current task — not the entire repository.

## 10. Failure model

A provider crash changes capability health; it does not crash the kernel or unrelated plugins. Compatible alternate providers may receive subsequent calls. In-flight retry behavior is contract-defined.

A missing required capability blocks only the dependent plugin. An optional capability produces an explicit degraded state.

## 11. Security model

Capability authorization and host-resource permission policy are separate. A plugin's right to call `git.commit@1` does not grant arbitrary filesystem write access.

The reference implementation enforces declared capability consumption. The final architecture requires OS-backed host-resource sandbox adapters before treating untrusted plugins as safely contained. This distinction is explicit rather than hidden.

## 12. Architecture completeness judgement

The v1 kernel boundary is considered complete because every previously ambiguous “core” responsibility has been classified by a repeatable membership test, the public interaction model is protocol-complete enough for independent plugins, and the design has been exercised by executable cross-process plugins and provider failover.

Future business growth should not require reopening the kernel boundary. A change that does require it is a Constitution Amendment, not an ordinary feature change.

## 13. Reference implementation status

The included Go implementation is an executable architectural reference, not a hardened production distribution. It currently proves:

- process-isolated plugin runtimes,
- manifest validation,
- versioned capability registration,
- cross-plugin request routing,
- declared-consumer authorization,
- plugin-originated calls,
- provider replacement/failover,
- runtime health loss after process failure,
- independent plugin source boundaries,
- architecture fitness checks.

Not yet hardened in the reference code: OS-level filesystem/network sandboxing, durable streams/event journal, production package signing, remote transport, full resource quotas, and persistent host metadata. These are implementation hardening items; they do not change the v1 architecture boundary.
