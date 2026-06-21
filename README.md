# Agentask

An API-only coordination substrate for a pool of AI agents draining a backlog of work.

Agentask is the control plane that powers a fleet of AI agents: it manages a task backlog, enforces
a state machine, ensures atomic task claiming with crash recovery, and routes submitted work to
reviewers. Agents claim bite-size tasks from a per-project board via REST API, execute them, and
submit for human review. It is **not a kanban UI** — there is no drag-drop or visualization. The
board is a work queue with a precise state machine and atomic claiming primitives.

## What It Is

Agentask exists to power this workflow:

1. Design a feature and formalize it in a design document.
2. Decompose the design into **bite-size tasks** — each well-scoped enough that a senior engineer would hand it to someone for execution.
3. Register the document and create tasks on a per-project board.
4. A pool of **execution agents** (e.g., Haiku) claim tasks, execute them, and submit for review.
5. **Reviewer agents** (e.g., Opus) review the work; a **human** merges and gates the final ship.

The system is built on Go + SQLite, deployed as a single-replica service. It is the queue and
state-machine primitive underneath an agent-driven development workflow.

## The Core Model

**Projects → Documents → Tasks**

- **Project**: Maps to one code repository (e.g., `https://github.com/myorg/myrepo`). Created and known upfront.
- **Document**: Either a `design` (one per project) or a `feature_spec`. Lives in the project's repo (e.g., `DESIGN.md`, `docs/features/foo.md`). Agentask stores only the ref and optional commit pin; content is not centralized.
- **Task**: A unit of work decomposed from a document. Has a spec, assigned model (e.g., `haiku`, `opus`), and required reviewers.

**State Machine**

```
backlog ──promote──► ready ──claim──► in_progress ──submit──► review ──approve──► approved ──merge──► done
                       ▲                    │                     │                     │
                       └──── lease expiry ──┘             reject──┘ (→ ready)    ──────┘
                                                                            PR-watch:
                                                                      merged → done
                                                                      closed → abandoned

                      blocked / failed / abandoned are off-ramps
```

- **backlog**: Initial state. Task is not yet claimable.
- **ready**: Human has promoted it. Task is claimable (subject to dependencies).
- **in_progress**: Agent has claimed it and is executing. Lease governs crash recovery.
- **review**: Agent submitted work. Reviewers vote. On rejection, task returns to `ready`.
- **approved**: All reviewers voted approve. Awaits human merge (or PR-watch driven transitions).
- **done**: Work is merged.
- **blocked / failed / abandoned**: Off-ramps. Abandoned is terminal (PR closed without merging or task explicitly abandoned).

**Dependencies & Claiming**

A task is **claimable** iff:
- It is in `ready` state (human promoted it).
- All its dependencies are `done`.
- It has no active lease (crashed agent recovery).

Claiming is a single atomic database transaction — no locks, no broker, no race conditions.
Leases are checked lazily: if an agent dies, its lease expires, and the task becomes claimable
again. No background sweeper needed for MVP concurrency (2–5 agents).

**Task Kind & Review**

Each task has a `kind`:
- **implement**: Execution work. Claims a model (e.g., `haiku`), carries a spec, and specifies which models review it (e.g., `["opus", "sonnet"]`). On submit, review tasks are auto-spawned.
- **review**: Review work. Auto-created per reviewer model. Points back to its parent implement task. Reviewers vote; results are aggregated (majority or unanimous, configurable per deployment).

## The API

All endpoints (except `/healthz`) require `Authorization: Bearer <token>` header.

**Server Configuration:**
- `AGENTASK_TOKEN` (required): The bearer token for authentication.
- `AGENTASK_DB` (required): SQLite database path (e.g., `/data/agentask.db`).
- `AGENTASK_ADDR` (optional, default `:8080`): Server address.
- `AGENTASK_MODELS` (optional, default `haiku,sonnet,opus`): Comma-separated list of valid model names. This is the allowlist for all models that can claim tasks or be specified as reviewers.
- `AGENTASK_ESCALATION_LADDER` (optional): Comma-separated list of models in escalation order for reviewer routing. Defaults to `AGENTASK_MODELS` if unset. Every model in this ladder must be in `AGENTASK_MODELS`. Models can be valid review models without being in the escalation ladder — for example, `gpt-5.5` can be specified as a reviewer model via the Codex CLI (see below) without being in the escalation ladder.
- `FORGE_TOKENS` (optional): Path to the forge tokens file for GitHub API authentication (defaults to `~/.agentask/forge-tokens`). Only needed if PR-watch reconciler is enabled.

