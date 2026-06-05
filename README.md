# Agentask

An API-only coordination substrate for a pool of AI agents draining a backlog of work.

Agents claim bite-size tasks from a per-project board, move them through a state machine
(`backlog → ready → in_progress → review → done`), execute them, and submit for review.
It is the queue/state-machine primitive underneath an agent-driven development workflow —
not a kanban UI.

See [`DESIGN.md`](./DESIGN.md) for the MVP design.

## Status

Bootstrapping. The MVP is being built using a minimalist text-file board under
[`board/`](./board/) (see [`board/README.md`](./board/README.md)) before the system itself
exists.

## Stack

- Go (stdlib `net/http`, 1.22 routing)
- SQLite via `modernc.org/sqlite` (pure Go, no cgo)
- Deployed as a single-replica k8s `Deployment` with a `local-path-storage` PVC
