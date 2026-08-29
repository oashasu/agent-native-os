> **SUPERSEDED STATUS NOTICE (v0.9.4):** The earlier “Architecture Closed” conclusion was withdrawn after adversarial qualification review. This file remains historical design input. Current status is **Microkernel Boundary Candidate / M0.5 Architecture Validation**. See patches `09` through `13` in this `docs/` directory.

# Kernel Completeness Review v1.0

## Question

Is the kernel design sufficiently complete to stop iterating on the boundary and begin building higher-level capabilities?

## Result

**Historical answer: Yes — v1.0 boundary is closed. This answer is WITHDRAWN. See `13-M0.5-ADVERSARIAL-QUALIFICATION-v0.10.0.md`.**

“Complete” here does not mean no future implementation refinement. It means there is no unresolved architectural reason to put additional application semantics into the kernel.

## Decisions that are now closed

- Work/Task/Agent/Session/Workflow/Knowledge are not kernel concepts.
- State/Blob/Durable Event/Scheduler/Notification/Search/Secret storage are replaceable capabilities, not kernel services.
- Cross-plugin implementation imports/private DB access are forbidden.
- Capability contract is the sole business dependency boundary.
- Plugin runtime is process-isolated by default.
- Provider selection is runtime binding, not source dependency.
- Cross-plugin transactions are not part of the model.
- Contract major versions may coexist.
- Kernel carries runtime health, routing, authorization and supervision only.

## What remains implementation work, not design uncertainty

- hardened OS sandbox backends;
- persistent host metadata implementation;
- production stream/backpressure implementation;
- durable event/state/blob foundation plugins;
- package signing/trust store;
- remote transport binding;
- richer provider policy/scopes;
- restart/backoff and rolling upgrade manager;
- multi-language generated SDK tooling.

None of these require moving domain semantics into the kernel.

## Reopen conditions

The kernel boundary should be reopened only if a validated requirement demonstrates that a constitutional invariant cannot be enforced outside the TCB. Convenience, performance before measurement, or “many plugins use it” are insufficient reasons.

## Next architectural work outside the kernel

The next phase should be a Plugin Boundary Map for the Engineering Work OS, followed by Foundation contracts/plugins (state/blob/artifact/event journal) and then small domain capability plugins. Those are deliberately outside this kernel package.
