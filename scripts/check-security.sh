#!/usr/bin/env sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
output_root=$(mktemp -d "${TMPDIR:-/tmp}/cfs-security.XXXXXX")
tool_root="$output_root/tools"
binary="$output_root/cfs"

cleanup() {
  find "$output_root" -depth -delete
}
trap cleanup EXIT INT TERM

mkdir -p "$tool_root"
GOBIN="$tool_root" go install golang.org/x/vuln/cmd/govulncheck@v1.8.0
GOBIN="$tool_root" go install github.com/zricethezav/gitleaks/v8@v8.30.1
GOBIN="$tool_root" go install github.com/securego/gosec/v2/cmd/gosec@v2.29.0

cd "$repo_root"
"$tool_root/govulncheck" ./...
go build -trimpath -o "$binary" ./cmd/cfs
"$tool_root/govulncheck" -mode=binary "$binary"
"$tool_root/gosec" -quiet ./...
"$tool_root/gitleaks" git --no-banner --no-color --redact "$repo_root"
"$tool_root/gitleaks" dir --no-banner --no-color --redact "$repo_root"