See [`docs/api.md`](./docs/api.md) for the full API reference with all request/response examples.

**Key Endpoints**

- `GET /healthz` — Health check (no auth).
- `POST /projects`, `GET /projects`, `GET /projects/{id}` — Manage projects.
- `POST /projects/{id}/documents`, `GET /projects/{id}/documents` — Register and list design/spec documents.
- `POST /projects/{id}/tasks`, `GET /projects/{id}/tasks` — Bulk-create and list tasks (with filters: `state`, `model`, `kind`, `claimable`).
- `GET /tasks/{id}` — Get task with dependencies and links.
- `POST /tasks/{id}/claim` — Atomic claim (agent → `in_progress` with lease).
- `POST /tasks/{id}/heartbeat` — Extend lease (agent signals it is alive).
- `POST /tasks/{id}/submit` — Agent submits work (→ `review`, auto-spawns review tasks).
- `POST /tasks/{id}/review` — Reviewer votes (verdict, notes).
- `POST /tasks/{id}/promote`, `POST /tasks/{id}/transition` — Human promotion and state transitions.
- `POST /tasks/{id}/archive`, `POST /tasks/{id}/unarchive` — Soft-archive tasks and projects.

**Links**

Tasks can carry typed links:
- `pr`: Pull request (e.g., GitHub PR URL).
- `branch`: Git branch (e.g., `mr/abc123def456`).
- `commit`: Commit SHA.
- `ci`: CI run (e.g., test result).

Links are indexed and can be queried in reverse (e.g., find the task for a given PR URL).

## Notifications

Agentask includes an in-process notification loop that notifies external systems when a task
requires human attention. The notifier is **level-triggered** and runs at regular intervals,
checking for tasks in terminal states and POSTing to a configurable webhook.

**When notifications are sent:**

The notifier monitors tasks in three states and sends notifications to the `NOTIFY_URL` endpoint
(if configured). The state-to-event mapping and recency rules are:

| Task State | Event              | Priority | Notes                                                     |
|------------|--------------------|----|----------------------------------------------|
| `approved` | `agentask-review`  | P2 | Task has passed review and awaits human merge decision    |
| `blocked`  | `agentask-blocked` | P2 | Task was blocked and requires human intervention          |
| `failed`   | `agentask-failed`  | P3 | Task failed; only notified within `NOTIFY_FAILED_WINDOW` (default 1h, recency-windowed) |

The notifier is a **no-op** when `NOTIFY_URL` is unset — notifications are simply not sent, and
no errors are logged.

**Configuration:**

Set the following environment variables to enable notifications:

- `NOTIFY_URL` (required): The webhook URL to POST notifications to. If unset, notifications are
  disabled entirely. Example: `https://your-notifier.example.com/notify`.
- `NOTIFY_TOKEN` (required if `NOTIFY_URL` is set): Bearer token used to authenticate requests
  to the webhook. Sent as the `Authorization: Bearer <token>` header.
- `NOTIFY_INTERVAL` (optional, default `30s`): How often the notifier checks for tasks in need
  of notification. Duration format: `30s`, `1m`, etc.
- `NOTIFY_FAILED_WINDOW` (optional, default `1h`): Time window within which a failed task triggers
  notifications. Tasks that failed more than this duration ago are not notified. Duration format:
  `1h`, `30m`, etc.

**Example:**

```bash
export NOTIFY_URL="https://notifier.example.com/notify"
export NOTIFY_TOKEN="your-secret-token"
export NOTIFY_INTERVAL="30s"
export NOTIFY_FAILED_WINDOW="1h"
./bin/agentask server
```

## PR-Watch Reconciler

The PR-watch reconciler runs in-server on the reconcile runner and watches GitHub pull requests
linked to approved tasks. It drives state transitions on the board based on PR activity, enabling
human-gated workflows where code review and approval happen on GitHub, and Agentask remains
synchronized with the PR's state.

