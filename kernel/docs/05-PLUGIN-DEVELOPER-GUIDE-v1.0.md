# Plugin Developer Guide v1.0

## The rule

Your plugin may know contracts. It may not know another plugin implementation.

## Minimal development package

A new Agent/session should receive:

- Architecture Constitution,
- Platform Protocol Specification,
- relevant capability contracts/SDK,
- this plugin's requirements/design/source/tests,
- current task.

Do not preload the whole product repository unless integration work truly requires it.

## Responsibility boundary

A plugin should have one coherent sentence describing its responsibility. If it owns multiple unrelated business vocabularies/lifecycles, split it.

## Manifest workflow

Declare all exports, required/optional consumes, published/subscribed events, permissions and resource expectations before code execution.

## Calling another capability

Use the SDK's `Query`/`Command` API with a capability name/major. Never import the provider package. The included consumer example calls `demo.uppercase@1` without importing either provider implementation.

## Optional capabilities

Handle absence explicitly. Do not crash because an optional semantic/search/notification provider is missing. Surface degraded status/behavior.

## Persistence

Choose one:

- use a public State/Blob capability;
- own private storage.

Never read another plugin's private tables/files.

## Testing

Required plugin test layers:

1. Unit tests for local rules.
2. Contract tests against mocks/fixtures.
3. Provider compliance tests for exported contracts.
4. Local process integration where transport matters.
5. System integration only for workflows that genuinely cross plugin boundaries.

## Handoff

At the end of an Agent development session, produce: current state, decisions, contracts changed, tests, open issues and remaining work. This lets a new session continue without reconstructing the entire system.

## Go reference SDK

Public packages:

- `sdk/go/protocol`
- `sdk/go/pluginhost`

Plugins must not import `internal/*`.

Build/run the included examples with `make demo`.
