# Execution agent runbook (live API)

You are an execution agent draining a project's backlog **through the live Agentask API**.
This is the production loop — you claim, work, and submit over HTTP, not by moving files.
(For the legacy text-file board used to bootstrap the MVP, see `AGENT.md`; this file
supersedes it for any project tracked in a running Agentask instance.)

## Configuration

Two values, from the environment:

- `AGENTASK_URL` — base URL, e.g. `https://agentask.summercamp.eastharbor.casa`
- `AGENTASK_TOKEN` — bearer token for the service

Every request except `GET /healthz` requires `Authorization: Bearer $AGENTASK_TOKEN`.
You also need:

- `PROJECT_ID` — the project you're draining (a UUID).
- `AGENT_ID` — a stable string identifying you (e.g. `haiku-3`). You report it on claim,
  heartbeat, and submit; the server checks it on every state change.

A convenience for the examples below:

```bash
A=(-H "Authorization: Bearer $AGENTASK_TOKEN" -H "Content-Type: application/json")
```

## The loop

Work **one task at a time**, end to end. Do not start a second task until the current one
is in `review`, `approved`, `blocked`, or `failed`.

### 1. Discover claimable work

Poll for tasks that match your assigned model:

```bash
curl -s "${A[@]}" "$AGENTASK_URL/projects/$PROJECT_ID/tasks?claimable=true&model=haiku" | jq -r '.[].id'
```

`claimable=true` returns only tasks that are `ready`, have all dependencies `done`, and
carry no live lease. The `model` parameter restricts results to your model (e.g., `haiku`,
`sonnet`, `opus`). Pick one id. If the list is empty, there is nothing to do — stop.

### 2. Claim it (atomic — you must win, must match model)

```bash
curl -s -o /tmp/claim.json -w '%{http_code}' "${A[@]}" \
  -X POST "$AGENTASK_URL/tasks/$TASK_ID/claim" \
  -d "{\"agent_id\":\"$AGENT_ID\",\"model\":\"haiku\"}"
```

Declare your assigned model when claiming (e.g., `haiku`, `sonnet`, `opus`). The server
enforces model matching — you can only claim tasks whose model equals your declared model.

- `200` → you won. The task is now `in_progress`, assigned to you, with a lease. Note
  `lease_expires_at` and `kind` in the response body.
- `409` → someone else won, it stopped being claimable, or the model didn't match. If your
  claimed model doesn't match the task's model, the error code is `MODEL_MISMATCH`. Pick a
  different task; **never** work a task you did not win.
- `404` → the task id is gone; re-list.

### 3. Read the full spec

```bash
curl -s "${A[@]}" "$AGENTASK_URL/tasks/$TASK_ID" | jq '{title, spec, kind, depends_on, links}'
```

The `spec` is your contract. Read it, and read the design/feature document it derives from
if you need context (`GET /projects/$PROJECT_ID/documents`). Note the `kind` field — it will
be either `implement` or `review`. Build exactly what the spec says — no more.

### 3.5. Branch on task kind

The `kind` field determines your workflow:

**For `kind: implement` tasks:** Proceed to section 4 (Branch and implement). You will
implement the feature, run tests, open a PR, and submit the task to `review`. The system
will auto-spawn review tasks for assigned reviewers (typically Opus). The task advances
through states: `in_progress` → `review` → `approved` → `done` (once human merges).

**For `kind: review` tasks:** This is review work — you are a reviewer. Skip the branch and
implement sections and jump to section 8.5 (below), where instead of submitting the task to
review, you submit a `verdict` (approve or reject) that completes your review task and
potentially advances the parent implement task.

### 4. Branch and implement

```bash
git checkout main && git pull --ff-only
git checkout -b agentask/<task-slug>
```

Implement on the branch. Write tests where the acceptance criteria call for them. Keep the
change scoped to this task. **Never commit to `main`.**

### 5. Heartbeat on long work

The lease expires (default 5m). If your work takes longer, extend it **before** it lapses,
or another agent may reclaim the task:

```bash
curl -s "${A[@]}" -X POST "$AGENTASK_URL/tasks/$TASK_ID/heartbeat" -d "{\"agent_id\":\"$AGENT_ID\"}" | jq -r .lease_expires_at
```

Only the current assignee may heartbeat, and only while `in_progress`.

### 6. Verify before submitting

Actually run it — build, vet, test — and confirm every acceptance-criteria bullet holds.
Do not submit with failing checks.

### 7. Open a PR

```bash
git push -u origin agentask/<task-slug>
gh pr create --fill --base main   # title: short summary of the task
```

The PR body should list which acceptance criteria are met and how you verified them. A
required CI check (`test`, and `docker` where configured) runs on the PR.

### 8. Submit the implement task (moves to `review`)

For `implement` kind tasks, attach the PR URL and head commit as typed links:

```bash
PR_URL=$(gh pr view --json url -q .url)
SHA=$(git rev-parse HEAD)
curl -s "${A[@]}" -X POST "$AGENTASK_URL/tasks/$TASK_ID/submit" -d "$(jq -n \
  --arg a "$AGENT_ID" --arg pr "$PR_URL" --arg sha "$SHA" \
  '{agent_id:$a, result:"see PR", links:[{kind:"pr",value:$pr},{kind:"commit",value:$sha}]}')" \
  | jq -c '{state, links:[.links[].kind]}'
```

Submit clears your lease and moves the task to `review`. Only the assignee may submit, only
from `in_progress`. Valid link kinds: `pr`, `branch`, `commit`, `ci`.

