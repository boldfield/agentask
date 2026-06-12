# local_commit delivery mode — task breakdown (FOR REVIEW, not yet on the board)

**Target board:** project `83c4152e`, new document **"local_commit delivery mode"**.
**Delivery mode for THIS build:** `pull_request` (we are *building* local_commit, not using it). Targets `boldfield/agentask`.
**Models:** every task `review_models = [opus, sonnet]`. Author = **haiku** for all coding; **opus** for prompts (H1, H2) and skill (S1, S2).
**Status:** 22 tasks. Author + hold. Cut to the board only on your go.

## Shared conventions (pinned contracts — every task inherits these; do not re-derive)
- **New package:** `internal/localcommit`. Pure-ish Go wrappers over `git`, each with a `_test.go` using a temp git repo (`t.TempDir()` + `git init`).
- **Env:** `AGENTASK_DELIVERY_MODE` ∈ {`pull_request` (default), `local_commit`}; `AGENTASK_WORKTREE_HOME` (durable worktree root; fallback `$AGENTASK_HOME`).
- **Refs:** base = `origin/main`; MR branch = `wi/<slug>`; per-item WIP = `wip/<iid>`. `<iid>` = the task id; `<slug>` = `Slugify(document title)`.
- **Link kind `commit` already exists** server-side (`internal/store/store.go:1531` `validKinds`) — no server change for the SHA link.
- **CLI dispatch:** all verbs add a `case` in `cmd/agentask/main.go` (line ~72) + an `executeXxx`. **Any task touching `main.go` is part of the U→V CHAIN** (linear `depends_on`) — never parallel — because they serialize-conflict on that file (the 2026-06-09 lesson).
- **Commit messages for CLI-generated local_commit commits:** SHORT, templated from task title, **NO `Co-Authored-By` trailer** (deliberate scoped override).
- **Existing typed client:** `internal/tuiclient` already has `GetTask`, `ListTasks`, `SubmitTask`, `TransitionTask`, `ReviewTask`, `ClaimTask`, `PromoteTask`. Reuse — do not hand-roll HTTP.

---

# Group L — `internal/localcommit` library · author **haiku** · PARALLEL (each its own file)

### L1 — `localcommit/refs.go`: ref + slug helpers
- **Files:** `internal/localcommit/refs.go`, `refs_test.go`
- **Contract:**
  - `func Slugify(title string) string` — lowercase, spaces/underscores→`-`, strip non-`[a-z0-9-]`, collapse repeated `-`, trim leading/trailing `-`. Empty→`item`.
  - `func BaseRef() string` → `"origin/main"`.
  - `func MRBranch(slug string) string` → `"wi/" + slug`.
  - `func WIPBranch(iid string) string` → `"wip/" + iid`.
  - `func ResolveTip(repoDir, slug string) (string, error)` — if `git -C repoDir rev-parse --verify --quiet wi/<slug>` succeeds → return `"wi/"+slug`; else return `BaseRef()`.
- **Tests:** Slugify table (incl. unicode/empty/already-slug); ResolveTip with and without an existing `wi/<slug>` in a temp repo.
- **Deps:** none. **Models:** haiku / [opus, sonnet].

### L2 — `localcommit/env.go`: delivery-mode read
- **Files:** `internal/localcommit/env.go`, `env_test.go`
- **Contract:** `func DeliveryMode() string` (reads `AGENTASK_DELIVERY_MODE`, default `"pull_request"`, lowercased/trimmed); `func IsLocalCommit() bool` (== `"local_commit"`).
- **Tests:** unset→pull_request; `local_commit`→true; mixed case/whitespace; unknown value→treated as pull_request (not local).
- **Deps:** none. **Models:** haiku / [opus, sonnet].

