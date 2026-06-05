# Bootstrap board

A minimalist, text-file kanban used to build the Agentask MVP before Agentask exists.
This mirrors the system's own model so we dogfood it.

## Layout

```
board/
  backlog/       # not yet ready to claim
  ready/         # claimable now (deps satisfied, blessed by planner)
  in_progress/   # being worked
  review/        # awaiting human review
  done/          # complete
```

One markdown file per task. **Moving a task = `git mv` it between state dirs.**

## Task file format

```markdown
---
id: T07
title: Atomic claim query + endpoint
state: ready
document: DESIGN.md
depends_on: [T03, T06]
---

## Spec
<what to build, scoped for a junior to execute>

## Acceptance criteria
- <checkable outcome>
```

During bootstrap there is no automated dependency or promotion gate — promotion is manual.
A task lives in `ready/` only once its `depends_on` tasks are in `done/`. Keep the
`state:` frontmatter in sync with the directory the file lives in.
