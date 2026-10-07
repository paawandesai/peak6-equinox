#!/usr/bin/env bash
# Fails if statement coverage across ./internal/... is below the threshold
# (default 80.0%). Passes with a notice when there are no statements yet.
set -euo pipefail

threshold="${COVERAGE_THRESHOLD:-80.0}"
profile="${COVERAGE_PROFILE:-coverage.out}"

go test -timeout 20m -coverpkg=./internal/... -coverprofile="$profile" ./...

# With -coverpkg the same block can appear once per test binary; a block is
# covered if any binary hit it.
read -r total covered < <(awk '
  NR > 1 && NF == 3 { stmts[$1] = $2; if ($3 > 0) hit[$1] = 1 }
  END {
    t = 0; c = 0
    for (k in stmts) { t += stmts[k]; if (k in hit) c += stmts[k] }
    print t, c
  }' "$profile")

if [[ "$total" -eq 0 ]]; then
  echo "coverage: NOTICE: 0 statements under ./internal/...; nothing to measure, passing."
  exit 0
fi

pct=$(awk -v c="$covered" -v t="$total" 'BEGIN { printf "%.1f", 100 * c / t }')
echo "coverage: ${pct}% of ${total} statements under ./internal/... (threshold ${threshold}%)"
if awk -v p="$pct" -v th="$threshold" 'BEGIN { exit !(p + 0 < th + 0) }'; then
  echo "coverage: FAIL: ${pct}% is below ${threshold}%" >&2
  exit 1
fi
