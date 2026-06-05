---
id: T02
title: SQLite schema + migrations
state: done
document: DESIGN.md
depends_on: [T01]
---

## Spec
Define the schema in `migrations/0001_init.sql` per DESIGN.md §2:
`project`, `document`, `task`, `task_dep`, `task_link`, `event`.

- `task.state` constrained to: `backlog, ready, in_progress, review, done, blocked, failed`.
- `document.kind` constrained to: `design, feature_spec`.
- `task_link.kind` constrained to: `pr, branch, commit, ci`.
- Index `task_link(kind, value)` for reverse lookup.
- Index `task(project_id, state)` for board queries.
- Timestamps stored as RFC3339 text (UTC).
- Embed migrations with `embed.FS`; apply on boot inside a transaction, tracking applied
  versions in a `schema_migrations` table.

## Acceptance criteria
- Applying migrations to a fresh DB creates all tables/indexes.
- Re-applying is a no-op (idempotent).
- A unit test opens an in-memory/temp DB, migrates, and asserts the tables exist.

## Result
- **PR URL:** https://github.com/boldfield/agentask/pull/2
- **Branch:** agentask/T02-schema-migrations
- **Head commit SHA:** c1e4042
