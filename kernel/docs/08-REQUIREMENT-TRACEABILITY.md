# Requirement Traceability

This architecture reset preserves the substantive intent of the uploaded baselines while discarding their prematurely centralized implementation choices.

Source documents:

- `Agent-Native软件工程工作系统-需求分析与总体方案基线-v0.4(1).docx`
- `Agent-Native软件工程工作系统-总体设计-v0.1(1).docx`

Key retained requirement families:

1. UI/View independence: closing/replacing the UI must not destroy work/session history or canonical state.
2. Runtime independence: long-term work/history must survive disposable Agent/PTY processes.
3. Provider independence: Codex/Claude/Gemini/etc. must be replaceable and work must be transferable.
4. Evidence over Agent claim: completion must be supported by deterministic tools/tests/gates.
5. Parallel isolated execution: multiple coding agents need isolated workspaces and traceable outputs.
6. Large-scale history retrieval: sessions/work/artifacts must be findable beyond tab/window identity.
7. Raw history as source asset: original history/provenance remains reconstructable; derived indexes/summaries may be rebuilt.
8. Extensibility: new Agent/framework/Skill/domain must not force repeated changes to the platform core.
9. Data/permission isolation: engineering and personal-domain data require namespace/ACL/context injection boundaries.
10. Lightweight interaction: heavy semantic analysis must be separable from the responsive human interface.

Architectural mechanisms deliberately unfrozen from the old design include the previous central Core Daemon business modules, universal Control Plane business schema, central Workflow/Scheduler/Notification/Search/Connector ownership, and mandatory SQLite/HTTP/WebSocket choices.

The new architecture treats these as replaceable implementation/provider decisions unless they satisfy the strict kernel membership test.
