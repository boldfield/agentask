#!/usr/bin/env bash
# Opus implementation worker (e.g. prompt-authoring tasks) — thin wrapper over the unified engine.
# Keep the slot DISTINCT from any reviewer's slot (separate id + worktree).
# Usage: worker-opus.sh [slot]   (slot defaults to "opus-worker-1")
set -uo pipefail
src="${BASH_SOURCE[0]}"; while [ -h "$src" ]; do d="$(cd -P "$(dirname "$src")" && pwd)"; src="$(readlink "$src")"; [[ $src != /* ]] && src="$d/$src"; done
exec "$(cd -P "$(dirname "$src")" && pwd)/agent.sh" --model opus --kind implement "${1:-opus-worker-1}"
