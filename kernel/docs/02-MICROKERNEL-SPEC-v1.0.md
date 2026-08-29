# Microkernel Specification v1.0

## Mission

The microkernel is a domain-neutral controlled plugin runtime host. It provides the rules by which domains coexist, not the domains themselves.

## Trusted computing boundary

The minimal trusted boundary consists of:

- manifest validator,
- runtime protocol parser,
- process supervisor,
- capability registry/resolver,
- message router,
- authorization enforcement,
- runtime health/failure bookkeeping,
- host resource/sandbox enforcement interface.

## Host metadata

Only host-level metadata belongs to the kernel: installed/enabled plugin metadata, runtime instance identifiers, provider bindings, grants/policy, health and host configuration. No Task/Session/Agent/Workflow/Knowledge fields belong here.

## Bootstrap

1. Load host configuration.
2. Discover installed plugin manifests.
3. Validate manifest format and identities.
4. Build capability/provider index.
5. Detect missing required capabilities; mark dependents blocked.
6. Establish permission policy.
7. Start eligible plugin runtimes.
8. Execute runtime handshake.
9. Register healthy runtime providers.
10. Accept routed traffic.

Provider process start failure is local; the host continues with other plugins.

## Runtime model

Default runtime isolation is one OS process per plugin runtime. A package may eventually declare multiple runtime instances. In-process extensions are optimization-only and must not obtain privileged business channels.

Supervisor responsibilities: spawn, handshake, heartbeat/health, stop/kill, restart policy hooks, runtime identity and resource/sandbox adapter invocation.

It does not perform Session resume, Workflow recovery or Agent retry semantics.

## Capability resolution

Capability identity: `namespace.name@major`.

The resolver maps compatible healthy providers at runtime. Default selection must be deterministic. Provider hints are explicit runtime choices and do not alter source dependencies.

Required capability absent at package graph time => dependent plugin BLOCKED. Optional capability absent => plugin may start DEGRADED. Provider becoming unhealthy after start => new requests resolve to another compatible provider if policy allows.

## Router

The router is payload-opaque. It validates the protocol envelope and enforces caller declarations/authorization before forwarding.

Supported semantic families:

- Command: requests an effect.
- Query: requests information with no intended business side effect.
- Event: states that something happened.
- Stream: long-running/bulk flow with backpressure/cancel semantics.

Kernel event routing is not a durable event journal. Durable retention/replay is a Foundation capability.

## Deadlines and cancellation

All long-running request paths support deadlines. Cancellation is a protocol primitive; whether a provider can abort a specific underlying operation is capability-defined and observable.

## Trace propagation

Message envelopes propagate trace, correlation and causation identifiers. Long-term trace storage is provided by replaceable observability plugins.

## State and blob

Neither is kernel-owned. Plugins may use a managed Foundation capability or private storage. Private storage remains private regardless of physical engine.

## Secrets

Secret storage is a provider capability (Keychain/Vault/etc.). The kernel/host enforces whether a caller is allowed to request a secret scope.

## Security hardening boundary

Logical capability authorization is mandatory in v1. Host resource isolation needs platform-specific backends. Production untrusted-plugin execution requires an OS-level filesystem/network/process sandbox rather than relying on manifest honesty.

## Language neutrality

The protocol is not a Go/Rust ABI. Go is the executable reference implementation because a compiler is available in the delivery environment. A Rust kernel can implement the same wire/runtime contracts without changing plugins that use language-neutral bindings.
