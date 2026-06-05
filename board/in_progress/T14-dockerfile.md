---
id: T14
title: Dockerfile — multi-stage static binary
state: in_progress
document: DESIGN.md
depends_on: [T01]
---

## Spec
- Multi-stage build: `golang:1.22` builder → `CGO_ENABLED=0 go build` (pure-Go SQLite makes
  this clean) → copy the static binary onto `gcr.io/distroless/static` (or `scratch`).
- Non-root user, expose the API port, `ENTRYPOINT ["/agentask"]`.
- `.dockerignore` to keep the context small.

## Acceptance criteria
- `docker build` produces an image that runs and serves `GET /healthz`.
- Final image contains no shell/toolchain (distroless/scratch).
