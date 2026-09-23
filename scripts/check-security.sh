#!/usr/bin/env sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
output_root=$(mktemp -d "${TMPDIR:-/tmp}/cfs-security.XXXXXX")
tool_root="$output_root/tools"
binary="$output_root/cfs"
gitleaks_config="$repo_root/.gitleaks.toml"

cleanup() {
  find "$output_root" -depth -delete
}
trap cleanup EXIT INT TERM

mkdir -p "$tool_root"
GOBIN="$tool_root" go install golang.org/x/vuln/cmd/govulncheck@v1.8.0
GOBIN="$tool_root" go install github.com/zricethezav/gitleaks/v8@v8.30.1
GOBIN="$tool_root" go install github.com/securego/gosec/v2/cmd/gosec@v2.29.0

cd "$repo_root"
if printf '{"%s":"%s"}\n' \
  "RefreshToken" \
  "cf-refresh-7Zp4xM2qN8vK6tR1wB9yH3dF5sJ0aL2c" | \
  "$tool_root/gitleaks" stdin \
    --config "$gitleaks_config" \
    --no-banner \
    --no-color \
    --redact \
    --exit-code 37 >/dev/null 2>&1; then
  printf 'custom Cloud Foundry secret rule did not detect its test fixture\n' >&2
  exit 1
else
  status=$?
  if [ "$status" -ne 37 ]; then
    printf 'custom Cloud Foundry secret rule check failed with status %s\n' "$status" >&2
    exit "$status"
  fi
fi

"$tool_root/govulncheck" ./...
go build -trimpath -o "$binary" ./cmd/cfs
"$tool_root/govulncheck" -mode=binary "$binary"
"$tool_root/gosec" -quiet ./...
"$tool_root/gitleaks" git --config "$gitleaks_config" --no-banner --no-color --redact "$repo_root"
"$tool_root/gitleaks" dir --config "$gitleaks_config" --no-banner --no-color --redact "$repo_root"