**How It Works**

The reconciler monitors all `approved` tasks that have `agent_merge=false` (i.e., tasks awaiting
human action). For each task with a linked GitHub PR URL, it fetches the PR's current state and
review decisions from GitHub, then applies one of four actions:

| PR State              | Action                                                      |
|----------------------|-------------------------------------------------------------|
| `merged`             | Task transitions to `done` and fires `agentask-merged` event |
| `closed` (unmerged)  | Task transitions to `abandoned`                             |
| `open` + `changes requested` (newer than approval) | Task bounces back to `ready` for rework |
| All other states     | No action (continues monitoring)                             |

**State Transitions**

- **PR merged → done**: When the PR is merged, the task is marked complete. An `agentask-merged`
  notification is published to alert external systems of the merge.
- **PR closed unmerged → abandoned**: If the PR is closed without merging, the task is marked
  `abandoned` — a terminal state indicating it will not be completed.
- **PR 'changes requested' → ready**: If a reviewer posts 'changes requested' on the PR *after*
  the task was approved, the reconciler bounces it back to `ready` and posts a comment on the
  PR explaining that the task has been returned for rework.
- **Abandoned is terminal**: Once a task is in the `abandoned` state, it cannot transition to
  any other state (it is a permanent off-ramp for work that is no longer needed).

**GitHub Authentication**

The reconciler requires GitHub API tokens to fetch PR state and post comments. Tokens are read
from a file specified by the `FORGE_TOKENS` environment variable (or `~/.agentask/forge-tokens`
if unset). The file format is one owner-token pair per line:

```
# File: ~/.agentask/forge-tokens (or $FORGE_TOKENS)
owner1=token_for_owner1
owner2="token_for_owner2"  # quoted tokens are supported
# Comments are allowed
owner3=token_for_owner3
```

Tokens are **server-side per-owner**: each GitHub organization owner has its own API token,
allowing the server to act on behalf of that owner when accessing private repos. The reconciler
looks up the owner from the PR URL and fetches the corresponding token to authenticate requests.

**Configuration**

Set the `FORGE_TOKENS` environment variable to point to your tokens file:

```bash
export FORGE_TOKENS="/path/to/forge-tokens"
./bin/agentask server
```

