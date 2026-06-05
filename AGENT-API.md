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
is in `review`, `blocked`, or `failed`.

### 1. Discover claimable work

```bash
curl -s "${A[@]}" "$AGENTASK_URL/projects/$PROJECT_ID/tasks?claimable=true" | jq -r '.[].id'
```

`claimable=true` returns only tasks that are `ready`, have all dependencies `done`, and
carry no live lease. Pick one id. If the list is empty, there is nothing to do — stop.

### 2. Claim it (atomic — you must win)

```bash
curl -s -o /tmp/claim.json -w '%{http_code}' "${A[@]}" \
  -X POST "$AGENTASK_URL/tasks/$TASK_ID/claim" -d "{\"agent_id\":\"$AGENT_ID\"}"
```

- `200` → you won. The task is now `in_progress`, assigned to you, with a lease. Note
  `lease_expires_at` in the response body.
- `409` → someone else won (or it stopped being claimable). Pick a different task; **never**
  work a task you did not win.
- `404` → the task id is gone; re-list.

### 3. Read the full spec

```bash
curl -s "${A[@]}" "$AGENTASK_URL/tasks/$TASK_ID" | jq '{title, spec, depends_on, links}'
```

The `spec` is your contract. Read it, and read the design/feature document it derives from
if you need context (`GET /projects/$PROJECT_ID/documents`). Build exactly what the spec
says — no more.

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

### 8. Submit (moves the task to `review`)

Attach the PR URL and head commit as typed links; this is how the task is tied to its work:

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

### 9. Stop

Do **not** merge. Do **not** transition the task to `done` — a human (or reviewer) approves
via `POST /tasks/$TASK_ID/review` (`verdict: approve`) and then
`POST /tasks/$TASK_ID/transition` (`to: done`). Report the task id and PR URL, and stop.
Only return to step 1 if explicitly told to continue.

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

## Rules

- **One task at a time.** Finish, block, or fail it before touching another.
- **Only work a task you won** (claim returned `200`). A `409` means it's not yours.
- **Heartbeat** before the lease expires on long work, or lose the task.
- **Respect dependencies** — the `claimable=true` filter already enforces them; don't try to
  claim a `ready` task whose deps aren't `done` (the claim will `409`).
- **Stay in scope.** Build what the spec says. Ambiguous or wrong → Blocked, don't improvise.
- **Never self-merge.** Your terminal state is `review`.

## Status / HTTP code reference

| Code | Meaning in this API |
|------|---------------------|
| 200 / 201 | success |
| 400 | bad input (e.g. invalid link kind, empty agent_id) |
| 401 | missing/invalid bearer token |
| 404 | unknown task/project id |
| 409 | not claimable / not your task / illegal transition (e.g. `done` without an approve) |
