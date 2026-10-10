# M4-79 Phase 2: deploy, re-match and pin a fresh live round (runbook)

**Issue:** #79 · **Decision:** D-114 (plan), D-111 (Phase 1 diagnosis) · **Status:** NOT RUN. Waiting for operator / Paawan sign-off on *when* to deploy: `integration/m4-final` now, or `main` after the final cumulative CI run (M4-07, D-102). The steps are the same either way; only the deployed sha changes.

## Why a deploy is needed

The live site (revision `equinox-00014-c9b`, pinned round 735) shows **Routable 2**. Round 735 was ingested before the M4-70 nominee fix (D-108, read at ingest) and matched before #72 (D-109, Fed decision formats) existed. Neither re-matching round 735 nor pinning another existing round helps: the fix has to be live when the round is **ingested**, and the matcher that runs on it has to include #72.

There is also a second trap. The `equinox-match` Cloud Run job pins its own image by digest (`sha256:c9c1ac6e…`, built 2026-10-08 16:48 UTC), which differs from the service's image (`sha256:b1e2e1ab…`, 2026-10-09 14:58 UTC). `make deploy` updates the service only. **If you skip step 3, the match job runs a matcher without #72 and the count stays low.**

## Measured expectation (local, not production)

These were measured on a fresh local scratch DB with public unauthenticated venue GETs only: `equinox ingest` with the default quote cap of 400 per venue, then `equinox match`, on 2026-10-10 at about 01:05 UTC. Universe files are unchanged.

| Head | Instruments | Pairs compared | equivalent | equivalent_given_facts | complement | Routable (by type, as on `/`) | Router-eligible (conf ≥ 0.90, quotes both sides) |
|---|---|---|---|---|---|---|---|
| `2d9c1c5` | 2,563 | 633,659 | 77 | 16 | 1 | **94** | **12** (11 egf + 1 complement, all Fed) |
| `8b5b0c8` (this MR's base) | same round, re-matched | 633,659 | 77 | 16 | 1 | **94** | **12** |

The 77 `equivalent` pairs are the 2028 nominee pairs (politics, confidence 0.72). The 17 Fed pairs are KXFED and KXFEDDECISION against Polymarket fed-rates. Both categories are represented. Live counts will differ a little with the venues' open markets at ingest time.

## Steps

Run these from a clean checkout of the agreed sha. Every command uses the already-authenticated host `gcloud`, and the admin password is only ever read inline, never printed.

```bash
export SVC=equinox REGION=us-central1
LIVE=https://peak6-equinox-287867547175.us-central1.run.app
ADMIN="admin:$(gcloud secrets versions access latest --secret=equinox-admin-password)"
```

### 0. Record the rollback point

```bash
PREV_REV=$(gcloud run services describe $SVC --region $REGION --format='value(status.traffic[0].revisionName)')
PREV_JOB_IMG=$(gcloud run jobs describe equinox-match --region $REGION \
  --format='value(spec.template.spec.template.spec.containers[0].image)')
PREV_PIN=735   # check on /admin ("round N pinned") before starting
echo "$PREV_REV $PREV_JOB_IMG $PREV_PIN"   # paste into docs/log.md
```

On 2026-10-10 these were `equinox-00014-c9b`, `…/equinox@sha256:c9c1ac6ebd7a64ff425d72558d289b0e79665df6f17e6b8dbb12f266b77c3648` and 735.

### 1. Deploy (about 8 minutes, Cloud Build)

```bash
git rev-parse HEAD          # must be the agreed sha
make deploy                 # deploys and moves traffic --to-latest (D-053)
curl -fsS $LIVE/healthz && curl -fsS $LIVE/version   # /version must show the agreed sha
```

A failed revision never takes traffic (D-065), so a failed deploy is safe to retry.

### 2. Confirm the new revision is serving

```bash
gcloud run services describe $SVC --region $REGION \
  --format='value(status.latestReadyRevisionName,status.traffic[0].revisionName,spec.template.spec.containers[0].image)'
```

### 3. Point the match job at the new image

```bash
NEW_IMG=$(gcloud run services describe $SVC --region $REGION --format='value(spec.template.spec.containers[0].image)')
gcloud run jobs update equinox-match --region $REGION --image "$NEW_IMG"
```

### 4. Ingest a fresh round with the new revision

The pin does not stop Cloud Scheduler, but a manual round gives a known id:

```bash
curl -fsS -X POST -u "$ADMIN" $LIVE/admin/ingest   # JSON: {"round_id":N,"status":"ok"|"partial",...}
ROUND=<round_id from the response>
```

A 409 response means a Scheduler round is running. Wait a minute and retry, or use that round's id once it finishes `ok` or `partial` (it started after step 2, so it was ingested by the new revision).

### 5. Match that round

```bash
gcloud run jobs execute equinox-match --region $REGION --wait --args=match,--round=$ROUND
gcloud logging read "resource.type=cloud_run_job AND resource.labels.job_name=equinox-match" \
  --limit 20 --freshness 15m --format='value(textPayload)' | grep -E "equivalent|relationships stored"
```

Expect roughly `equivalent ~77, equivalent_given_facts ~16, complement ~1`.

### 6. Pin it (D-065 match-then-pin)

```bash
gcloud run jobs execute equinox-match --region $REGION --wait --args=pin,$ROUND
```

`PinRound` unpins round 735 in the same transaction.

### 7. Verify on the live site

```bash
curl -fsS $LIVE/ | grep -oE 'Routable pairs \([0-9]+\)'   # must be >= 10
curl -fsS $LIVE/ | grep -o "round $ROUND"
scripts/smoke-live.sh $LIVE --pinned $ROUND
```

Open one Fed `equivalent_given_facts` pair and route it (`POST /route`) to confirm that a router-eligible pair routes.

### 8. Record

- `docs/status.md`: replace the "M4-03 pinned demo round" block (round id, finished at, counts) and the success-criterion-2 line.
- `README.md` § Live demo: update the pinned round id.
- `docs/log.md`: one entry with the sha, revision, job image, round id and measured counts.
- D-114: mark Phase 2 as run.

## Rollback

```bash
gcloud run services update-traffic $SVC --region $REGION --to-revisions $PREV_REV=100
gcloud run jobs update equinox-match --region $REGION --image "$PREV_JOB_IMG"
gcloud run jobs execute equinox-match --region $REGION --wait --args=pin,$PREV_PIN
```

`update-traffic --to-revisions` fixes traffic to one revision. The next `make deploy` moves it back with `--to-latest` (D-053). Rounds ingested meanwhile are harmless: pages follow the pin.
