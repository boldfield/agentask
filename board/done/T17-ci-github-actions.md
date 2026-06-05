---
id: T17
title: CI — GitHub Actions build/vet/test on PRs
state: done
document: DESIGN.md
depends_on: [T01]
---

## Spec
Add continuous integration so every PR is machine-verified before merge. This is also the
foundation for the CI-green review verdict in DESIGN.md §5.

- `.github/workflows/ci.yml`:
  - Triggers: `pull_request` (all branches into `main`) and `push` to `main`.
  - Single job (give it a stable name, e.g. `test`, so branch protection can require it as a
    status check — the check context name must not churn).
  - Steps: `actions/checkout`, `actions/setup-go` pinned to Go 1.22, then run
    `make build`, `go vet ./...`, and `go test -count=1 ./...`.
  - Enable the Go build/module cache via `setup-go` to keep runs fast.
- Do not add deploy/publish steps — build and test only.

## Acceptance criteria
- The workflow runs automatically on the PR that introduces it and the `test` job passes.
- The job fails if `go test` or `go vet` fails (verify the wiring is real, not a no-op).
- The status check appears on the PR with the stable job/context name.

## Result
- PR: https://github.com/boldfield/agentask/pull/4
- Branch: agentask/T17-ci-github-actions
- Head SHA: 1efa55a0790af4993fcc8dc17c860776c99352d2
