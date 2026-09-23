#!/usr/bin/env sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
real_cf=${CFS_REAL_CF:-$(command -v cf)}
test_root=$(mktemp -d "${TMPDIR:-/tmp}/cfs-real-smoke.XXXXXX")
test_user_home="$test_root/home"
project_a="$test_root/projects/a"
project_b="$test_root/projects/b"

cleanup() {
  find "$test_root" -depth -delete
}
trap cleanup EXIT INT TERM

mkdir -p "$test_user_home/.cf" "$project_a" "$project_b"
global_config="$test_user_home/.cf/config.json"
printf '%s\n' '{"ColorEnabled":"sentinel"}' >"$global_config"
global_before=$(cksum "$global_config")

(cd "$repo_root" && go build -o "$test_root/cfs" ./cmd/cfs)

HOME="$test_user_home" \
CFS_CONFIG_FILE="$test_root/config/config.json" \
CFS_STATE_HOME="$test_root/state" \
CFS_SHIM_DIR="$test_root/shims" \
  "$test_root/cfs" setup --real-cf="$real_cf" >/dev/null

HOME="$test_user_home" \
PATH="$test_root/shims:$PATH" \
CFS_CONFIG_FILE="$test_root/config/config.json" \
CFS_STATE_HOME="$test_root/state" \
CFS_WORKSPACE_ROOT="$project_a" \
CF_HOME= CF_PLUGIN_HOME= \
  cf config --color false >/dev/null

HOME="$test_user_home" \
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

global_after=$(cksum "$global_config")
if [ "$global_before" != "$global_after" ]; then
  printf 'FAIL: the global CF configuration changed\n' >&2
  exit 1
fi

printf 'PASS: two workspaces wrote independent CF configurations\n'
printf 'PASS: the global CF configuration was unchanged\n'
