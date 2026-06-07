#!/usr/bin/env bash
# create-repo.sh — greenfield repo creation for the agentask-breakdown skill.
# Pluggable forge seam; GitHub (gh) is the only implemented forge for now.
#
#   FORGE=github (default).  Requires: git; and for github, an authenticated `gh`.
#
# Usage: create-repo.sh <name> <private|public> <design-doc-path> [description]
#   <name> may be "repo" (your default account) or "owner/repo" (an org).
# Creates ./<name-leaf>, git-inits it, commits the design doc as DESIGN.md, creates the
# remote, and pushes. Prints the repo URL on stdout — feed it to `agentask.sh create-project`.
set -euo pipefail

FORGE="${FORGE:-github}"
name="${1:?repo name (repo or owner/repo)}"
vis="${2:?visibility: private|public}"
doc="${3:?path to the design doc}"
desc="${4:-}"

[ -f "$doc" ] || { echo "no such design doc: $doc" >&2; exit 1; }
case "$vis" in private|public) ;; *) echo "visibility must be private|public" >&2; exit 1 ;; esac

leaf="${name##*/}"          # strip any owner/ prefix for the local dir
dir="./$leaf"
[ -e "$dir" ] && { echo "target already exists: $dir" >&2; exit 1; }

mkdir -p "$dir"
cp "$doc" "$dir/DESIGN.md"
git -C "$dir" init -q
git -C "$dir" add DESIGN.md
git -C "$dir" commit -q -m "Initial design: $leaf"

forge_github() {
  command -v gh >/dev/null || { echo "gh not found — install + authenticate the GitHub CLI" >&2; exit 1; }
  local args=(repo create "$name" "--$vis" --source "$dir" --remote origin --push)
  [ -n "$desc" ] && args+=(--description "$desc")
  gh "${args[@]}" >/dev/null
  gh repo view "$name" --json url -q .url
}

case "$FORGE" in
  github) forge_github ;;
  *) echo "forge '$FORGE' not implemented (only 'github' for now)" >&2; exit 1 ;;
esac
