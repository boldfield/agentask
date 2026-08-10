#!/usr/bin/env bash
# sbx-agent-setup.sh — idempotently provision agent tooling + board config INSIDE an `sbx`
# sandbox container, after harness/sbx.sh has booted the stack. See
# docs/features/sbx-agent-bootstrap.md (Deliverable 1) for the full design.
#
# What it does, in order:
#   1. Installs codex globally (sudo npm) if it's missing.
#   2. Installs claude globally (sudo npm) ONLY if it's missing — the standard sbx image already
#      ships an authenticated claude, and this script must never disturb that install or its
#      credentials.
#   3. Verifies both binaries actually resolve on PATH (never assumes a package install worked).
#   4. Symlinks (not copies) the repo's four Agentask skills into ~/.claude/skills/, so editing a
#      skill in the mounted repo takes effect immediately with no re-provisioning — the same
#      "code in the repo, state in $HOME" split the rest of the harness already follows.
#   5. Reports clearly if codex is installed but unauthenticated, naming the host-side fix
#      (`make sbx-codex-auth`).
#   6. Writes ~/.claude/CLAUDE.md board instructions (local sandbox board URL/token, and the
#      mandatory two-reviewer review_models pair) and merges a permission allowlist into
#      ~/.claude/settings.json for the local board's routine traffic.
#
# Usage:
#   bash harness/sbx-agent-setup.sh [--port P]   # port defaults to 8080, matching sbx.sh
#
# Safe to re-run: every step here is a clean no-op on a second pass.
set -uo pipefail

