# Product contracts

Versioned public contracts for the agent-native-os product plugins.
One directory per contract: `<dotted.capability.name>/v<major>/schema.json`.
`catalog.json` maps `<capability>@<major>` to the schema path.

A contract carries: identity, operation kind (command|query|event),
JSON Schema for request/response (or event payload), and a semantic version
whose major equals the capability major.

Validate: `python3 ../scripts/check-contracts.py --root .`

These are NOT the kernel's contracts. `../kernel/contracts/` holds adversarial
fixtures used only by the kernel's own M0.5 self-test.
