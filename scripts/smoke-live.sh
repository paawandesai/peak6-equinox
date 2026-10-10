#!/usr/bin/env bash
# Live smoke test for Equinox (M4-05). Exercises the demo path over HTTP.
#
# Usage: scripts/smoke-live.sh [base-url] [--pinned round-id]
#
# Checks health, home page (with round id and pinned flag when a round is
# pinned), near-misses, log, eval, the first pair linked from /, POST /route,
# CSP headers, admin without auth (401), and an invalid pair (404 not 500).
# Computes p95 latency for GET / and POST /route over 20 requests (NFR-01).
#
# Each check prints PASS/FAIL with the URL and the elapsed ms. The script
# exits non-zero on any FAIL. With --pinned <round-id>, asserts the served
# round id equals the pinned one.
set -euo pipefail

LIVE_URL="https://peak6-equinox-287867547175.us-central1.run.app"
BASE_URL="$LIVE_URL"
PINNED_ROUND=""
TIMEOUT=20
FAIL_COUNT=0

# Parse arguments: [base-url] [--pinned round-id]
while [[ $# -gt 0 ]]; do
	case "$1" in
		--pinned)
			if [[ -z "${2:-}" ]]; then
				echo "Error: --pinned requires a round-id argument" >&2
				echo "Usage: $(basename "$0") [--dry-run] [base-url] [--pinned round-id]" >&2
				exit 2
			fi
			PINNED_ROUND="$2"
			shift 2
			;;
		*)
			BASE_URL="$1"
			shift
			;;
	esac
done

# check <name> <url> [curl-args...]
# Measures elapsed time, expects 200, and prints PASS/FAIL.
check() {
	local name="$1" url="$2"
	shift 2
	local start end elapsed status body
	start=$(date +%s%3N)
	set +e
	body=$(curl -fsS --max-time "$TIMEOUT" -w '\n%{http_code}' "$@" "$url" 2>&1)
	local ret=$?
	set -e
	end=$(date +%s%3N)
	elapsed=$((end - start))
	if [[ $ret -ne 0 ]]; then
		echo "FAIL: $name ($url) — curl failed (exit $ret) — ${elapsed}ms" >&2
		((FAIL_COUNT++)) || true
		return 1
	fi
	status=$(echo "$body" | tail -n1)
	body=$(echo "$body" | head -n -1)
	if [[ "$status" != "200" ]]; then
		echo "FAIL: $name ($url) — HTTP $status — ${elapsed}ms" >&2
		((FAIL_COUNT++)) || true
		return 1
	fi
	echo "PASS: $name ($url) — ${elapsed}ms" >&2
	echo "$body"
}

# check_status <name> <url> <expected-status> [curl-args...]
# Like check, but expects a specific status code (not necessarily 200).
check_status() {
	local name="$1" url="$2" expected="$3"
	shift 3
	local start end elapsed status
	start=$(date +%s%3N)
	set +e
	status=$(curl -o /dev/null -w '%{http_code}' -sS --max-time "$TIMEOUT" "$@" "$url" 2>&1)
	local ret=$?
	set -e
	end=$(date +%s%3N)
	elapsed=$((end - start))
	if [[ $ret -ne 0 && "$expected" != "000" ]]; then
		echo "FAIL: $name ($url) — curl failed (exit $ret) — ${elapsed}ms" >&2
		((FAIL_COUNT++)) || true
		return 1
	fi
	if [[ "$status" != "$expected" ]]; then
		echo "FAIL: $name ($url) — HTTP $status, want $expected — ${elapsed}ms" >&2
		((FAIL_COUNT++)) || true
		return 1
	fi
	echo "PASS: $name ($url) — ${elapsed}ms" >&2
}

# p95 <name> <url> [curl-args...]
# Measures p95 latency over 20 requests (information, not a gate).
p95() {
	local name="$1" url="$2"
	shift 2
	local -a times
	echo "# p95 for $name ($url) over 20 requests..."
	for _ in {1..20}; do
		local start end elapsed
		start=$(date +%s%3N)
		curl -fsS --max-time "$TIMEOUT" -o /dev/null "$@" "$url" 2>/dev/null || true
		end=$(date +%s%3N)
		elapsed=$((end - start))
		times+=("$elapsed")
	done
	# Sort times and take the 95th percentile (index 18 in 0-indexed array of 20).
	local -a sorted
	mapfile -t sorted < <(printf '%s\n' "${times[@]}" | sort -n)
	echo "p95 for $name: ${sorted[18]}ms (NFR-01: 500ms pages, 300ms route)"
}

echo "==> Smoke test: $BASE_URL"
echo

# 1. /health is 200
check "health" "$BASE_URL/health" >/dev/null

# 2. / is 200 and contains the context bar with a round id (and "pinned" when pinned)
home_file=$(mktemp)
check "home" "$BASE_URL/" > "$home_file"
if ! grep -q 'class="context-bar"' "$home_file"; then
	echo "FAIL: home ($BASE_URL/) — no context-bar" >&2
	((FAIL_COUNT++)) || true
fi
# Extract round id from context bar (format: "round 123" or "round 123, pinned")
round_id=$(grep -oP 'round \K[0-9]+' "$home_file" | head -n1 || true)
if [[ -z "$round_id" ]]; then
	echo "FAIL: home ($BASE_URL/) — no round id in context-bar" >&2
	((FAIL_COUNT++)) || true
