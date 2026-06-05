---
id: T09
title: Atomic claim endpoint + query + concurrency test
state: review
document: DESIGN.md
depends_on: [T08, T04]
---

## Spec
The crown jewel (DESIGN.md §4). `POST /tasks/{id}/claim` `{agent_id}`.

- Single conditional `UPDATE` setting `state='in_progress', assignee, lease_expires_at`
  guarded by: `state='ready'` AND lease free/expired AND no incomplete dependency
  (`NOT EXISTS` over `task_dep`).
- `rowsAffected == 1` → 200 with the claimed task. `0` → 409.
- Lease TTL from config (`AGENTASK_LEASE_TTL`, default e.g. 5m).
- Append a `claim` event in the same transaction (T04).

## Acceptance criteria
- Claiming a `ready` task succeeds; claiming it again → 409.
- Claiming a task with an unfinished dependency → 409.
- **Concurrency test:** N goroutines claim the same task; exactly one wins. This is the test
  that proves the design — it must be present and pass reliably.

## Result
- PR URL: https://github.com/boldfield/agentask/pull/11
- Branch: agentask/T09-atomic-claim
- Head SHA: 7dde39b