When you submit an implement task, review tasks are auto-spawned for each required reviewer
(the task's `review_models` list; defaults to one Opus reviewer). Those review workers will
claim the review tasks and submit verdicts, advancing the parent task.

### 8.5. Submit a review verdict (review tasks only)

For `review` kind tasks, you submit a verdict instead of links. A review task completes by
submitting an `approve` or `reject` verdict plus an optional writeup:

```bash
curl -s "${A[@]}" -X POST "$AGENTASK_URL/tasks/$TASK_ID/submit" -d "$(jq -n \
  --arg a "$AGENT_ID" \
  '{agent_id:$a, verdict:"approve", result:"Code review passed. Well-structured and thoroughly tested."}')" \
  | jq -c '{state, target_task_id}'
```

The verdict transitions your review task to `done`. When **all** reviewers of the current
round have submitted verdicts:

- **If all approve** → the parent implement task moves to `approved` (awaiting human merge)
- **If any reject** → the parent implement task moves back to `ready` (rework needed)

The parent remains in `review` until the last reviewer completes. Do **not** submit verdicts
for implement tasks (forbidden); only for review tasks.

### 9. Stop

**Implement tasks:** Do **not** merge. Your task is done when it reaches `review` state (with
auto-spawned review tasks) or `approved` state (all reviewers approved). The human drains the
`approved` lane: once all reviewers approve and the task reaches `approved`, a human merges
the PR and transitions the task to `done` via `POST /tasks/$TASK_ID/transition` (`to: done`).

**Review tasks:** Your task is done once you submit your verdict. If you are the last reviewer,
the system will automatically move the parent task to either `approved` (all approve) or `ready`
(rework needed).

Report the task id and PR URL if it was an implement task, and stop. Only return to step 1 if
explicitly told to continue.

## Blocked or failed

If the spec is ambiguous/wrong, a dependency is broken, or the task can't be done as
specified, surface it instead of guessing:

```bash
# blocked: needs info / a decision / a fixed dependency
curl -s "${A[@]}" -X POST "$AGENTASK_URL/tasks/$TASK_ID/transition" \
  -d '{"to":"blocked","note":"<precisely what you need>"}'

# failed: attempted, cannot be done as specified
curl -s "${A[@]}" -X POST "$AGENTASK_URL/tasks/$TASK_ID/transition" \
  -d '{"to":"failed","note":"<what you tried and why it failed>"}'
```

Do not push partial guesses. A human decides next steps.

## Task states and the human merge gate

The `approved` state is the explicit merge gate: once all required reviewers have approved,
the parent task automatically moves to `approved`. Humans then drain this lane, merging the PR
and transitioning the task to `done`.

**Implement task lifecycle:**
- `in_progress` → (submit) → `review` (reviewers are now working)
- `review` → (all reviewers approve) → `approved` (ready for human merge)
- `approved` → (human merges PR) → `done`

If any reviewer rejects before all approve:
- `review` → (any reject + all verdicted) → `ready` (rework needed)

The human may also reject a task from `approved` if they disagree with the reviewers:
- `approved` → (human rejects) → `ready` (rework needed)

This design preserves the human as the final merge gate while automating the review-to-approved
transition once all reviewers agree.

## Rules

- **One task at a time.** Finish, block, or fail it before touching another.
- **Only work a task you won** (claim returned `200`). A `409` means it's not yours.
- **Model matching.** You can only claim tasks whose `model` matches your declared model.
- **Heartbeat** before the lease expires on long work, or lose the task.
- **Respect dependencies** — the `claimable=true` filter already enforces them; don't try to
  claim a `ready` task whose deps aren't `done` (the claim will `409`).
- **Stay in scope.** Build what the spec says. Ambiguous or wrong → Blocked, don't improvise.
- **Know your kind.** Implement tasks end in `review` (or `approved`); review tasks end with
  a verdict. Branch accordingly in section 3.5.
- **Implement workers never merge.** Your terminal state is `review` (or `approved` if all
  reviewers approve). Humans drain the `approved` lane and merge.

## Reviewer auto-merge contract (`agent_merge` flag)

Each task has an optional `agent_merge` boolean flag (defaults to `false`). This flag is **immutable** — set at task creation and never changed.

**When `agent_merge` is `true`:** After a passing review verdict (`approve`), the reviewer (Opus, running with local `gh`) automatically merges the PR:

```bash
gh pr merge --auto
```

The merge is **CI-gated**: it succeeds only if required CI checks have passed. If the merge fails (red checks, branch protection, conflicts, etc.), the task remains in `approved` state — the human must intervene.

If the merge succeeds, the reviewer then transitions the task to `done` via `POST /tasks/{id}/transition` (`to: done`).

**When `agent_merge` is `false` (default):** The task stays in `approved` after a passing review. The human gates the merge via the standard merge workflow; no automatic merge happens.

### Task creation with `agent_merge`

Agents creating tasks (or humans via the API) include `agent_merge: true` in the task input to opt in:

```json
{
  "title": "Low-risk feature",
  "spec": "...",
  "document_id": "...",
  "agent_merge": true
}
```

Omitting it defaults to `false`.

## Status / HTTP code reference

| Code | Meaning in this API |
|------|---------------------|
| 200 / 201 | success |
| 400 | bad input (e.g. invalid link kind, empty agent_id) |
| 401 | missing/invalid bearer token |
| 404 | unknown task/project id |
| 409 | not claimable / not your task / illegal transition (e.g. `done` without an approve) |
