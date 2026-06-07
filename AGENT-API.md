# Execution agent runbook (live API)

You are an execution agent draining a project's backlog **through the live Agentask API**
(v0.2.0+). You claim, work, and submit over HTTP, not by moving files. (For the legacy text-file
board used to bootstrap the MVP, see `AGENT.md`.)

Two things are central in v0.2.0:

- **Tasks are model-pinned.** Every task has a `model` tier (`haiku` / `sonnet` / `opus` / any value
  in the deployment allowlist). You declare your model on claim and may only claim tasks whose
  `model` matches yours.
- **Review is a kind of task.** When an implement task is submitted to `review`, the server
  **auto-spawns a `review`-kind task per reviewer**. Reviewers are model-pinned workers (typically
  `opus`) that claim those review tasks and return a verdict. Most of this runbook is the implement
  loop; the **Review tasks** section covers the reviewer loop.

## Configuration

From the environment:

- `AGENTASK_URL` — base URL, e.g. `https://agentask.summercamp.eastharbor.casa`
- `AGENTASK_TOKEN` — bearer token (every request except `GET /healthz` needs
  `Authorization: Bearer $AGENTASK_TOKEN`)
- `PROJECT_ID` — the project you're draining (a UUID)
- `AGENT_ID` — a stable string identifying you (e.g. `haiku-3`); reported on claim/heartbeat/submit
- `AGENT_MODEL` — your model tier; you may only claim tasks whose `model` equals it

```bash
A=(-H "Authorization: Bearer $AGENTASK_TOKEN" -H "Content-Type: application/json")
```

## The implement loop

Work **one task at a time**, end to end. Don't start a second task until the current one is in
`review`, `blocked`, or `failed`.

### 1. Discover claimable work (your model)

```bash
curl -s "${A[@]}" "$AGENTASK_URL/projects/$PROJECT_ID/tasks?model=$AGENT_MODEL&claimable=true" | jq -r '.[].id'
```

`claimable=true` returns only tasks that are `ready`, have all dependencies `done`, and carry no
live lease; `model=$AGENT_MODEL` further restricts to your tier. Implementers can add `&kind=implement`
to filter for implement-only tasks; reviewers can add `&kind=review` for review-only tasks. Pick one id.
Empty list → nothing to do; stop.

### 2. Claim it (atomic, model-matched — you must win)

```bash
curl -s -o /tmp/claim.json -w '%{http_code}' "${A[@]}" -X POST "$AGENTASK_URL/tasks/$TASK_ID/claim" \
  -d "{\"agent_id\":\"$AGENT_ID\",\"model\":\"$AGENT_MODEL\"}"
```

- `model` is **required** (`400 EMPTY_MODEL` if omitted) and must equal the task's `model`
  (`409`/`MODEL_MISMATCH` otherwise).
- `200` → you won; the task is `in_progress`, assigned to you, with a lease (`lease_expires_at`).
- `409` → someone else won, it's not your model, or it stopped being claimable. Pick another.
- `404` → the task id is gone; re-list.

Never work a task you did not win.

### 3. Read the full spec

```bash
curl -s "${A[@]}" "$AGENTASK_URL/tasks/$TASK_ID" | jq '{title, spec, model, review_models, agent_merge, depends_on, links}'
```

The `spec` is your contract — build exactly what it says, no more. Read the design/feature document
it derives from for context (`GET /projects/$PROJECT_ID/documents`).

### 4. Branch and implement

Branch from the remote (`git fetch origin && git checkout -b <slug> origin/main`); never commit to
`main`. Implement on the branch, scoped to this task, with the tests the acceptance criteria call
for.

### 5. Heartbeat on long work

The lease expires (default 5m). Extend it **before** it lapses, or another agent may reclaim the
task:

```bash
curl -s "${A[@]}" -X POST "$AGENTASK_URL/tasks/$TASK_ID/heartbeat" -d "{\"agent_id\":\"$AGENT_ID\"}" | jq -r .lease_expires_at
```

Only the current assignee may heartbeat, only while `in_progress`.

### 6. Sync with main, then verify

Bring your branch up to date so it merges cleanly, then verify the **merged** result:

```bash
git fetch origin && git merge origin/main --no-edit   # resolve any conflicts, keeping both sides' intent
make check && make test                               # on the merged result; don't submit with failures
```

### 7. Open a PR

```bash
git push -u origin <slug>
gh pr create --fill --base main
```

The PR body should list which acceptance criteria are met and how you verified them.

### 8. Submit (moves the task to `review` and spawns review tasks)

For an **implement** task, submit with the PR/commit links and **no verdict**:

```bash
PR_URL=$(gh pr view --json url -q .url); SHA=$(git rev-parse HEAD)
curl -s "${A[@]}" -X POST "$AGENTASK_URL/tasks/$TASK_ID/submit" -d "$(jq -n \
  --arg a "$AGENT_ID" --arg pr "$PR_URL" --arg sha "$SHA" \
  '{agent_id:$a, result:"see PR", links:[{kind:"pr",value:$pr},{kind:"commit",value:$sha}]}')"
```

Submit clears your lease, moves the task to `review`, and **auto-spawns one `review`-kind task per
entry in `review_models`** (default `["opus"]`), each `ready` and pinned to that reviewer's model.
Only the assignee may submit, only from `in_progress`. Valid link kinds: `pr`, `branch`, `commit`,
`ci`. On rework (a rejected task bounced back to `ready`), continue the **existing** PR — don't open
a new one — and omit links you already attached.

### 9. Stop

Do **not** merge and do **not** transition the task yourself. Report the task id and PR URL, stop.

