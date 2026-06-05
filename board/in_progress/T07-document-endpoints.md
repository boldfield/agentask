---
id: T07
title: Document endpoints — register / list
state: in_progress
document: DESIGN.md
depends_on: [T06]
---

## Spec
- `POST /projects/{id}/documents` `{kind, title, ref, commit?}` → 201.
  - `kind` ∈ {`design`, `feature_spec`}. Enforce **at most one** `design` per project (409 on a second).
- `GET /projects/{id}/documents` → list, optional `?kind=` filter.
- Store methods `CreateDocument`, `ListDocuments`.

## Acceptance criteria
- Registering a `feature_spec` then listing returns it.
- Second `design` for the same project → 409.
- `kind` outside the allowed set → 400.
