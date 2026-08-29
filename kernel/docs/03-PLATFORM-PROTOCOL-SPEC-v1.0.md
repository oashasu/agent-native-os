# Platform Protocol Specification v1.0

## 1. Runtime protocol identity

Runtime protocol string: `vibe-plugin/1`.

Wire encoding is an implementation binding. The reference binding is newline-delimited JSON over process stdio. The semantic contract is transport independent.

## 2. Manifest

Required fields:

```json
{
  "manifest_version": 1,
  "plugin": {"id":"org.example.plugin", "version":"1.2.3"},
  "runtime": {"protocol":"vibe-plugin/1", "executable":"bin/plugin"}
}
```

Optional declarative sections:

- `exports`: capability + major.
- `consumes.required` / `consumes.optional`.
- `publishes` / `subscribes`.
- `permissions`.
- `resources`.
- `restart`.

Manifest is static/declarative and validated before execution.

## 3. Capability identity

`<namespace>.<name>@<major>`.

Major is the semantic compatibility boundary. Minor/patch schema evolution belongs to the independently distributed contract package/SDK.

## 4. Envelope

Logical fields:

- protocol
- message_id
- kind
- capability
- major
- caller
- provider_hint
- reply_to
- trace_id
- correlation_id
- causation_id
- deadline
- idempotency_key
- stream_id
- payload
- error

Kernel interprets envelope metadata, not domain payload fields.

## 5. Handshake

Host sends `hello` containing runtime protocol, runtime ID and expected plugin identity. Plugin returns `ready` containing runtime protocol, plugin ID/version and optional manifest hash. Mismatch is an incompatible runtime, not a partially started plugin.

## 6. Command

Command asks a capability to cause an effect. Contract documentation defines idempotency and retry behavior. Commands that may be automatically retried must define an idempotency key strategy.

## 7. Query

Query requests information and is expected not to cause a business effect. Queries are normally retryable subject to deadline/provider semantics.

## 8. Result/Error

Result/Error carries `reply_to` referencing the request. Error has stable code, human-readable message, retryability and optional structured details.

Baseline error classes include:

- INVALID_REQUEST
- PERMISSION_DENIED
- CAPABILITY_UNAVAILABLE
- PROVIDER_UNAVAILABLE
- DEADLINE_EXCEEDED
- UNSUPPORTED
- CONFLICT
- BUSINESS_ERROR
- ROUTE_ERROR
- INTERNAL_PROVIDER_ERROR

Provider contracts may add domain-specific codes without reusing baseline semantics.

## 9. Event

Event means “this happened”, not “please do this”. Publishers/subscribers declare event contract and major version. Kernel delivery may be best-effort. At-least-once durable delivery/replay belongs to a durable event capability and requires consumer idempotency.

## 10. Stream

Semantic frame kinds:

- stream.open
- stream.data
- stream.close
- cancel

A stream has `stream_id`, bounded buffers/backpressure, explicit close/error and cancellation behavior. Production bindings may use a transport-native streaming channel while preserving these semantics.

## 11. Deadline

Deadline is absolute RFC3339Nano in the JSON reference binding. A router must fail requests whose deadline has already expired and must not silently wait forever on a dead provider.

## 12. Cancellation

Cancellation targets an outstanding request/stream identifier. Providers explicitly document whether cancellation is immediate, cooperative, or unsupported for a given capability.

## 13. Version compatibility

Within one major:

- optional fields may be added;
- consumers tolerate unknown optional fields;
- required field removal or semantic reinterpretation is forbidden;
- enum growth requires unknown-value handling or explicit compatibility rules.

Breaking semantics => new major. Multiple majors may coexist.

## 14. Provider selection

Default: deterministic healthy provider selection. Policy may consider scope/user preference/capability metadata. Explicit `provider_hint` is permitted for a user/operation pin. A consumer implementation must not require a provider identity to compile.

## 15. Authorization

A routed call is permitted only if the caller manifest/policy declares consumption of the capability. Host-resource access is a separate policy dimension.

## 16. Trace

A routed subcall propagates the same trace/correlation lineage while setting causation to the request that caused it. This gives causal observability without making an event store part of the kernel.
