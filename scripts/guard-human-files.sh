#!/usr/bin/env bash
# Fails an MR pipeline when a human-owned file changed without the
# `human-approved` label (brief hard constraint 10, operating-rules.md).
#
# Env: CI_MERGE_REQUEST_DIFF_BASE_SHA (required), CI_MERGE_REQUEST_LABELS (comma-separated).
set -euo pipefail

human_files=(
  docs/brief.md
  docs/requirements.md
  docs/threat-model.md
  docs/plan.md
  docs/operating-rules.md
  testdata/golden/labels.yaml
)
label="human-approved"

base="${CI_MERGE_REQUEST_DIFF_BASE_SHA:?CI_MERGE_REQUEST_DIFF_BASE_SHA must be set}"
labels="${CI_MERGE_REQUEST_LABELS:-}"

changed=$(git diff --no-renames --name-only "${base}...HEAD" -- "${human_files[@]}")

if [[ -z "$changed" ]]; then
  echo "guard-human-files: OK, no human-owned files changed."
  exit 0
fi

IFS=',' read -ra label_list <<< "$labels"
for l in "${label_list[@]}"; do
  l="${l#"${l%%[![:space:]]*}"}"
  l="${l%"${l##*[![:space:]]}"}"
  if [[ "$l" == "$label" ]]; then
    echo "guard-human-files: human-owned files changed, allowed by the '${label}' label:"
    printf '  %s\n' $changed
    exit 0
  fi
done

{
  echo "guard-human-files: FAIL: this MR changes human-owned files without the '${label}' label:"
  printf '  %s\n' $changed
  echo "Only the human may approve these changes. Add the '${label}' label to the MR and re-run the pipeline."
} >&2
exit 1
