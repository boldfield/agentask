---
id: T04
title: Event append helper
state: backlog
document: DESIGN.md
depends_on: [T03]
---

## Spec
Append-only event log per DESIGN.md §2 (`Event` is the spine).

- `store.AppendEvent(ctx, tx, taskID, actor, kind, verdict, note)` — writes one row.
- Must be callable **inside an existing transaction** so a state transition and its event
  commit atomically. Provide a tx-aware signature.
- `kind` examples: `claim, heartbeat, submit, review, transition`. `verdict` nullable.
- `store.ListEvents(ctx, taskID)` returns events ordered by `created_at, id`.

## Acceptance criteria
- A transition + its event are written in one tx (test: rollback drops both).
- `ListEvents` returns chronological order.
