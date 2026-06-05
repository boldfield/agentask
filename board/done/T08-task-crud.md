---
id: T08
title: Task create (bulk) / get / list with filters
state: done
document: DESIGN.md
depends_on: [T03, T05]
---

## Spec
- `POST /projects/{id}/tasks` accepts an **array** of tasks:
  `{title, spec, document_id?, depends_on?: [task_id]}`. Created in state `backlog`.
  Insert tasks and their `task_dep` edges in one transaction. Reject edges referencing
  unknown/other-project tasks (400).
- `GET /tasks/{id}` → task incl. its `depends_on` and `links`.
- `GET /projects/{id}/tasks?state=&assignee=&claimable=` — filters compose.
  - `claimable=true` ⇒ `state='ready'` AND all deps `done` AND (no lease OR lease expired).
    (Reuse the predicate from the claim query in T09 — keep it in one place.)
- Store methods `CreateTasks`, `GetTask`, `ListTasks(filter)`.

## Acceptance criteria
- Bulk create with a valid `depends_on` edge persists both tasks and the edge.
- `?state=backlog` returns only backlog tasks.
- A task with an unfinished dep is excluded from `claimable=true`.

## Result
PR: https://github.com/boldfield/agentask/pull/10
Branch: agentask/T08-task-crud
Head SHA: 274124eba6a5576bf40ead6c7e0c77a8edac63f0