### L3 — `localcommit/home.go`: durable worktree-home resolution
- **Files:** `internal/localcommit/home.go`, `home_test.go`
- **Contract:** `func WorktreeHome() (string, error)` — return `$AGENTASK_WORKTREE_HOME` if set, else `$AGENTASK_HOME`; **error** if neither set, or if the resolved path is under `/tmp` or `/var/folders` (tmpfs durability guard — a bounced item's `wip/<iid>` must survive the rework loop).
- **Tests:** WORKTREE_HOME wins over HOME; HOME fallback; neither→error; `/tmp/...`→error.
- **Deps:** none. **Models:** haiku / [opus, sonnet].

### L4 — `localcommit/worktree_add.go`: create/attach a per-item worktree
- **Files:** `internal/localcommit/worktree_add.go`, `worktree_add_test.go`
- **Contract:** `func AddWorktree(repoDir, iid, tip string) (wtPath string, err error)`:
  - `wtPath = filepath.Join(WorktreeHome(), iid)`.
  - If `wtPath` already exists and is a valid worktree (`git -C repoDir worktree list --porcelain` lists it) → return it (idempotent re-attach for the rework loop).
  - Else `git -C repoDir worktree add <wtPath> -b wip/<iid> <tip>`. **No clean-tree precondition.**
- **Tests:** fresh add (worktree dir exists, on `wip/<iid>`, clean status); re-attach is idempotent (second call no error, same path); tip = base vs tip = an existing `wi/<slug>`.
- **Deps:** L1, L3. **Models:** haiku / [opus, sonnet].

### L5 — `localcommit/commit.go`: commit / amend in a worktree
- **Files:** `internal/localcommit/commit.go`, `commit_test.go`
- **Contract:**
  - `func CommitAll(wtPath, message string) (sha string, err error)` — `git -C wtPath add -A && git -C wtPath commit -m <message>` then return `git -C wtPath rev-parse HEAD`.
  - `func AmendAll(wtPath, message string) (sha string, err error)` — `git -C wtPath add -A && git -C wtPath commit --amend -m <message>` then `rev-parse HEAD`.
  - Both: error if there is nothing to commit on a first `CommitAll` (empty tree); `AmendAll` allowed even with no new changes (message-only re-stamp).
  - **No `Co-Authored-By` trailer.**
- **Tests:** CommitAll returns a real SHA and the tree matches; AmendAll changes the SHA and keeps a single commit on top of base; empty CommitAll errors.
- **Deps:** L1. **Models:** haiku / [opus, sonnet].

### L6 — `localcommit/freeze.go`: advance the MR branch (the only edge that moves it)
- **Files:** `internal/localcommit/freeze.go`, `freeze_test.go`
- **Contract:** `func Freeze(repoDir, slug, iid string) error`, in order:
  1. **Footgun guard:** parse `git -C repoDir worktree list --porcelain`; if any worktree has `branch refs/heads/wi/<slug>` checked out → return an error whose message names the path and the fix: `"MR branch wi/<slug> is checked out at <path>; cd out or run 'git checkout --detach' there, then re-approve"`. Do nothing else.
  2. `git -C repoDir branch -f wi/<slug> wip/<iid>` (ff by construction — `wip/<iid>` was branched from `tip`).
  3. `git -C repoDir worktree remove <WorktreeHome>/<iid>`.
  4. `git -C repoDir branch -d wip/<iid>`.
- **Tests (enumerated — this is the delicate one, and these make it haiku-executable):**
  - **first-freeze:** `wi/<slug>` absent → after Freeze, `wi/<slug>` points at the wip commit; worktree + `wip/<iid>` gone.
  - **ff-advance:** `wi/<slug>` exists and is an ancestor of `wip/<iid>` → `wi/<slug>` advances; still linear.
  - **footgun:** `wi/<slug>` checked out in a second worktree → Freeze returns the error, `wi/<slug>` unmoved, source worktree intact.
  - **already-removed worktree:** step 3 tolerates a missing worktree (no hard error).
- **Deps:** L1. **Models:** haiku / [opus, sonnet].

### L7 — `localcommit/cleanup.go`: abandon cleanup (MR branch untouched)
- **Files:** `internal/localcommit/cleanup.go`, `cleanup_test.go`
- **Contract:** `func CleanupAbandon(repoDir, iid string) error` — `git -C repoDir worktree remove --force <WorktreeHome>/<iid>` then `git -C repoDir branch -D wip/<iid>`; **never touches `wi/<slug>`**; idempotent (already-gone → no error).
- **Tests:** removes worktree + `wip/<iid>`; an existing `wi/<slug>` is unchanged; second call is a no-op.
- **Deps:** L1. **Models:** haiku / [opus, sonnet].

### L8 — `localcommit/show.go`: render a commit for review
- **Files:** `internal/localcommit/show.go`, `show_test.go`
- **Contract:** `func ShowCommit(repoDir, sha string) (string, error)` → `git -C repoDir show <sha>`; `func DiffBase(repoDir, sha string) (string, error)` → `git -C repoDir diff origin/main...<sha>`. Return stdout; error on bad SHA.
- **Tests:** ShowCommit contains the commit subject + patch; DiffBase shows only the item's delta vs base; unknown SHA errors.
- **Deps:** L1. **Models:** haiku / [opus, sonnet].

---

# Group U — universal human-gate verbs · author **haiku** · CHAIN on `main.go` · useful in pull_request mode too

### U1 — `pending --project <id>` verb
- **Files:** `cmd/agentask/main.go` (+ `executepending.go` if splitting), `main_test.go`
- **Contract:** list tasks for a project in state `review` or `approved` (the human-gate queue). Columns: id, state, kind, title. Honors global `--json`. Reuse `tuiclient.ListTasks` with a state filter (or client-side filter).
- **Tests:** filters to review/approved only; `--json` shape; empty → clean exit 0.
- **Deps:** framework. **Models:** haiku / [opus, sonnet].

### U2 — `diff <id>` verb (pull_request path)
- **Files:** `main.go`/`executediff.go`, `main_test.go`
- **Contract:** fetch task; find its `pr` link; print the PR URL and, if `gh` is available, `gh pr diff <url>`. (Local_commit branch added in V3.) Error clearly if no `pr` link in pull_request mode.
- **Tests:** prints PR link; no-pr-link error message.
- **Deps:** U1 (chain). **Models:** haiku / [opus, sonnet].

### U3 — `approve <id>` verb
- **Files:** `main.go`/`executeapprove.go`, `main_test.go`
- **Contract:** transition `approved → done` via `tuiclient.TransitionTask` (the human gate). No git in this task (freeze layered in V4). Validate the task is in `approved` before transitioning; clear error otherwise.
- **Tests:** approved→done happy path; wrong-state error.
- **Deps:** U2 (chain). **Models:** haiku / [opus, sonnet].

### U4 — `reject <id> --note <text>` verb
- **Files:** `main.go`/`executereject.go`, `main_test.go`
- **Contract:** transition `review`/`approved → ready` with the note attached (rework). Reuse `TransitionTask`. `--note` required.
- **Tests:** review→ready with note; approved→ready; missing `--note` error.
- **Deps:** U3 (chain). **Models:** haiku / [opus, sonnet].

---

# Group V — local_commit layer · author **haiku** · CHAIN (continues from U4 on `main.go`)

### V1 — `wt-ensure <id>` verb (worktree add)
- **Files:** `main.go`/`executewtensure.go`, `main_test.go`
- **Contract:** new verb the worker calls right after claim. `IsLocalCommit()` required (error in pull_request mode). Resolve `repoDir` (mounted repo; from `--repo`/`AGENTASK_REPO`), `tip = ResolveTip(repoDir, slug)`, then `AddWorktree(repoDir, iid, tip)`; print `wtPath` to stdout (the worker `cd`s there). Idempotent on re-claim.
- **Tests:** prints worktree path; idempotent second call; pull_request mode → error. (Use a temp repo + stub `IsLocalCommit`.)
- **Deps:** U4 (chain), L4. **Models:** haiku / [opus, sonnet].

### V2 — `submit` local_commit branch
- **Files:** `cmd/agentask/main.go` `executeSubmit` (line ~590), `main_test.go`
- **Contract:** when `IsLocalCommit()`: ignore `--pr/--branch`; build a short message from the task title (or `--message` override); `CommitAll` on first submit, `AmendAll` on a rework re-submit (detect: `wip/<iid>` already has a commit beyond `tip`); then `SubmitTask` attaching a `commit:<sha>` link. pull_request path unchanged.
- **Tests:** first submit → commit + `commit` link with SHA; re-submit → amend (single commit, new SHA); pull_request path still attaches `pr`/`branch`.
- **Deps:** V1 (chain), L5. **Models:** haiku / [opus, sonnet].

### V3 — `diff` local_commit branch
- **Files:** `executediff.go`, `main_test.go`
- **Contract:** when `IsLocalCommit()`: read the task's `commit` link SHA; print `DiffBase(repoDir, sha)` (default) or `ShowCommit` with `--full`. Used by both the LLM reviewers and the human gate.
- **Tests:** local mode prints the base-diff for the linked SHA; missing `commit` link error.
- **Deps:** V2 (chain), L8. **Models:** haiku / [opus, sonnet].

### V4 — `approve` freeze branch
- **Files:** `executeapprove.go`, `main_test.go`
- **Contract:** when `IsLocalCommit()`: after the `approved→done` transition succeeds, call `Freeze(repoDir, slug, iid)`. Order: transition first, then freeze; if freeze hits the footgun, surface its message and **do not** leave the task half-done (document: transition already happened; freeze is re-runnable once the user frees the branch — provide a `--freeze-only` re-run path or instruct re-`approve`).
- **Tests:** local approve → done + `wi/<slug>` advanced + worktree gone; footgun path surfaces the fix message.
- **Deps:** V3 (chain), L6. **Models:** haiku / [opus, sonnet].

### V5 — `reject`/abandon cleanup branch
- **Files:** `executereject.go` (+ abandon path), `main_test.go`
- **Contract:** when `IsLocalCommit()` and the operation is an **abandon** (terminal, not rework): call `CleanupAbandon(repoDir, iid)`. A plain `reject --note` (rework) keeps the worktree (the worker amends in place) — only abandon cleans up. Pin the trigger: `reject --abandon` flag vs `reject --note` (rework).
- **Tests:** `reject --abandon` → worktree+`wip/<iid>` gone, `wi/<slug>` untouched; `reject --note` (rework) → worktree preserved.
- **Deps:** V4 (chain), L7. **Models:** haiku / [opus, sonnet].

---

# Group H — harness

### H1 — local_commit worker prompt · author **opus**
- **Files:** `harness/prompts/local_commit/build/implement.md`
- **Contract:** no clone, no push, no `gh`. Flow: `agentask next --kind implement --claim` → `agentask wt-ensure <id>` (cd into printed worktree) → **edit files only** (the CLI makes every commit) → `agentask submit` (CLI commits + links SHA) → on reject, re-claim, amend in the same worktree, re-submit. Emphasize: never run git commit/branch/push yourself.
- **Tests:** n/a (prompt) — reviewers check it against the verb contracts above.
- **Deps:** V2, V1 (verbs must exist). **Models:** opus / [opus, sonnet].

### H2 — local_commit reviewer prompt · author **opus**
- **Files:** `harness/prompts/local_commit/build/review.md`
- **Contract:** no clone. `agentask next --kind review --claim` → `agentask diff <target-id>` to see the commit (no PR) → submit a verdict via `agentask submit --verdict`. N reviewers screen; the human freezes on approve.
- **Deps:** V3. **Models:** opus / [opus, sonnet].

### H3 — `agent.sh`/`fleet.sh` local_commit mode (shell) · author **haiku**
- **Files:** `harness/agent.sh`, `harness/fleet.sh` (or `~/.agentask/` equivalents)
- **Contract:** branch on `AGENTASK_DELIVERY_MODE=local_commit`: skip clone, set/require `AGENTASK_WORKTREE_HOME`, select the local_commit prompt, drop the push/`gh pr` steps. pull_request path unchanged.
- **Tests:** shell smoke — local mode skips clone and sets the home var; pull_request mode unchanged.
- **Deps:** H1 (prompt exists). **Models:** haiku / [opus, sonnet].

---

# Group S — `/review` skill (the sandbox's only human interface is interactive `claude`)

### S1 — `/review` skill: pending + diff display · author **opus**
- **Files:** `.claude/skills/review/SKILL.md` (+ any helper)
- **Contract:** conversational wrapper that runs `agentask pending --project <id>` and `agentask diff <id>` and presents the queue + the commit/diff for a human decision. Read-only; makes no decision.
- **Deps:** U1, U2/V3. **Models:** opus / [opus, sonnet].

### S2 — `/review` skill: approve / reject actions · author **opus**
- **Files:** `.claude/skills/review/SKILL.md`
- **Contract:** on the human's call, run `agentask approve <id>` (= freeze in local_commit) or `agentask reject <id> --note … [--abandon]`. The skill RECORDS the human's decision; it never makes it (the boundary: CLI does mechanics, human does judgment).
- **Deps:** S1, U3/U4, V4/V5. **Models:** opus / [opus, sonnet].

---

## Dependency summary
- **L1, L2, L3** first (no deps) → **L4–L8** parallel.
- **U1→U2→U3→U4→V1→V2→V3→V4→V5** one linear chain (shared `main.go`).
- **H1, H2** after their verbs exist; **H3** after H1. **S1** after U/V display verbs; **S2** after S1.
- Open self-registering-command refactor (each verb its own file, `init()`-registered) would let U/V run parallel — deferred; chain is the conflict-safe choice now.
