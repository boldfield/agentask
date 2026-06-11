#!/usr/bin/env bash
# Merger — non-LLM drains merge-kind tasks. Thin wrapper over the unified engine (agent.sh).
# Claim and merge PRs without invoking claude.
# Usage: merger.sh [slot]   (slot defaults to "merger-1")
set -uo pipefail
src="${BASH_SOURCE[0]}"; while [ -h "$src" ]; do d="$(cd -P "$(dirname "$src")" && pwd)"; src="$(readlink "$src")"; [[ $src != /* ]] && src="$d/$src"; done
exec "$(cd -P "$(dirname "$src")" && pwd)/agent.sh" --kind merge "${1:-merger-1}"
