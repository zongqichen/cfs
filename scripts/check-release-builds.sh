#!/usr/bin/env sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
output_root=$(mktemp -d "${TMPDIR:-/tmp}/cfs-release-builds.XXXXXX")

cleanup() {
  find "$output_root" -depth -delete
}
trap cleanup EXIT INT TERM

version=0.0.0-test
commit=release-check
build_date=2026-01-01T00:00:00Z

for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
  goos=${target%/*}
  goarch=${target#*/}
  destination="$output_root/$goos-$goarch/cfs"
  mkdir -p "$(dirname -- "$destination")"
  (
    cd "$repo_root"
    CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build \
      -trimpath \
      -ldflags "-s -w -X main.version=$version -X main.commit=$commit -X main.buildDate=$build_date" \
      -o "$destination" \
      ./cmd/cfs
  )
  test -s "$destination"
done

native="$output_root/$(go env GOOS)-$(go env GOARCH)/cfs"
version_output=$("$native" version)
printf '%s\n' "$version_output" | grep -F "cfs $version" >/dev/null
printf '%s\n' "$version_output" | grep -F "commit $commit" >/dev/null
printf '%s\n' "$version_output" | grep -F "built $build_date" >/dev/null

printf 'PASS: release binaries build for Linux and macOS on amd64 and arm64\n'
printf 'PASS: release metadata is embedded in the native binary\n'