If unset, the reconciler defaults to `~/.agentask/forge-tokens`. If no tokens file exists, the
reconciler proceeds with unauthenticated GitHub API calls: for public repos this may succeed
(subject to GitHub's 60 req/hr unauthenticated rate limit), but for private repos the requests
fail with 401/404 errors that are logged each reconcile cycle. If you do not need GitHub
integration (development or local deployments), create an empty tokens file or set
`FORGE_TOKENS` to point to an empty file.

## How to Run It

### Build

```bash
# Server
make build
./bin/agentask server

# TUI (optional)
make tui
./bin/agentask-tui
```

### Server

Set environment variables:

```bash
export AGENTASK_TOKEN="your-secret-token"
export AGENTASK_DB="/path/to/agentask.db"
export AGENTASK_ADDR=":8080"  # optional, default :8080
```

Then run:

```bash
./bin/agentask server
```

The server will listen on the configured address and expose the REST API. The database is created
automatically on first run.

### TUI

The optional terminal UI (`cmd/agentask-tui`) displays projects, documents, and tasks organized by state, with filtering and search. It supports confirm-gated actions to archive and unarchive tasks and projects. Run it against the server:

```bash
./bin/agentask-tui
```

Useful for human oversight and management of the board.

### Testing & Checks

```bash
make test      # Run tests
make check     # Run gofmt, go vet, and go mod tidy checks
```

## The Worker & Reviewer Harness

See [`harness/README.md`](./harness/README.md) for a deep dive.

**High-Level Overview**

The `harness/` directory contains a fleet of headless agents:

- **Workers**: Claim `implement` tasks across all models, execute them via `claude -p`, and submit results.
  - `worker.sh`: Generic implementer for any model tier.
- **Reviewers**: Claim `review` tasks across all models, run `claude -p` to produce verdicts, and submit votes.
  - `reviewer.sh`: Generic reviewer for any model tier.

Each agent:
1. Polls for claimable work of its `kind` across all models.
2. Claims a task atomically (the task specifies the model).
3. Dispatches one `claude -p` task with the appropriate model (with a prompt that includes the spec).
4. Waits for completion.
5. Submits the result and repeats.

Agents stand up their own git worktrees (one per repo), so multiple agents can work in parallel
without stepping on each other.

**Review-Only Models via Codex**

Some models (e.g., `gpt-5.5`) are available as review-only models via the Codex CLI. These models do not need to be in `AGENTASK_ESCALATION_LADDER` and are routed through the Codex sandbox rather than the direct Claude API.

To enable Codex routing:

- `AGENT_CODEX_MODELS` (optional): Comma-separated list of models to route through `codex exec` (e.g., `gpt-5.5`). When a reviewer's model is in this list, the harness invokes `codex exec --sandbox danger-full-access` instead of the standard `claude -p` command.
- `AGENT_CODEX_FLAGS` (optional): Additional flags to pass to `codex exec` (e.g., `-c model_reasoning_effort=high`).

The fleet image bundles the Codex CLI (installed via `npm install -g @openai/codex`); no manual installation is required.

Reviewers using Codex-routed models require the `codex-auth` subscription secret to be configured in their environment so they can authenticate with the Codex CLI.

**Running the Harness**

```bash
cd harness
export AGENTASK_PROJECT="<project-id>"
export AGENTASK_REPO="~/projects/<repo>"

# Start workers in separate terminals:
./worker.sh worker-1
./worker.sh worker-2

# Start reviewers in separate terminals:
./reviewer.sh reviewer-1
./reviewer.sh reviewer-2
```

For multi-project mode and advanced configuration, see [`harness/README.md`](./harness/README.md).

### Running inside an `sbx` sandbox (`sbx.sh`)

To boot the **entire stack — server + fleet — self-contained inside an `sbx` sandbox**, use
[`harness/sbx.sh`](./harness/sbx.sh). One command starts the agentask server (local SQLite DB + a
fixed local token), polls `/healthz` until it's up, and launches N workers + N reviewers — keeping
**all state under `/tmp/agentask`** (nothing touches `~/.agentask`, your repos, or GitHub).

```bash
# Drain a project backed by a LOCAL git repo (local_commit mode — the CLI commits; no PR/forge):
bash harness/sbx.sh --project <uuid> --repo <path-to-local-git-repo>

# …or a fully self-contained throwaway demo (creates its own repo + project + board):
bash harness/sbx.sh --seed-demo

# Manage it without Ctrl-C (handy when launched from inside an agent):
bash harness/sbx.sh status
bash harness/sbx.sh stop
```

Workers and reviewers are **model-dynamic** (each runs `claude` with the task's own model). Inside a
sandbox, a nested `claude -p` needs `--allow-dangerously-skip-permissions` alongside
`--dangerously-skip-permissions`; `sbx.sh` supplies it via the `AGENT_CLAUDE_FLAGS` env var that
`agent.sh` appends (empty and harmless outside the sandbox). See
[`harness/README.md`](./harness/README.md#running-inside-an-sbx-sandbox-sbxsh) for the full flag,
delivery-mode, and shutdown reference.

## Skills

Agentask ships two [Claude Code](https://docs.claude.com/claude-code) **skills** (`SKILL.md` agents)
covering the two human-facing ends of the workflow — turning intent into a board, and draining the
review gate. They live alongside the model-pinned fleet: `agentask-breakdown` fills the board, the
workers and reviewers drain it, and `review` is how the human inspects and gates the `approved` lane.
Each triggers on natural-language requests inside an interactive `claude` session.

### `agentask-breakdown` — intent → board

[`skills/agentask-breakdown/`](./skills/agentask-breakdown/SKILL.md) drives the collaborative front
of the pipeline: **brainstorm the design → formalize a `design`/`feature_spec` doc → (greenfield)
create the repo → decompose into bite-size, model-pinned tasks → register the project, document, and
tasks via the API.** It proposes and takes positions but **stops for the human's decision** at every
design choice, task boundary, and spec — it never finalizes alone. Its decomposition rules encode the
system's conventions: no code in a spec, every coding task is Haiku-sized (decompose finer rather than
escalate to a bigger model), and same-file tasks are dependency-ordered to avoid the merge-conflict
trap. Ships helper scripts (`scripts/agentask.sh`, `scripts/create-repo.sh`). Triggers on requests
like *"let's break this down for the board"* or *"decompose this feature into Agentask tasks"*.

### `review` — the human merge gate

[`skills/review/`](./skills/review/SKILL.md) is a conversational wrapper for the
**human review gate**: show the queue of tasks awaiting a decision (`agentask pending`), show one
task's diff (`agentask diff`), and — **only on the human's explicit instruction** — record the
verdict (`agentask approve` / `agentask reject --note …`). It never forms its own opinion; the human
supplies the judgment and the CLI does the mechanics (the state transition, and in `local_commit`
mode the branch freeze + worktree cleanup). Triggers on *"what's waiting for review?"*, *"show me the
diff for <task>"*, *"approve <task>"*, or *"reject <task> because …"*.

### Installing

Both skills live under [`skills/`](./skills/) and install **three ways — pick one**. Whichever you
use, each skill needs `AGENTASK_URL` and `AGENTASK_TOKEN` in your environment (and, for `review` in
`local_commit` mode, `AGENTASK_REPO`), and triggers automatically on requests matching its
`description` — there is no separate enable step.

**1. Marketplace (recommended) — installs both, globally.** This repo is itself a Claude Code plugin
marketplace ([`.claude-plugin/marketplace.json`](./.claude-plugin/marketplace.json)); the `agentask`
plugin bundles both skills:

```text
/plugin marketplace add boldfield/agentask
/plugin install agentask@agentask
/reload-plugins
```

They're then available in every session, namespaced by the plugin: **`/agentask:agentask-breakdown`**
and **`/agentask:review`**. (A marketplace can also be added from a local path or any git URL — e.g.
`/plugin marketplace add ./` from a checkout — and managed from the interactive `/plugin` menu.)

**2. Symlink or copy into a skills directory.** A Claude Code skill is just a directory containing a
`SKILL.md`, auto-discovered under `~/.claude/skills/` (personal, every project) or
`<repo>/.claude/skills/` (one project). This gives them **bare** names (`/agentask-breakdown`,
`/review`) rather than the plugin namespace:

```bash
mkdir -p ~/.claude/skills
# symlink (tracks upstream changes) — or `cp -r` for a frozen copy:
ln -s "$PWD/skills/agentask-breakdown" ~/.claude/skills/agentask-breakdown
ln -s "$PWD/skills/review"             ~/.claude/skills/review
# …or scope either to a single project under <that-repo>/.claude/skills/ instead.
```

**3. Project-local in another repo.** Drop (copy/symlink) a skill into a target repo's
`.claude/skills/` so it's available only when you work in that repo — handy for `agentask-breakdown`
in whatever repo you're scaffolding boards from.

`agentask-breakdown`'s helper scripts (`scripts/agentask.sh`, `scripts/create-repo.sh`) travel with
its directory; keep them executable (`chmod +x`).

## Documentation

- [`DESIGN.md`](./DESIGN.md) — MVP design document with detailed state machine, atomic claiming, and review semantics.
- [`docs/api.md`](./docs/api.md) — Complete REST API reference with all endpoints, request/response formats, and examples.
- [`harness/README.md`](./harness/README.md) — Worker and reviewer harness design, configuration, multi-project mode, GitHub auth, and running self-contained inside an `sbx` sandbox (`sbx.sh`).
- [`skills/agentask-breakdown/SKILL.md`](./skills/agentask-breakdown/SKILL.md) & [`skills/review/SKILL.md`](./skills/review/SKILL.md) — Claude Code skills for decomposing intent onto the board and driving the human review gate (installable via the plugin marketplace; see **Skills**).
- [`docs/features/`](./docs/features/) — Feature specifications for deeper Agentask subsystems.

## Status

The MVP is in active development. Core features (projects, documents, tasks, atomic claiming,
review, and crash recovery) are complete and tested. The system is ready for multi-agent fleets
to drain boards in production-like scenarios.
