# M4-03: Pin the demo round procedure

**Status:** BLOCKED - waiting for MR !58 (issue #39) to be deployed to production

**Issue:** #61  
**Depends on:** Issue #39 (MR !58) merged and deployed to live service

## Precondition verification

MR !58 (issue #39: matcher for 6 corrected golden pairs + 3 new series fixtures) was merged to main on **2026-10-08 21:36:06 UTC** (commit 32360a88873a6670b2dae68aea7235da8bdd312e).

The current live Cloud Run revision **equinox-00012-9v7** was created on **2026-10-06 19:45:28 UTC**, which is **before the merge**.

Therefore, the precondition is **NOT met**. Issue #39 is merged but not yet deployed.

## Exact procedure to run when deployed

Once a Cloud Run revision containing commit 32360a8 or later is deployed and serving traffic, execute the following steps:

### Step 1: Run match on the latest successful round

Use the `equinox-match` Cloud Run job (D-065; 2 GiB memory, 2 vCPU) to match the latest successful round:

```bash
# Get the latest successful round ID from the live service
LATEST_ROUND=$(curl -fsS https://peak6-equinox-287867547175.us-central1.run.app/admin \
  -u admin:$(gcloud secrets versions access latest --secret=equinox-admin-password) \
  | grep -oP 'round_id":\K\d+' | head -1)

echo "Latest round: ${LATEST_ROUND}"

# Execute the match job on the latest round
gcloud run jobs execute equinox-match \
  --region us-central1 \
  --wait \
  --format='value(status.logUri)'
```

The match job runs `equinox match` against the production database. It will process all unmatched cross-venue pairs for the latest round and store relationships (`equivalent`, `equivalent_given_facts`, `complement`, `related`, `not_a_match`).

**Expected output:** The job should complete successfully and log the number of relationships stored per type.

### Step 2: Pin the matched round

Once matching completes, pin that round so `/` serves it consistently across Scheduler ticks:

```bash
# Pin the matched round via the same Cloud Run job
gcloud run jobs execute equinox-match \
  --region us-central1 \
  --wait \
  --args="pin,${LATEST_ROUND}" \
  --format='value(status.logUri)'
```

Alternatively, if the `equinox` CLI is available locally with production database access:

```bash
equinox pin ${LATEST_ROUND}
```

### Step 3: Verify the pinned round

Verify the pinned round is serving and contains the required demo pairs:

```bash
LIVE_URL="https://peak6-equinox-287867547175.us-central1.run.app"

# Check that the home page shows "pinned" in the context bar
curl -fsS "${LIVE_URL}/" | grep -o "pinned"

# Get the round metadata
curl -fsS "${LIVE_URL}/admin" \
  -u admin:$(gcloud secrets versions access latest --secret=equinox-admin-password) \
  | jq -r '.rounds[] | select(.pinned == true) | 
    "Round ID: \(.round_id)\nFinished at: \(.finished_at)\nStatus: \(.status)"'

# Check relationship counts
curl -fsS "${LIVE_URL}/admin" \
  -u admin:$(gcloud secrets versions access latest --secret=equinox-admin-password) \
  | jq -r '.rounds[] | select(.pinned == true) | 
    .stats.relationships | to_entries[] | "\(.key): \(.value)"'
```

### Step 4: Verify acceptance criteria

1. **At least one `equivalent_given_facts`** (the Fed demo pair): check the home page shows matches grouped by type
2. **At least one `complement`**: verify in the relationship counts from step 3
3. **Scheduler keeps collecting** while `/` stays pinned: wait 10+ minutes, check `/admin` shows new rounds but `/` stays on the pinned round
4. **No venue calls for pinned pages**: run the security test:
   ```bash
   go test ./tests/security/ -run NFR04
   ```

### Step 5: Record results in docs

Once verified, record in `docs/status.md` (Current milestone section) and `docs/log.md`:

- Round ID (from step 2)
- `finished_at` timestamp (from step 3)
- Counts per relationship type (from step 3):
  - `equivalent`
  - `equivalent_given_facts`
  - `complement`
  - `related`
  - `not_a_match`

## Example documentation

```markdown
### M4-03: Pinned demo round

- **Round ID:** 42
- **Finished at:** 2026-10-08T22:15:30Z
- **Relationship counts:**
  - equivalent: 5
  - equivalent_given_facts: 12
  - complement: 8
  - related: 234
  - not_a_match: 1,542
```

## Cleanup command (if pin needs to change)

If a later deploy with a schema change requires re-pinning:

```bash
# Clear the current pin
gcloud run jobs execute equinox-match \
  --region us-central1 \
  --wait \
  --args="pin,--clear"

# Then repeat steps 1-5 above
```

## References

- Ticket: docs/tickets/M4.md § M4-03
- D-065: Match-then-pin rule and Cloud Run job setup
- D-063: `CurrentRound` serves pinned round, else latest ok/partial
