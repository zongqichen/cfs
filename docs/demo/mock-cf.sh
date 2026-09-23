#!/usr/bin/env sh

set -eu

command=${1:-}
if [ "$#" -gt 0 ]; then
  shift
fi

cf_home=${CF_HOME:?cfs must provide CF_HOME}
state_dir="$cf_home/.cf"
target_file="$state_dir/demo-target"

show_target() {
  if [ ! -f "$target_file" ]; then
    printf "Not logged in. Use 'cf login' to log in.\n" >&2
    exit 1
  fi

  {
    IFS= read -r api
    IFS= read -r org
    IFS= read -r space
  } <"$target_file"

  printf 'API endpoint:   %s\n' "$api"
  printf 'API version:    3.210.0\n'
  printf 'user:           demo@example.com\n'
  printf 'org:            %s\n' "$org"
  printf 'space:          %s\n' "$space"
}

case "$command" in
  login)
    api=
    org=
    space=
    while [ "$#" -gt 0 ]; do
      case "$1" in
        -a | --api)
          api=${2:?missing API endpoint}
          shift 2
          ;;
        -o | --organization)
          org=${2:?missing organization}
          shift 2
          ;;
        -s | --space)
          space=${2:?missing space}
          shift 2
          ;;
        *)
          shift
          ;;
      esac
    done

    : "${api:?login requires an API endpoint}"
    : "${org:?login requires an organization}"
    : "${space:?login requires a space}"

    umask 077
    mkdir -p "$state_dir"
    printf '%s\n%s\n%s\n' "$api" "$org" "$space" >"$target_file"
    printf '{"Target":"%s"}\n' "$api" >"$state_dir/config.json"

    printf 'API endpoint: %s\n' "$api"
    printf 'Authenticating...\nOK\n\n'
    printf 'Targeted org %s.\n' "$org"
    printf 'Targeted space %s.\n\n' "$space"
    show_target
    ;;
  target)
    show_target
    ;;
  version)
    printf 'cf version 8.18.3+demo\n'
    ;;
  *)
    printf 'The README demo fixture supports only login, target, and version.\n' >&2
    exit 2
    ;;
esac
