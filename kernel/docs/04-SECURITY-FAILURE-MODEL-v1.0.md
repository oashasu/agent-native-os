# Security and Failure Model v1.0

## Trust model

The kernel is trusted. Plugin code is not automatically trusted. Capability declarations express intent; host enforcement determines authority.

## Two authorization planes

1. Capability authorization: may caller invoke `x@major`?
2. Host-resource authorization: may plugin access filesystem/network/process/secret scope?

These are intentionally separate.

## Default-deny target

Undeclared capability invocation is denied. Production host resources should also be default deny, mediated by OS-backed sandbox adapters.

## Reference implementation caveat

The included Go reference enforces capability declaration routing but does **not** provide hardened filesystem/network sandboxing. A plugin process can still use privileges inherited from the OS account. Do not treat the reference build as a hostile third-party plugin sandbox.

Before production untrusted plugins, implement per-platform enforcement such as macOS sandbox profiles/Endpoint Security design as appropriate, Linux namespaces/seccomp/cgroups/landlock or a dedicated sandbox runtime, and constrained Windows job/token equivalents if supported.

## Failure taxonomy

- Package/manifest invalid: plugin not eligible to start.
- Required capability missing: plugin BLOCKED; unrelated plugins continue.
- Runtime handshake incompatible: that runtime FAILED.
- Runtime crash: capability provider becomes unhealthy; unrelated runtimes continue.
- Alternate provider healthy: new calls may fail over.
- Deadline exceeded: caller receives explicit error.
- Provider/business error: structured error passes back; kernel does not reinterpret domain meaning.
- Durable business recovery: owned by the relevant plugin/composition, not kernel.

## Data corruption containment

A plugin cannot legitimately write another plugin's private state. Backups/migrations are plugin-owned or provided through explicit state/blob capabilities. Cross-plugin reconciliation is message-based rather than shared-transaction based.

## Irreversible effects

Delete, external send, publish, merge, release, secret rotation and production mutation capabilities must expose explicit effect/risk semantics and should be subject to policy/human gates outside ordinary read/query calls.

## Secret model

Secret storage is replaceable. Kernel authorization gates requests to secret scopes. Raw logs/events should support redaction policies at the plugin/observability layer while maintaining an audit trail of redaction.

## Denial of service

Kernel design requires process/resource isolation hooks and bounded message/stream buffers. The reference code has bounded scanner/message sizes but not complete CPU/memory quota enforcement; production host adapters must implement quotas.