## Review tasks (the reviewer loop)

A reviewer is a model-pinned worker (e.g. `AGENT_MODEL=opus`) that drains `review`-kind tasks.

### Claim a review task

```bash
curl -s "${A[@]}" "$AGENTASK_URL/projects/$PROJECT_ID/tasks?model=$AGENT_MODEL&claimable=true" | jq -r '.[].id'
curl -s "${A[@]}" -X POST "$AGENTASK_URL/tasks/$REVIEW_TASK_ID/claim" -d "{\"agent_id\":\"$AGENT_ID\",\"model\":\"$AGENT_MODEL\"}"
```

The review task's `spec` carries the **Implementation PR** URL and the **Parent task** id (also in
`target_task_id`). GET the parent task too — its `spec` is the acceptance criteria you review
against, and its `agent_merge` flag + `pr` link matter for the merge step.

### Review as merged with main

Check out the PR head **detached**, merge current main into it, and verify the merged result:

```bash
git fetch origin && git fetch origin "pull/<n>/head" && git checkout --detach FETCH_HEAD
git merge origin/main --no-edit    # CONFLICT → automatic reject
make check && make test            # on the merged result; failure → reject
```

A PR that conflicts with main, or whose merged result fails the build/tests, is never approvable.

### Submit a verdict

```bash
curl -s "${A[@]}" -X POST "$AGENTASK_URL/tasks/$REVIEW_TASK_ID/submit" -d "$(jq -n \
  --arg a "$AGENT_ID" '{agent_id:$a, result:"<findings>", verdict:"approve"}')"   # or "reject"
```

The server records the verdict on the parent and drives it: **reject → parent back to `ready`**
(rework); **approve →** once *all* of this round's reviewers approve (wait-for-all), the parent
moves to `approved`. A fresh review round spawns each time the parent re-enters `review`.

## The `approved` state and the merge gate

A task in `approved` passed review and awaits merge:

- **`agent_merge` is `false` (default):** the **human** merges the PR and transitions
  `approved → done`.
- **`agent_merge` is `true`:** the reviewer auto-merges (next section).

`review → approved` and `review → ready` are server-driven by review verdicts — not manual
transitions. `approved → done` / `approved → ready` are the merge gate.

## Reviewer auto-merge (`agent_merge`)

`agent_merge` is an **immutable** per-task boolean (set at creation, default `false`). When it's
`true`, after a passing review the reviewer (running with local `gh`) merges the parent's PR:

```bash
gh pr merge "<parent-pr-url>" --auto    # CI-gated: merges only once required checks pass
```

If the merge succeeds, transition the parent `POST /tasks/<parent-id>/transition {"to":"done"}`. If
it can't merge (red checks, branch protection, conflict), the task stays in `approved` for the
human. Only do this when the parent has actually reached `approved` (all reviewers approved).

## Task creation

```json
{ "title": "...", "spec": "...", "document_id": "...",
  "model": "haiku", "review_models": ["opus"], "agent_merge": false }
```

- `model` (required) and each `review_models` entry must be in the deployment allowlist
  (`AGENTASK_MODELS`) — else `400 UNKNOWN_MODEL`. `review_models` defaults to `["opus"]`.
- `agent_merge` defaults to `false` and is immutable.
- Review tasks are auto-spawned only — never create them directly.

## Blocked or failed

If the spec is ambiguous/wrong, a dependency is broken, or the task can't be done as specified,
surface it instead of guessing:

```bash
curl -s "${A[@]}" -X POST "$AGENTASK_URL/tasks/$TASK_ID/transition" -d '{"to":"blocked","note":"<what you need>"}'
curl -s "${A[@]}" -X POST "$AGENTASK_URL/tasks/$TASK_ID/transition" -d '{"to":"failed","note":"<what you tried>"}'
```

## Unblocking

Once a blocker is cleared, the task can be recovered from `blocked` state (unlike terminal states
`done` and `failed`). A human operator can unblock and retry a blocked task:

```bash
curl -s "${A[@]}" -X POST "$AGENTASK_URL/tasks/$TASK_ID/transition" -d '{"to":"ready","note":"blocker cleared"}'
```

This transitions the task back to `ready`, clears any stale assignee and lease, and allows a worker
to claim it again.

## Rules

- **One task at a time.** Finish, block, or fail it before touching another.
- **Only work a task you won** (claim `200`). A `409` means it's not yours or not your model.
- **Declare your `model`** on every claim; it must match the task.
- **Heartbeat** before the lease expires on long work.
- **Review the merged-with-main result**, never the branch alone.
- **Never self-merge** unless the task is `agent_merge` and you merged it via the contract above.
  Otherwise your terminal state is `review` (implement) or a submitted verdict (review).

## Status / HTTP code reference

| Code | Meaning |
|------|---------|
| 200 / 201 | success |
| 400 | bad input — `EMPTY_AGENT_ID`, `EMPTY_MODEL`, `UNKNOWN_MODEL`, invalid link kind |
| 401 | missing/invalid bearer token |
| 404 | unknown task/project id |
| 409 | not claimable / not your task / model mismatch / illegal transition |

## State machine

```
backlog ─promote→ ready ─claim→ in_progress ─submit→ review ─(all reviewers approve)→ approved ─merge→ done
                  ▲ ▲                              │                                       │
                  │ └────── lease expiry ──────────┘            reject ─→ ready    human/agent_merge gate
                  │
                  └─ blocked →unblock ─ (recoverable off-ramp; clears stale assignee/lease)
                  
blocked / failed are off-ramps from any active state. blocked is recoverable via → ready; done/failed are terminal.
```
