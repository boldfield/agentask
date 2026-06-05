---
id: T03
title: Storage layer — open DB, WAL, Store interface
state: backlog
document: DESIGN.md
depends_on: [T02]
---

## Spec
In `internal/store`:

- Open SQLite via `modernc.org/sqlite` (pure Go, no cgo).
- On open: set `PRAGMA journal_mode=WAL`, `PRAGMA foreign_keys=ON`, `PRAGMA busy_timeout=5000`.
- Run migrations (T02) on open.
- Define a `Store` interface that the API depends on (methods added by later tasks).
  Keep the concrete `sqliteStore` behind the interface so the backend can be swapped later.
- Domain structs: `Project, Document, Task, TaskLink, Event` mirroring the schema.

## Acceptance criteria
- `store.Open(path)` returns a ready `Store` with WAL enabled (assert via `PRAGMA journal_mode`).
- Foreign keys enforced (insert with bad FK fails in a test).
- Opening the same path twice (sequentially) works.
