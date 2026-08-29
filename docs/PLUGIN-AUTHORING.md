# Adding an agent-native-os plugin

## Layout
- Domain-neutral capability  -> `plugins/foundation/<name>/`
- Domain plugin              -> `plugins/<name>/`
- Every plugin is `package main`; tests live beside it.

## Steps
1. `cp -r plugins/_template plugins/<name>`
2. Edit `main.go`: plugin id (`org.vibe.<name>`), version, capability name, handler kind.
3. Write `contracts/<dotted.capability>/v1/schema.json` (identity `contract`,
   `kind`, `version` `1.x.y`, `request`/`response` Draft 2020-12). Add it to
   `contracts/catalog.json`. Run `python3 scripts/check-contracts.py --root contracts`.
4. Write `plugins/manifests/<name>.manifest.json` from `_template/manifest.json.tmpl`.
   The export `contract` field MUST equal `<capability>@<major>` exactly.
5. Build wiring: `scripts/build.sh` compiles every `plugins/**/main.go` whose
   directory name is not `_template` into `plugins/bin/<dir-name>`.

For a state-owning plugin, use `mode: "stateful"` and provide `service`,
`authority`, and `runtime.data_namespace`. Drop the `consumes` block when the
plugin needs no other capability.

## Handler pattern (testable without a kernel)
Keep domain logic in a function or a closure over a store:

    func newThingHandler(s *store) pluginhost.Handler {
        return func(e protocol.Envelope) (any, *protocol.Error) { ... }
    }

Unit-test it by calling the handler with a hand-built `protocol.Envelope`
(see `plugins/_template/main_test.go`). Only the smoke/integration tests need a
running kernel.

## Rules
- Talk to other capabilities only through contracts (Command/Query/Event/Stream).
- Never import another plugin or `kernel/internal/**`.
- Stateful writes go through `fencing.WithWriteFence(e, func() error { ... })`.
- A composition plugin sets `"composition": true` in its manifest and owns no
  stateful export (enforced by the composition fitness check).
