---
id: T16
title: API reference + curl walkthrough
state: review
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

## Result

**PR:** https://github.com/boldfield/agentask/pull/18  
**Branch:** agentask/T16-api-docs  
**Head SHA:** 74c1f92  
**CI Status:** Both checks passing (test ✓, docker ✓)

**Verification:**
- Built server and ran against throwaway database
- Verified full 12-step lifecycle end-to-end with curl:
  1. GET /healthz (no auth)
  2. POST /projects
  3. POST /projects/{id}/documents (design)
  4. POST /projects/{id}/tasks (bulk with depends_on)
  5. GET /projects/{id}/tasks?state=backlog
  6. POST /tasks/{id}/promote
  7. POST /tasks/{id}/claim (state→in_progress, lease set)
  8. POST /tasks/{id}/heartbeat (lease extended)
  9. POST /tasks/{id}/submit (state→review, links recorded)
  10. POST /tasks/{id}/review (verdict=approve)
  11. POST /tasks/{id}/transition (state→done)
  12. GET /tasks/{id} (final state confirmed)
- Tested all 14 endpoint routes enumerated from api.go
- Verified status codes: 200, 201, 400, 404, 409, 500 with appropriate error codes
- Tested edge cases: missing auth, invalid tokens, 409 conflicts, empty fields
- All tests passing: `make build && go vet ./... && go test -count=1 ./...`
