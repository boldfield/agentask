---
id: T05
title: HTTP server skeleton — router, auth, health, JSON helpers
state: done
document: DESIGN.md
depends_on: [T01]
---

## Spec
In `internal/api`:

- `net/http` server using Go 1.22 method+pattern routing (`mux.HandleFunc("POST /projects", ...)`).
- Bearer-token auth middleware: reject requests whose `Authorization: Bearer <token>` does
  not match `AGENTASK_TOKEN`. Exempt `GET /healthz`.
- `GET /healthz` → 200 `{"status":"ok"}`.
- JSON request-decode and response-encode helpers; consistent error envelope
  `{"error":{"code","message"}}` with correct status codes (400/401/404/409/500).
- Wire the server into `cmd/agentask/main.go` (read `AGENTASK_TOKEN`, `AGENTASK_DB`,
  `AGENTASK_ADDR` from env; sane defaults).

## Acceptance criteria
- `GET /healthz` returns 200 without auth.
- A protected route returns 401 without/with a wrong token, and proceeds with the right token.
- Malformed JSON body → 400 with the error envelope.

## Result

- PR: https://github.com/boldfield/agentask/pull/7
- Branch: agentask/T05-http-skeleton
- Head SHA: f74ec17
