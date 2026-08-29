# Plugin Storage Guidance

M1 plugins that own mutable canonical state use an **append-only JSONL log plus an in-memory projection rebuilt on start**. `event-journal` and `work-registry` are the reference implementations.

Each accepted mutation appends one record and calls `fsync` before acknowledging success. Replay uses the same reducer as live mutation so recovery and steady-state behavior do not diverge. A partial trailing JSONL line is treated as an interrupted final append and ignored; corruption before the trailing record is an error.

Content-addressed bytes belong in `org.vibe.blob`. Other plugins store `blob://sha256/<hex>` references rather than sharing or inventing private cross-plugin byte directories.

M1.1 grants `local-cli` direct `work.transition@1` access for development convenience. M1.6 will split that policy according to `docs/M1-DESIGN.md` §4.2 so the qualification identity loses the direct transition grant and the engineering workflow reaches it only through its delegation scope.

SQLite/WAL is the intended M2 storage upgrade once the build environment can resolve the required module dependency. The M1 JSONL command log is deliberately replayable so its history can be imported into a database-backed projection later.
