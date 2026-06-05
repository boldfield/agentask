---
id: T13
title: Review verdict + transition (done/blocked/failed), human gate
state: ready
document: DESIGN.md
depends_on: [T12, T04]
---

## Spec
Per DESIGN.md §5 — human gate for the MVP.

- `POST /tasks/{id}/review` `{actor, verdict, note}` — records a `review` event
  (`verdict` ∈ approve|reject). Allowed only while `state='review'`.
  - This records a verdict; it does **not** itself move the task (any actor — human, agent,
    CI later — may post one).
- `POST /tasks/{id}/transition` `{to, note}`:
  - `review → done`: allowed (human decision). Require at least one `approve` review event
    present (reject the transition 409 if none).
  - `→ blocked` / `→ failed`: allowed from any active state.
  - `review → ready`: the reject path (kick back for rework).
  - All other transitions → 409. Append a `transition` event.

## Acceptance criteria
- Posting an `approve` then `transition {to: done}` succeeds.
- `transition {to: done}` with no approve event → 409.
- `transition {to: ready}` from review returns the task to the claimable pool.
- Review on a non-`review` task → 409.
