#!/usr/bin/env bash
set -euo pipefail

REAL_SYSTEMCTL="/usr/bin/systemctl"

action="${XINSIGHT_CI_SIGKILL_ACTION:-stop_service}"
intercept=false
if [ "$action" = "stop_service" ] && [ "$#" -eq 2 ] && [ "$1" = "stop" ] && [ "$2" = "xinsight.service" ]; then
  intercept=true
elif [ "$action" = "restart_service_reload" ] && [ "$#" -eq 1 ] && [ "$1" = "daemon-reload" ]; then
  intercept=true
fi

if [ "$intercept" = true ]; then
  if [ "${GITHUB_ACTIONS:-}" != "true" ] || [ "${XINSIGHT_CI_EPHEMERAL_HOST:-}" != "1" ]; then
    echo "SIGKILL qualification wrapper is restricted to an explicitly disposable GitHub Actions VM" >&2
    exit 97
  fi
  if [ -z "${RUNNER_TEMP:-}" ] || [ -z "${XINSIGHT_CI_SIGKILL_MARKER:-}" ] || [ -z "${XINSIGHT_CI_EXPECTED_CTL:-}" ]; then
    echo "SIGKILL qualification wrapper is missing its bounded CI inputs" >&2
    exit 98
  fi

  marker="$(realpath -m "$XINSIGHT_CI_SIGKILL_MARKER")"
  runner_temp="$(realpath "$RUNNER_TEMP")"
  if [ "$marker" != "$XINSIGHT_CI_SIGKILL_MARKER" ]; then
    echo "SIGKILL marker path must be absolute and clean" >&2
    exit 99
  fi
  case "$marker" in
    "$runner_temp"/*) ;;
    *)
      echo "SIGKILL marker must remain inside RUNNER_TEMP" >&2
      exit 100
      ;;
  esac
  if [ -e "$marker" ]; then
    echo "SIGKILL marker already exists" >&2
    exit 101
  fi

  expected_ctl="$(readlink -f "$XINSIGHT_CI_EXPECTED_CTL")"
  parent_ctl="$(readlink -f "/proc/$PPID/exe")"
  if [ -z "$expected_ctl" ] || [ "$parent_ctl" != "$expected_ctl" ]; then
    echo "SIGKILL wrapper parent is not the expected xinsightctl" >&2
    exit 102
  fi

  "$REAL_SYSTEMCTL" "$@"

  umask 077
  staging_marker="$marker.staging-$PPID"
  if [ -e "$staging_marker" ]; then
    echo "SIGKILL staging marker already exists" >&2
    exit 103
  fi
  printf '%s\n' "$PPID" > "$staging_marker"
  chmod 0444 "$staging_marker"
  mv -- "$staging_marker" "$marker"
  while true; do
    sleep 1
  done
fi

exec "$REAL_SYSTEMCTL" "$@"
