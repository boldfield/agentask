#!/usr/bin/env bash
# harness_test.sh — smoke tests for agent.sh and fleet.sh local_commit mode support
set -uo pipefail

HARNESS_DIR="$(cd -P "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT_TO_TEST="$HARNESS_DIR/agent.sh"
FLEET_SCRIPT="$HARNESS_DIR/fleet.sh"

test_count=0
pass_count=0
fail_count=0

test_pass() {
  ((test_count++))
  ((pass_count++))
  echo "✓ $1"
}

test_fail() {
  ((test_count++))
  ((fail_count++))
  echo "✗ $1"
}

echo "=== Smoke tests for local_commit harness support ==="
echo ""

# Test 1: Check that agent.sh has DELIVERY_MODE check
echo "Test 1: agent.sh validates DELIVERY_MODE"
if grep -q 'DELIVERY_MODE.*pull_request' "$SCRIPT_TO_TEST" && grep -q 'pull_request|local_commit' "$SCRIPT_TO_TEST"; then
  test_pass "agent.sh has DELIVERY_MODE check"
else
  test_fail "agent.sh missing DELIVERY_MODE check"
fi

# Test 2: Check that agent.sh has local_commit branch in SINGLE-PROJECT mode
echo "Test 2: agent.sh has local_commit branch in SINGLE-PROJECT"
if grep -q 'if \[ "$DELIVERY_MODE" = "local_commit" \]; then' "$SCRIPT_TO_TEST"; then
  test_pass "agent.sh has local_commit conditional"
else
  test_fail "agent.sh missing local_commit conditional"
fi

# Test 3: Check that AGENTASK_WORKTREE_HOME is required in local_commit
echo "Test 3: agent.sh requires AGENTASK_WORKTREE_HOME in local_commit"
if grep -q 'AGENTASK_WORKTREE_HOME.*local_commit' "$SCRIPT_TO_TEST"; then
  test_pass "agent.sh validates AGENTASK_WORKTREE_HOME"
else
  test_fail "agent.sh missing AGENTASK_WORKTREE_HOME validation"
fi

# Test 4: Check that agent.sh exports AGENTASK_WORKTREE_HOME
echo "Test 4: agent.sh exports AGENTASK_WORKTREE_HOME in local_commit"
if grep -q 'export AGENTASK_WORKTREE_HOME' "$SCRIPT_TO_TEST"; then
  test_pass "agent.sh exports AGENTASK_WORKTREE_HOME"
else
  test_fail "agent.sh doesn't export AGENTASK_WORKTREE_HOME"
fi

# Test 5: Check that clone is skipped in local_commit (no ensure_clone call)
echo "Test 5: agent.sh local_commit skips clone"
if grep -A 30 'if \[ "$DELIVERY_MODE" = "local_commit" \]' "$SCRIPT_TO_TEST" | grep -q 'ensure_clone'; then
  test_fail "agent.sh still calls ensure_clone in local_commit mode"
else
  test_pass "agent.sh skips clone in local_commit mode"
fi

# Test 6: Check that pull_request path still has worktree setup
echo "Test 6: agent.sh pull_request mode has worktree setup"
if grep -A 20 'pull_request mode: standard clone' "$SCRIPT_TO_TEST" | grep -q 'git.*worktree add'; then
  test_pass "agent.sh still sets up worktrees in pull_request mode"
else
  test_fail "agent.sh doesn't set up worktrees in pull_request mode"
fi

# Test 7: Check that get_prompt_file selects local_commit prompts
echo "Test 7: get_prompt_file handles local_commit prompts"
if grep -q 'worker-prompt-localcommit' "$SCRIPT_TO_TEST"; then
  test_pass "agent.sh uses worker-prompt-localcommit for local_commit"
else
  test_fail "agent.sh doesn't select local_commit prompt"
fi

# Test 8: Check fleet.sh exists and is executable
echo "Test 8: fleet.sh exists"
if [ -x "$FLEET_SCRIPT" ]; then
  test_pass "fleet.sh exists and is executable"
else
  test_fail "fleet.sh missing or not executable"
fi

# Test 9: Check fleet.sh has --delivery-mode flag
echo "Test 9: fleet.sh supports --delivery-mode flag"
if grep -q '\-\-delivery-mode' "$FLEET_SCRIPT"; then
  test_pass "fleet.sh has --delivery-mode flag"
else
  test_fail "fleet.sh missing --delivery-mode flag"
fi

# Test 10: Check fleet.sh exports AGENTASK_DELIVERY_MODE
echo "Test 10: fleet.sh exports AGENTASK_DELIVERY_MODE"
if grep -q 'export AGENTASK_DELIVERY_MODE' "$FLEET_SCRIPT"; then
  test_pass "fleet.sh exports AGENTASK_DELIVERY_MODE"
else
  test_fail "fleet.sh doesn't export AGENTASK_DELIVERY_MODE"
fi

echo ""
echo "=== Test Summary ==="
echo "Total: $test_count | Passed: $pass_count | Failed: $fail_count"

if [ "$fail_count" -eq 0 ]; then
  echo "✓ All smoke tests passed"
  exit 0
else
  echo "✗ Some tests failed"
  exit 1
fi
