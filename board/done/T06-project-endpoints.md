---
id: T06
title: Project endpoints — create / get
state: done
document: DESIGN.md
depends_on: [T03, T05]
---

## Spec
- `POST /projects` `{name, repo}` → 201 with the created project (server-generated id).
- `GET /projects/{id}` → 200 or 404.
- Validate `name` non-empty; `repo` may be empty (but see workflow — usually set).
- Add the corresponding `Store` methods (`CreateProject`, `GetProject`).

## Acceptance criteria
- Create then get round-trips all fields.
- Get on unknown id → 404 with the error envelope.
- Empty `name` → 400.

## Result

PR: https://github.com/boldfield/agentask/pull/8
Branch: agentask/T06-project-endpoints
Head SHA: 648de47b26b24f99d649e99e48a87e62db9e6798
