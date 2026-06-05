---
id: T12
title: Submit endpoint — in_progress → review + typed links
state: done
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

## Result
- **PR URL**: https://github.com/boldfield/agentask/pull/15
- **Branch**: agentask/T12-submit
- **Head SHA**: c4ab895c8d1c6b85e4b7acf6f05e9cd1c6a4c8e2
