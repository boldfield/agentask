---
id: T01
title: Go module + project layout + Makefile
state: review
document: DESIGN.md
depends_on: []
---

## Spec
Initialize the Go project skeleton.

- `go mod init github.com/boldfield/agentask` (Go 1.22+).
- Directory layout:
  - `cmd/agentask/main.go` — entrypoint, prints version and exits 0 for now.
  - `internal/store/` — storage layer (empty placeholder).
  - `internal/api/` — HTTP handlers (empty placeholder).
  - `migrations/` — SQL files.
- `Makefile` with `build`, `run`, `test`, `tidy` targets.
- `.editorconfig` optional.

## Acceptance criteria
- `make build` produces `bin/agentask`.
- `make run` prints a version line and exits cleanly.
- `go vet ./...` and `go test ./...` pass (no tests yet is fine).

## Result

**PR:** https://github.com/boldfield/agentask/pull/1
**Branch:** agentask/T01-project-layout
**Head commit:** ffd1e7a (T01: Initialize Go module and project layout)

All acceptance criteria met and verified.
