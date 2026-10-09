#!/usr/bin/env bash
# Cloud Run min-instances=1 for the review window, and the revert (M4-04, D-101).
#
# Usage: review-window.sh [--dry-run] on|off|status
#
# Idempotent: each command is echoed before it runs, and the script reads the
# current value before setting it, so it can be re-run safely.
#
# --dry-run: echo what would be done without executing gcloud commands.
#
# The production change is run by the operator or Paawan at the review window,
# not by the ticket worker (operating-rules § tiers: production deploy/config).
#
# Needs: gcloud authenticated with Owner (or equivalent) on the project.
# Env overrides: GCP_PROJECT, GCP_REGION.
set -euo pipefail

DRY_RUN=0
PROJECT="${GCP_PROJECT:-peak6-equinox-510816}"
REGION="${GCP_REGION:-us-central1}"
SERVICE="equinox"

# gc runs gcloud against the project, without interactive prompts.
gc() { gcloud --project="$PROJECT" --quiet "$@"; }

# run echoes a gc command to stderr, then runs it (or skips if DRY_RUN=1).
run() {
  {
    printf '    + gcloud --project=%q --quiet' "$PROJECT"
    printf ' %q' "${@:2}"
    printf '\n'
  } >&2
  if [[ "$DRY_RUN" == "1" ]]; then
    echo "    (dry-run: command not executed)" >&2
    return 0
  fi
  "$@"
}

get_min_instances() {
  gc run services describe "$SERVICE" --region="$REGION" \
    --format='value(spec.template.metadata.annotations."autoscaling.knative.dev/minScale")' 2>/dev/null || echo "0"
}

cmd_status() {
  local current
  current="$(get_min_instances)"
  echo "Current min-instances: ${current:-0}"
}

cmd_on() {
  local current
  if [[ "$DRY_RUN" == "1" ]]; then
    echo "Would set min-instances=1 for the review window"
    run gc run services update "$SERVICE" --region="$REGION" --min-instances=1
    echo "(dry-run: would result in min-instances=1)"
    return
  fi
  current="$(get_min_instances)"
  if [[ "${current:-0}" == "1" ]]; then
    echo "Already on: min-instances=1"
    return
  fi
  echo "Setting min-instances=1 for the review window"
  run gc run services update "$SERVICE" --region="$REGION" --min-instances=1
  echo "Done. Current min-instances: $(get_min_instances)"
}

cmd_off() {
  local current
  if [[ "$DRY_RUN" == "1" ]]; then
    echo "Would revert to min-instances=0"
    run gc run services update "$SERVICE" --region="$REGION" --min-instances=0
    echo "(dry-run: would result in min-instances=0)"
    return
  fi
  current="$(get_min_instances)"
  if [[ "${current:-0}" == "0" ]]; then
    echo "Already off: min-instances=0"
    return
  fi
  echo "Reverting to min-instances=0"
  run gc run services update "$SERVICE" --region="$REGION" --min-instances=0
  echo "Done. Current min-instances: $(get_min_instances)"
}

main() {
  # Parse --dry-run flag
  if [[ "${1:-}" == "--dry-run" ]]; then
    DRY_RUN=1
    shift
  fi

  command -v gcloud >/dev/null || { echo "gcloud not found" >&2; exit 1; }
  case "${1:-}" in
    on) cmd_on ;;
    off) cmd_off ;;
    status) cmd_status ;;
    *)
      echo "Usage: $(basename "$0") [--dry-run] on|off|status" >&2
      exit 1
      ;;
  esac
}

main "$@"
