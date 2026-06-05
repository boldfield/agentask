---
id: T10
title: Promote endpoint — backlog → ready
state: ready
document: DESIGN.md
depends_on: [T08]
---

## Spec
- `POST /tasks/{id}/promote` → moves `backlog` → `ready`. 409 if not in `backlog`.
- Append a `transition` event.

## Acceptance criteria
- Promoting a backlog task makes it `ready` (and, if deps are done, claimable per T08).
- Promoting a task not in `backlog` → 409.