# --- resolve our REAL directory, even when invoked via a symlink ---
_src="${BASH_SOURCE[0]}"
while [ -h "$_src" ]; do
  _d="$(cd -P "$(dirname "$_src")" && pwd)"; _src="$(readlink "$_src")"; [[ $_src != /* ]] && _src="$_d/$_src"
done
HARNESS_DIR="$(cd -P "$(dirname "$_src")" && pwd)"
REPO_ROOT="$(cd -P "$HARNESS_DIR/.." && pwd)"

PORT=8080
while [ $# -gt 0 ]; do
  case "$1" in
    --port) PORT="${2:?}"; shift 2 ;;
    -h|--help) sed -n '2,20p' "$_src"; exit 0 ;;
    *) echo "unknown flag: $1" >&2; exit 1 ;;
  esac
done

say() { echo "[sbx-agent-setup] $*"; }
die() { echo "[sbx-agent-setup] ERROR: $*" >&2; exit 1; }

command -v npm >/dev/null 2>&1 || die "npm not on PATH — the sbx image is expected to ship node/npm"
command -v jq  >/dev/null 2>&1 || die "jq not on PATH — the sbx image is expected to ship jq"
command -v sudo >/dev/null 2>&1 || die "sudo not on PATH — a passwordless sudo is expected for global npm installs"

# ============================== 1. install codex (global, root-owned npm prefix) ==============================
if command -v codex >/dev/null 2>&1; then
  say "codex already installed: $(command -v codex)"
else
  say "codex not found; installing globally (sudo npm install -g @openai/codex)…"
  sudo -n npm install -g @openai/codex \
    || die "sudo npm install -g @openai/codex failed — check passwordless sudo works (sudo -n true)"
fi

# ============================== 2. install claude ONLY if genuinely missing ==============================
if command -v claude >/dev/null 2>&1; then
  say "claude already installed: $(command -v claude) — leaving its install and credentials untouched"
else
  say "claude not found; installing globally (sudo npm install -g @anthropic-ai/claude-code)…"
  sudo -n npm install -g @anthropic-ai/claude-code \
    || die "sudo npm install -g @anthropic-ai/claude-code failed — check passwordless sudo works (sudo -n true)"
fi

# ============================== 3. verify — never assume an install worked ==============================
command -v codex >/dev/null 2>&1 \
  || die "codex still not resolving on PATH after install — check the npm global bin dir (npm config get prefix) is on PATH"
command -v claude >/dev/null 2>&1 \
  || die "claude not resolving on PATH — check the npm global bin dir (npm config get prefix) is on PATH, or that the sandbox image's normal claude install is intact"
say "codex:  $(command -v codex)"
say "claude: $(command -v claude)"

# ============================== 4. wire the repo's skills into ~/.claude/skills (symlink, not copy) ==============================
SKILLS_DIR="$HOME/.claude/skills"
mkdir -p "$SKILLS_DIR"
for skill in agentask-board agentask-breakdown agentask-ops review; do
  src="$REPO_ROOT/skills/$skill"
  [ -d "$src" ] || die "expected skill directory missing: $src"
  link="$SKILLS_DIR/$skill"
  if [ -e "$link" ] && [ ! -L "$link" ]; then
    die "$link already exists and is not a symlink — refusing to clobber it"
  fi
  ln -sfn "$src" "$link"
done
say "skills linked -> $SKILLS_DIR (agentask-board, agentask-breakdown, agentask-ops, review)"

# ============================== 5. codex auth sanity ==============================
if [ ! -f "$HOME/.codex/auth.json" ]; then
  say "codex is installed but NOT authenticated (no ~/.codex/auth.json)."
  say "fix it from the HOST with: make sbx-codex-auth SBX_NAME=<sandbox-name>"
fi

# ============================== 6. board config: ~/.claude/CLAUDE.md ==============================
# Lives under $HOME, not the mounted repo — this is sandbox-local throwaway state, not versioned
# project content (same "code in the repo, state in $HOME" split as the skill symlinks above).
mkdir -p "$HOME/.claude"
CLAUDE_MD="$HOME/.claude/CLAUDE.md"
BEGIN_MARK="<!-- BEGIN agentask sbx-agent-setup: local sandbox board -->"
END_MARK="<!-- END agentask sbx-agent-setup: local sandbox board -->"
BLOCK="$BEGIN_MARK
## Agentask sandbox board

This container's Agentask board is a **throwaway local instance** at \`http://localhost:$PORT\`,
authenticated with the fixed sandbox token \`sbx-local-token\`. It is NOT the production cluster —
nothing created here is durable or shared.

Every task you create must carry \`review_models: [\"opus\",\"gpt-5.5\"]\` unless a human explicitly
tells you otherwise. This spawns two independent reviewers in parallel — one Claude (\`opus\`), one
Codex (\`gpt-5.5\`) — so a change has to convince two different models, not just one. Dropping the
pair silently falls back to a single reviewer that can share the same blind spots as the model that
wrote the code.
$END_MARK"

if [ -f "$CLAUDE_MD" ] && grep -qF "$BEGIN_MARK" "$CLAUDE_MD"; then
  tmp="$(mktemp)"
  awk -v begin="$BEGIN_MARK" -v end="$END_MARK" '
    $0 == begin { skip=1; next }
    $0 == end   { skip=0; next }
    !skip { print }
  ' "$CLAUDE_MD" > "$tmp"
  printf '%s\n' "$BLOCK" >> "$tmp"
  mv "$tmp" "$CLAUDE_MD"
else
  printf '%s\n' "$BLOCK" >> "$CLAUDE_MD"
fi
say "board instructions written -> $CLAUDE_MD"

# ============================== 7. ~/.claude/settings.json — merge a permission allowlist ==============================
# Never overwrite: merge our entries into whatever settings the image (or a prior run) already has.
SETTINGS="$HOME/.claude/settings.json"
[ -f "$SETTINGS" ] || echo '{}' > "$SETTINGS"
jq -e . "$SETTINGS" >/dev/null 2>&1 || die "$SETTINGS exists but is not valid JSON — refusing to merge into it"
tmp="$(mktemp)"
jq '.permissions.allow = (((.permissions.allow // []) + [
      "Bash(curl:*)",
      "Bash(agentask:*)",
      "Bash(git:*)"
    ]) | unique)' "$SETTINGS" > "$tmp" \
  || die "failed to merge permission allowlist into $SETTINGS"
mv "$tmp" "$SETTINGS"
say "permission allowlist merged -> $SETTINGS"

say "done. board: http://localhost:$PORT  (token: sbx-local-token)"
