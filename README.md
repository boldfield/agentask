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
                       ▲                    │                     │
                       └──── lease expiry ──┘             reject──┘ (→ ready)

                      blocked / failed are off-ramps
```

- **backlog**: Initial state. Task is not yet claimable.
- **ready**: Human has promoted it. Task is claimable (subject to dependencies).
- **in_progress**: Agent has claimed it and is executing. Lease governs crash recovery.
- **review**: Agent submitted work. Reviewers vote. On rejection, task returns to `ready`.
- **approved**: All reviewers voted approve. Human merges.
- **done**: Work is merged.
- **blocked / failed**: Off-ramps from any state, for unblocked or abandoned tasks.

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

## How to Run It

### Build

```bash
# Server
make build
./bin/agentask

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
./bin/agentask
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

- **Workers**: Claim `implement` tasks, execute them via `claude -p`, and submit results.
  - `worker-haiku.sh`: Haiku implementer.
  - `worker-opus.sh`: Opus implementer (e.g., for prompt authoring).
- **Reviewers**: Claim `review` tasks, run `claude -p` to produce verdicts, and submit votes.
  - `reviewer-opus.sh`: Opus reviewer.
  - `reviewer-sonnet.sh`: Sonnet reviewer.

Each agent:
1. Polls for claimable work of its `(model, kind)`.
2. Claims a task atomically.
3. Dispatches one `claude -p` task (with a prompt that includes the spec).
4. Waits for completion.
5. Submits the result and repeats.

Agents stand up their own git worktrees (one per repo), so multiple agents can work in parallel
without stepping on each other.

**Running the Harness**

```bash
cd harness
export AGENTASK_PROJECT="<project-id>"
export AGENTASK_REPO="~/projects/<repo>"

# Start workers in separate terminals:
./worker-haiku.sh haiku-1
./worker-opus.sh opus-1

# Start reviewers in separate terminals:
./reviewer-opus.sh opus-reviewer-1
./reviewer-sonnet.sh sonnet-reviewer-1
```

For multi-project mode and advanced configuration, see [`harness/README.md`](./harness/README.md).

## Documentation

- [`DESIGN.md`](./DESIGN.md) — MVP design document with detailed state machine, atomic claiming, and review semantics.
- [`docs/api.md`](./docs/api.md) — Complete REST API reference with all endpoints, request/response formats, and examples.
- [`harness/README.md`](./harness/README.md) — Worker and reviewer harness design, configuration, multi-project mode, and GitHub auth.
- [`docs/features/`](./docs/features/) — Feature specifications for deeper Agentask subsystems.

## Status

The MVP is in active development. Core features (projects, documents, tasks, atomic claiming,
review, and crash recovery) are complete and tested. The system is ready for multi-agent fleets
to drain boards in production-like scenarios.
