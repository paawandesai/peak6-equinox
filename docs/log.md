<!-- OWNER: operator. -->
# Operator log (append-only)

Add one line per action, newest at the bottom: `YYYY-MM-DD HH:MM UTC · actor · action · refs`. Never edit or delete earlier lines. CI fails if this file hasn't changed in 24 hours.

2026-10-06 18:30 UTC · kickoff · Kickoff pack created. · docs/*
2026-10-06 18:13 UTC · operator · Committed and pushed kickoff pack to main (chore: kickoff pack). · 6f77532
2026-10-06 18:13 UTC · operator · Confirmed gcloud Owner on peak6-equinox-510816 (Q-1 = a). · status.md
2026-10-06 18:14 UTC · operator · Created GitLab labels M1 + human-approved; main already protected; spawned AO session peak6-equinox-2 for M1-01 (issue #1). · issue#1
2026-10-06 18:40 UTC · M1-01 worker · Tier 1: pinned tools in the Makefile at their latest releases: golangci-lint v2.14.0, goose v3.28.0, govulncheck v1.8.0, gitleaks v8.30.1 (CI image zricethezav/gitleaks:v8.30.1). · Makefile, .gitlab-ci.yml
2026-10-06 18:40 UTC · M1-01 worker · Tier 2: pinned Go 1.27.1 (D-050); Tier 1: planned-subcommand stubs (D-051). · decisions.md
2026-10-06 19:31 UTC · Cursor reviewer · Approved M1-01 MR !1 (READY TO MERGE) on 95bb806. · MR!1
2026-10-06 19:32 UTC · operator · Merged M1-01 MR !1 to main (CI green + Cursor approved). · 69149a6 closes #1
2026-10-06 19:33 UTC · operator · Closed issue #1; creating remaining M1 GitLab issues; starting M1-02 and M1-03. · tickets/M1.md
2026-10-06 19:33 UTC · operator · Created GitLab issues #2–#12 (M1-02…M1-12) with label M1; daily log-freshness pipeline schedule id 18 on main (0 6 * * * UTC). · issues#2-12 schedule#18
2026-10-06 19:55 UTC · M1-02 worker · Ran scripts/gcp-bootstrap.sh (APIs, AR repo equinox, Cloud SQL equinox-pg db-f1-micro, SA equinox-run, secrets) and make deploy / make deploy-staging; live URL https://equinox-o5hdimw5rq-uc.a.run.app. Tier 2: /health alias (D-052); Tier 1: deploy details (D-053). · decisions.md, README.md
2026-10-06 20:50 UTC · operator · Merged M1-02 MR !4 to main (CI green). · 2e8aee2 closes #2
2026-10-06 21:01 UTC · operator · CI runner maintenance ~15:30 CT / 20:30 UTC Oct 6 2026. Pipeline 39142 (MR !5, sha 8d7ce6d7) canceled after long queue during window; retried then canceled again because superseded by head pipeline 39169 on rebased sha f9927917 (do not count maint-window failures/cancels toward ticket 3-attempt limits). Head !5 pipeline 39169 left pending on dedicated runner; main pipeline 39159 running. · pipelines#39142,#39169,#39159 MR!5
2026-10-06 21:01 UTC · operator · Runner policy: untagged jobs → dedicated project runner; jobs tagged `heavy` (E2E/red-team/browser) → shared runner one-at-a-time. If any job pending >10m, escalate to human — do not retry-loop. · docs/log.md
2026-10-06 22:30 UTC · M1-08 worker · Fees + router v0: Polymarket fee schedule verified from official docs; Kalshi kept approximate (PDF behind bot checkpoint; API series multipliers verified). Tier 2: domain.md § Fees updated (D-058). · internal/fees, internal/route, cmd/equinox/route.go
2026-10-06 22:35 UTC · M1-11 worker · internal/llm safety harness (Verifier, BuildPrompt, strict Decode, downgrade-only Guard/Apply, CacheKey, mocks), 32-case injection suite, AB-01/11/13 tests, llmlive live eval, docs/ai-usage.md created. Tier 1: D-059. · decisions.md, docs/ai-usage.md
2026-10-06 23:05 UTC · M1-06 worker · Collector, `equinox ingest|seed|fixture-server`, `/admin` + `POST /admin/ingest` with basic auth, lockout and origin check; AB-05/12/14 tests. Tier 1: quote cap 400/venue by soonest close with category round-robin, one round at a time, Dockerfile copies config/ (D-060). · decisions.md
