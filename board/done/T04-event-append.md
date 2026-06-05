---
id: T04
title: Event append helper
state: done
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

## Result

PR: https://github.com/boldfield/agentask/pull/5
Branch: agentask/T04-event-append
Head SHA: a85d7c4

### Acceptance criteria met:
1. **Transaction atomicity**: Implemented `AppendEvent(ctx, tx, taskID, actor, kind, verdict, note)` that accepts a `*sql.Tx` parameter, allowing callers to use it within an existing transaction. Test `TestAppendEventAtomicity` verifies that rolling back a transaction drops both the task state change and the appended event.
2. **ListEvents ordering**: Implemented `ListEvents(ctx, taskID)` that queries events with `ORDER BY created_at, id`, ensuring chronological order. Test `TestListEvents` verifies correct ordering.

### Implementation details:
- Added `AppendEvent` method to `sqliteStore` with signature `AppendEvent(ctx context.Context, tx *sql.Tx, taskID, actor, kind string, verdict, note *string) (Event, error)`. It generates a UUID-based ID, inserts one event row, and returns the created Event.
- Added `ListEvents` method to `sqliteStore` that opens its own query context (no transaction parameter needed).
- Added both methods to the `Store` interface.
- Created `GenerateID()` helper function using `github.com/google/uuid.NewString()` as the reusable ID pattern for future tasks.
- Timestamps use RFC3339 format in UTC, matching existing schema.
- UUID dependency already present in go.mod, ran `go mod tidy` to update.

### Verification:
- `make build` ✓
- `go vet ./...` ✓
- `go test -count=1 ./...` ✓ (all tests pass including new tests)