fi
if [[ -n "$round_id" ]]; then
	echo "# Round id: $round_id"
	if [[ -n "$PINNED_ROUND" ]]; then
		if [[ "$round_id" != "$PINNED_ROUND" ]]; then
			echo "FAIL: home ($BASE_URL/) — round $round_id, want pinned round $PINNED_ROUND" >&2
			((FAIL_COUNT++)) || true
		else
			echo "PASS: pinned round $PINNED_ROUND matches served round" >&2
		fi
		if ! grep -q 'pinned' "$home_file"; then
			echo "FAIL: home ($BASE_URL/) — pinned round $PINNED_ROUND but no 'pinned' in context-bar" >&2
			((FAIL_COUNT++)) || true
		fi
	fi
fi

# 3. /near-misses, /log, /eval are 200
check "near-misses" "$BASE_URL/near-misses" >/dev/null
check "log" "$BASE_URL/log" >/dev/null
check "eval" "$BASE_URL/eval" >/dev/null

# 4. First /pairs/<id> linked from / is 200 and contains the diff table
pair_id=$(grep -oP 'href="/pairs/\K[0-9]+' "$home_file" | head -n1 || true)
if [[ -z "$pair_id" ]]; then
	echo "FAIL: home ($BASE_URL/) — no /pairs/<id> link" >&2
	((FAIL_COUNT++)) || true
fi
if [[ -n "$pair_id" ]]; then
	echo "# First pair: $pair_id"
	pair_file=$(mktemp)
	check "pair" "$BASE_URL/pairs/$pair_id" > "$pair_file"
	if ! grep -q '<table class="data terms">' "$pair_file"; then
		echo "FAIL: pair ($BASE_URL/pairs/$pair_id) — no terms table" >&2
		((FAIL_COUNT++)) || true
	fi
fi

# 5. POST /route for that pair (buy 100 YES, market, IOC) is 200 and contains
#    either a chosen venue or "not routed" with a reason
if [[ -n "$pair_id" ]]; then
	# Extract the target instrument from the pair page (first ID in dt/dd/code structure).
	target=$(grep -oP '<dt>ID</dt><dd><code>\K[^<]+' "$pair_file" | head -n1 || true)
	if [[ -z "$target" ]]; then
		echo "FAIL: pair ($BASE_URL/pairs/$pair_id) — no instrument id" >&2
		((FAIL_COUNT++)) || true
	fi
	if [[ -n "$target" ]]; then
		echo "# Target instrument: $target"
		route_file=$(mktemp)
		check "route" "$BASE_URL/route" \
			-d "pair=$pair_id" -d "target=$target" \
			-d "side=buy" -d "outcome=yes" -d "quantity=100" \
			-d "type=market" -d "tif=IOC" > "$route_file"
		if grep -q '<h3>Chosen legs</h3>' "$route_file"; then
			if ! grep -qP '<span class="venue">[a-z]+</span>' "$route_file"; then
				echo "FAIL: route ($BASE_URL/route) — chosen legs but no venue" >&2
				((FAIL_COUNT++)) || true
			else
				echo "PASS: route ($BASE_URL/route) — chosen venue found" >&2
			fi
		elif grep -qi 'not routed' "$route_file"; then
			if ! grep -q '<p class="explanation">' "$route_file"; then
				echo "FAIL: route ($BASE_URL/route) — not routed but no explanation" >&2
				((FAIL_COUNT++)) || true
			else
				echo "PASS: route ($BASE_URL/route) — not routed with explanation" >&2
			fi
		else
			echo "FAIL: route ($BASE_URL/route) — no chosen venue or 'not routed' message" >&2
			((FAIL_COUNT++)) || true
		fi
		rm -f "$route_file"
	fi
	rm -f "$pair_file"
fi
rm -f "$home_file"

# 6. Every public page sends the CSP header
for page in "/" "/near-misses" "/log" "/eval"; do
	start=$(date +%s%3N)
	csp=$(curl -fsS --max-time "$TIMEOUT" -I "$BASE_URL$page" 2>/dev/null | grep -i '^Content-Security-Policy:' || true)
	end=$(date +%s%3N)
	elapsed=$((end - start))
	if [[ -z "$csp" ]]; then
		echo "FAIL: CSP header ($BASE_URL$page) — no Content-Security-Policy header — ${elapsed}ms" >&2
		((FAIL_COUNT++)) || true
	else
		echo "PASS: CSP header ($BASE_URL$page) — ${elapsed}ms" >&2
	fi
done
# Also check the pair page if we have one
if [[ -n "$pair_id" ]]; then
	start=$(date +%s%3N)
	csp=$(curl -fsS --max-time "$TIMEOUT" -I "$BASE_URL/pairs/$pair_id" 2>/dev/null | grep -i '^Content-Security-Policy:' || true)
	end=$(date +%s%3N)
	elapsed=$((end - start))
	if [[ -z "$csp" ]]; then
		echo "FAIL: CSP header ($BASE_URL/pairs/$pair_id) — no Content-Security-Policy header — ${elapsed}ms" >&2
		((FAIL_COUNT++)) || true
	else
		echo "PASS: CSP header ($BASE_URL/pairs/$pair_id) — ${elapsed}ms" >&2
	fi
fi

# 7. /admin without auth is 401
check_status "admin without auth" "$BASE_URL/admin" "401"

# 8. An invalid /pairs/999999 is 404 and never 500
check_status "invalid pair" "$BASE_URL/pairs/999999" "404"

# 9. p95 for GET / and POST /route over 20 requests (NFR-01)
echo
p95 "GET /" "$BASE_URL/"
if [[ -n "$pair_id" && -n "$target" ]]; then
	p95 "POST /route" "$BASE_URL/route" \
		-d "pair=$pair_id" -d "target=$target" \
		-d "side=buy" -d "outcome=yes" -d "quantity=100" \
		-d "type=market" -d "tif=IOC"
fi

echo
if [[ $FAIL_COUNT -eq 0 ]]; then
	echo "==> All checks passed"
	exit 0
else
	echo "==> $FAIL_COUNT check(s) failed"
	exit 1
fi
