#!/usr/bin/env sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
real_cf=${CFS_REAL_CF:-$(command -v cf)}
test_root=$(mktemp -d "${TMPDIR:-/tmp}/cfs-real-smoke.XXXXXX")
project_a=$(mktemp -d "${TMPDIR:-/tmp}/cfs-project-a.XXXXXX")
project_b=$(mktemp -d "${TMPDIR:-/tmp}/cfs-project-b.XXXXXX")

cleanup() {
  rm -rf -- "$test_root" "$project_a" "$project_b"
}
trap cleanup EXIT INT TERM

global_config=${CF_HOME:-$HOME}/.cf/config.json
if [ -f "$global_config" ]; then
  global_before=$(cksum "$global_config")
else
  global_before=absent
fi

(cd "$repo_root" && go build -o "$test_root/cfs" ./cmd/cfs)

CFS_CONFIG_FILE="$test_root/config/config.json" \
CFS_STATE_HOME="$test_root/state" \
CFS_SHIM_DIR="$test_root/shims" \
  "$test_root/cfs" setup --real-cf="$real_cf" >/dev/null

PATH="$test_root/shims:$PATH" \
CFS_CONFIG_FILE="$test_root/config/config.json" \
CFS_STATE_HOME="$test_root/state" \
CFS_WORKSPACE_ROOT="$project_a" \
CF_HOME= CF_PLUGIN_HOME= \
  cf config --color false >/dev/null

PATH="$test_root/shims:$PATH" \
CFS_CONFIG_FILE="$test_root/config/config.json" \
CFS_STATE_HOME="$test_root/state" \
CFS_WORKSPACE_ROOT="$project_b" \
CF_HOME= CF_PLUGIN_HOME= \
  cf config --color true >/dev/null

context_count=0
for context_dir in "$test_root"/state/contexts/*; do
  if [ -d "$context_dir" ]; then
    context_count=$((context_count + 1))
  fi
done
if [ "$context_count" != "2" ]; then
  printf 'FAIL: expected 2 isolated contexts, found %s\n' "$context_count" >&2
  exit 1
fi

false_count=$(grep -l '"ColorEnabled": "false"' "$test_root"/state/contexts/*/home/.cf/config.json | wc -l | tr -d ' ')
true_count=$(grep -l '"ColorEnabled": "true"' "$test_root"/state/contexts/*/home/.cf/config.json | wc -l | tr -d ' ')
if [ "$false_count" != "1" ] || [ "$true_count" != "1" ]; then
  printf 'FAIL: expected one false and one true color configuration\n' >&2
  exit 1
fi

if [ -f "$global_config" ]; then
  global_after=$(cksum "$global_config")
else
  global_after=absent
fi
if [ "$global_before" != "$global_after" ]; then
  printf 'FAIL: the global CF configuration changed\n' >&2
  exit 1
fi

printf 'PASS: two workspaces wrote independent CF configurations\n'
printf 'PASS: the global CF configuration was unchanged\n'
