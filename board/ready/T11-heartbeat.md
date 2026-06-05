---
id: T11
title: Heartbeat endpoint — extend lease
state: ready
document: DESIGN.md
depends_on: [T09]
---

## Spec
- `POST /tasks/{id}/heartbeat` `{agent_id}` → extends `lease_expires_at` by the lease TTL.
- Only the current `assignee` may heartbeat, and only while `state='in_progress'` (else 409).
- Append a `heartbeat` event (or rate-limit event writes — note the choice in the PR).

## Acceptance criteria
- Heartbeat by the assignee extends the lease (assert new expiry > old).
- Heartbeat by a non-assignee → 409/403.
- Heartbeat on a non-`in_progress` task → 409.
