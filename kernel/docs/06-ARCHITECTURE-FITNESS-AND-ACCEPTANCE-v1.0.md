# Architecture Fitness & Acceptance v1.0

Architecture constraints are executable where possible.

## Automated checks included

`architecture-tests/check_boundaries.py` verifies:

- kernel/SDK do not import business example plugins;
- example plugins do not import kernel internals;
- example plugins do not import one another;
- plugin IDs are unique;
- capability declarations are structurally valid;
- required example capabilities have providers;
- exported kernel/protocol identifiers do not acquire obvious domain vocabulary.

## Build/test acceptance

`make test` performs:

1. `go test ./...`
2. kernel and independent plugin binary builds
3. cross-process contract demo
4. architecture fitness check

`make demo` additionally proves provider failover:

```text
demo result: ... MICROKERNEL via:contract
failover result: ... [ALT]MICROKERNEL via:contract
```

The primary provider is killed. The consumer is unchanged; subsequent calls resolve to the alternate provider.

## Fitness functions required as the project grows

Add CI guards for:

- no cross-plugin build dependencies in Cargo/npm/Maven/Gradle/etc.;
- contract backward compatibility;
- manifest/schema validation;
- independent package build;
- permission declaration/policy tests;
- dependency/capability graph diagnostics;
- kernel public vocabulary purity;
- contract compliance suites;
- sandbox enforcement tests;
- stream backpressure and deadline tests;
- plugin crash/restart/reconciliation tests.

## Architecture acceptance questions

A feature is architecturally healthy when:

- it can be added mostly as a new/changed plugin/contract;
- unrelated plugins do not change;
- kernel does not acquire domain terms;
- private state remains owned;
- the plugin can be understood/tested locally;
- provider replacement does not require consumer source edits.

If adding Learning forces Engineering or kernel model changes, the abstraction failed.
