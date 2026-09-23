#!/usr/bin/env bash

set -euo pipefail

repo_root=$(git rev-parse --show-toplevel)
demo_root=$(mktemp -d "${TMPDIR:-/tmp}/cfs-readme-demo.XXXXXX")
original_path=$PATH

cleanup_demo() {
  find "$demo_root" -depth -delete
}
trap cleanup_demo EXIT

mkdir -p "$demo_root/bin" "$demo_root/home/work/orders" "$demo_root/home/work/payments"
printf 'version = 1\n' >"$demo_root/home/work/orders/.cfs.toml"
printf 'version = 1\n' >"$demo_root/home/work/payments/.cfs.toml"

go build -o "$demo_root/bin/cfs" ./cmd/cfs

export HOME="$demo_root/home"
export CFS_CONFIG_FILE="$demo_root/config/config.json"
export CFS_STATE_HOME="$demo_root/state"
export CFS_SHIM_DIR="$demo_root/shims"

"$demo_root/bin/cfs" setup \
  --real-cf="$repo_root/docs/demo/mock-cf.sh" \
  --shim-dir="$CFS_SHIM_DIR" >/dev/null

export PATH="$CFS_SHIM_DIR:$demo_root/bin:$original_path"
export PS1='\[\033[1;36m\]\w\[\033[0m\] $ '
unset PROMPT_COMMAND
cd "$HOME/work"
