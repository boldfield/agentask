---
id: T11
title: Heartbeat endpoint — extend lease
state: review
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

## Result

**PR:** https://github.com/boldfield/agentask/pull/14
**Branch:** agentask/T11-heartbeat
**Head SHA:** 7cdb88e87a3c4262de83d6c66af5c2ac0a28b14d

### Verification

- `make build` ✓ — builds successfully
- `go vet ./...` ✓ — no linting issues
- `go test -count=1 ./...` ✓ — all tests pass (9 new test cases added)

### Implementation

**Store layer** (`HeartbeatTask` in `internal/store/store.go`):
- Atomic transaction using conditional UPDATE: `UPDATE task SET lease_expires_at=?, updated_at=? WHERE id=? AND state='in_progress' AND assignee=?`
- Reuses `leaseExpiryTimestamp(leaseTTL)` helper from T09 to compute new lease expiry
- On success (rowsAffected==1): appends a `heartbeat` event in the same transaction, then SELECTs and returns the updated task
- On failure (rowsAffected==0): disambiguates via SELECT — if task doesn't exist, returns ErrNotFound; otherwise returns ErrConflict (task not in_progress or wrong assignee)
- Mirrors ClaimTask exactly in structure and transaction safety

**API layer** (`handleHeartbeat` in `internal/api/api.go`):
- Validates `agent_id` is non-empty (400 if missing)
- Maps store errors: ErrNotFound → 404, ErrConflict → 409
- Returns the updated task (200 OK) on success
- Registered as `POST /tasks/{id}/heartbeat` behind authMiddleware

**Event choice**: Each heartbeat call appends a `heartbeat` event. This is the simplest and most consistent approach. The PR notes that heartbeat events could be rate-limited later if they bloat the log, but that is deferred to a future task.

### Test coverage

Added 7 new test cases:
1. **TestHeartbeatExtendsLease** — Verifies heartbeat by assignee extends lease (new expiry > old)
2. **TestHeartbeatByDifferentAgentReturns409** — Non-assignee gets 409/CONFLICT
3. **TestHeartbeatOnNotInProgressReturns409** — Heartbeat on backlog task → 409/CONFLICT
4. **TestHeartbeatUnknownTaskReturns404** — Unknown task ID → 404/NOT_FOUND
5. **TestHeartbeatEmptyAgentIDReturns400** — Empty agent_id → 400/EMPTY_AGENT_ID
6. **TestHeartbeatAppendsHeartbeatEvent** — Verifies heartbeat event is appended
7. **TestHeartbeatRequiresAuth** — Verifies auth middleware is enforced

All existing tests continue to pass.
