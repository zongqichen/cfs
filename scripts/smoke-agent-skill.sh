#!/usr/bin/env bash

set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
codex_bin=${CFS_CODEX_BIN:-codex}
codex_home=${CODEX_HOME:-$HOME/.codex}

if ! command -v "$codex_bin" >/dev/null 2>&1; then
  echo "agent skill smoke: Codex CLI not found: $codex_bin" >&2
  exit 69
fi

test_root=$(mktemp -d "${TMPDIR:-/tmp}/cfs-agent-skill.XXXXXX")
trap 'rm -rf "$test_root"' EXIT

workspace=$test_root/workspace
fixture_bin=$workspace/.smoke/bin
invocations=$workspace/.smoke/invocations
result=$workspace/.smoke/result.txt
isolated_home=$test_root/home
mkdir -p "$workspace/.agents/skills" "$fixture_bin" "$isolated_home"
cp -R "$repo_root/.agents/skills/cfs" "$workspace/.agents/skills/cfs"
cp "$repo_root/test/fixtures/agent-skill/cfs" "$fixture_bin/cfs"
cp "$repo_root/test/fixtures/agent-skill/cf" "$fixture_bin/cf"
git -C "$workspace" init --quiet
chmod +x "$fixture_bin/cfs" "$fixture_bin/cf"
: >"$invocations"

prompt='Use the cfs skill to inspect and then list applications in the existing named context `qa-blue`. Execute the required read-only commands; do not merely describe them. Do not log in or change any context. Report the application name you observe.'

env \
  -u CF_HOME \
  -u CF_PLUGIN_HOME \
  -u CFS_ACTIVE_CONTEXT \
  -u CFS_DISABLE \
  -u CFS_REAL_CF \
  -u CFS_STATE_HOME \
  -u CFS_WORKSPACE_ROOT \
  CFS_AGENT_SMOKE_LOG="$invocations" \
  CODEX_HOME="$codex_home" \
  HOME="$isolated_home" \
  PATH="$fixture_bin:$PATH" \
  "$codex_bin" exec \
  --ephemeral \
  --ignore-rules \
  --sandbox workspace-write \
  --cd "$workspace" \
  --output-last-message "$result" \
  "$prompt"

required_commands=(
  "context list --json"
  "context status qa-blue --json --redact"
  "-c qa-blue apps"
)
next_required=0
unexpected=false
while IFS= read -r invocation; do
  case "$invocation" in
    "context list --json" | "context status qa-blue --json --redact" | "-c qa-blue apps") ;;
    *) unexpected=true ;;
  esac
  if ((next_required < ${#required_commands[@]})) &&
    [[ "$invocation" == "${required_commands[$next_required]}" ]]; then
    next_required=$((next_required + 1))
  fi
done <"$invocations"

if [[ "$unexpected" == true ]] || ((next_required != ${#required_commands[@]})); then
  echo "agent skill smoke: required command sequence was not followed" >&2
  sed 's/^/  /' "$invocations" >&2
  exit 1
fi

if [[ $(<"$result") != *sample-app* ]]; then
  echo "agent skill smoke: Codex did not report the fixture result" >&2
  exit 1
fi

echo "agent skill smoke: passed"
