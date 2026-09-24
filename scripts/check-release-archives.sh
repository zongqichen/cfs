#!/usr/bin/env bash

set -euo pipefail

dist_dir=${1:-dist}
readonly dist_dir
readonly checksum_file="$dist_dir/checksums.txt"
readonly expected_members=$'CHANGELOG.md\nLICENSE\nNOTICE\nREADME.md\ncfs'
readonly expected_archive_count=4
readonly document_mode=-rw-r--r--
readonly executable_mode=-rwxr-xr-x

fail() {
  echo "release archive check: $*" >&2
  exit 1
}

[[ $# -le 1 ]] || fail "usage: $0 [dist-directory]"
[[ -d "$dist_dir" ]] || fail "distribution directory not found: $dist_dir"
[[ -f "$checksum_file" ]] || fail "checksum file not found: $checksum_file"

declare -a archives=()
for target in darwin_amd64 darwin_arm64 linux_amd64 linux_arm64; do
  mapfile -t matches < <(find "$dist_dir" -maxdepth 1 -type f \
    -name "cfs_*_${target}.tar.gz" -print | LC_ALL=C sort)
  [[ ${#matches[@]} -eq 1 ]] ||
    fail "expected one $target archive, found ${#matches[@]}"
  archives+=("${matches[0]}")
done

mapfile -t all_archives < <(find "$dist_dir" -maxdepth 1 -type f \
  -name 'cfs_*.tar.gz' -print | LC_ALL=C sort)
[[ ${#all_archives[@]} -eq $expected_archive_count ]] ||
  fail "expected $expected_archive_count archives, found ${#all_archives[@]}"

for archive in "${archives[@]}"; do
  members=$(tar -tzf "$archive" | LC_ALL=C sort)
  if [[ "$members" != "$expected_members" ]]; then
    echo "release archive check: unexpected contents in $(basename "$archive")" >&2
    diff -u <(printf '%s\n' "$expected_members") <(printf '%s\n' "$members") >&2 || true
    exit 1
  fi

  listing=$(tar --numeric-owner --full-time -tzvf "$archive")
  while read -r mode owner _ date time member; do
    [[ "$owner" == "0/0" ]] ||
      fail "unexpected owner for $member in $(basename "$archive"): $owner"
    case "$member" in
      cfs) [[ "$mode" == "$executable_mode" ]] ||
        fail "unexpected mode for cfs in $(basename "$archive"): $mode" ;;
      *) [[ "$mode" == "$document_mode" ]] ||
        fail "unexpected mode for $member in $(basename "$archive"): $mode" ;;
    esac
    printf '%s %s\n' "$date" "$time"
  done <<<"$listing" | {
    mapfile -t mtimes
    [[ $(printf '%s\n' "${mtimes[@]}" | LC_ALL=C sort -u | wc -l) -eq 1 ]] ||
      fail "archive members do not share one deterministic timestamp"
  }
done

mapfile -t checksum_names < <(awk 'NF == 2 { print $2 }' "$checksum_file" | LC_ALL=C sort)
mapfile -t archive_names < <(printf '%s\n' "${archives[@]##*/}" | LC_ALL=C sort)
[[ "${checksum_names[*]}" == "${archive_names[*]}" ]] ||
  fail "checksums.txt must contain exactly one entry for each release archive"

(cd "$dist_dir" && sha256sum --check --strict --quiet checksums.txt) ||
  fail "release archive checksum verification failed"

printf 'PASS: release archives contain only the binary and approved distribution files\n'
printf 'PASS: release archive metadata and checksums are valid\n'
