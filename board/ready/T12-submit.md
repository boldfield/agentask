---
id: T12
title: Submit endpoint — in_progress → review + typed links
state: ready
document: DESIGN.md
depends_on: [T09, T04]
---

## Spec
- `POST /tasks/{id}/submit` `{result, links: [{kind, value}]}` → `in_progress` → `review`.
- Only the current `assignee` may submit; only from `in_progress` (else 409).
- Persist `result` on the task and insert `task_link` rows (`kind` ∈ pr|branch|commit|ci).
- Clear the lease (task no longer needs heartbeating). Append a `submit` event. One tx.

## Acceptance criteria
- Submit moves the task to `review`, stores `result`, and persists all links.
- Links are retrievable via `GET /tasks/{id}` and via the `(kind,value)` index.
- Submit by a non-assignee or from a non-`in_progress` state → 409.
