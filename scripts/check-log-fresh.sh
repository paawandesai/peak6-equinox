#!/usr/bin/env bash
# Fails if the last commit touching docs/log.md is older than 24 hours
# (operating-rules.md: log.md must change at least daily). Needs full history.
set -euo pipefail

log_file="docs/log.md"
max_age="${LOG_MAX_AGE_SECONDS:-86400}"

last=$(git log -1 --format=%ct -- "$log_file")
if [[ -z "$last" ]]; then
  echo "log-freshness: FAIL: no commit touches ${log_file} (is the clone shallow? set GIT_DEPTH: 0)" >&2
  exit 1
fi

now=$(date +%s)
age=$(( now - last ))
echo "log-freshness: ${log_file} last changed ${age}s ago (limit ${max_age}s)."
if (( age > max_age )); then
  echo "log-freshness: FAIL: ${log_file} has not changed in over ${max_age}s. The operator must append to it." >&2
  exit 1
fi
