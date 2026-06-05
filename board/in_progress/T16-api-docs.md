---
id: T16
title: API reference + curl walkthrough
state: in_progress
document: DESIGN.md
depends_on: [T13]
---

## Spec
- `docs/api.md`: every endpoint with method, path, request/response JSON, status codes.
- A copy-paste `curl` walkthrough of the full lifecycle:
  create project → register design doc → bulk-create tasks → promote → claim → heartbeat →
  submit (with links) → review approve → transition done.
- Link it from the README.

## Acceptance criteria
- Following the walkthrough end-to-end against a running instance works as written.
- Every implemented endpoint is documented.
