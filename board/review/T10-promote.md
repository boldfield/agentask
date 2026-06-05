---
id: T10
title: Promote endpoint — backlog → ready
state: review
document: DESIGN.md
depends_on: [T08]
---

## Spec
- `POST /tasks/{id}/promote` → moves `backlog` → `ready`. 409 if not in `backlog`.
- Append a `transition` event.

## Acceptance criteria
- Promoting a backlog task makes it `ready` (and, if deps are done, claimable per T08).
- Promoting a task not in `backlog` → 409.

## Result
- PR: https://github.com/boldfield/agentask/pull/12
- Branch: agentask/T10-promote
- Head SHA: 49e04a7
