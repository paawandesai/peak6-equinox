<!-- OWNER: shared. Operator may update to match reality; every change is logged in docs/decisions.md. -->
# Routing logic

This document describes how Equinox routes a simulated order across venues holding equivalent contracts (FR-12, FR-13, FR-14, FR-15, US-03, US-04, US-05). The router is venue-agnostic (D-028): all venue-specific logic lives in the adapters, and the router operates only on the canonical model.

## Objective

The router minimises the **all-in cost per $1 payout** for a buy order, or maximises the **net proceeds per contract** for a sell (D-026). Every routing decision is pure: the inputs are the order, the target instrument, its routable partners, the round's quotes, and the configuration; the output is a `Decision` with chosen legs, ranked alternatives, exclusions with reasons, and a written explanation.

## Inputs

| Input | Description |
|---|---|
| **Order** | Side (buy/sell), outcome (YES/NO), quantity (contracts or USD notional), order type (market/limit), limit price (if any), and time-in-force (IOC: immediate-or-cancel, or FOK: fill-or-kill). Follows FIX NewOrderSingle (D-024). |
| **Target** | The instrument the user names. It is one candidate, with no relationship to itself (no basis penalty). |
| **Partners** | Instruments related to the target by `equivalent`, `equivalent_given_facts`, or `complement` relationships (domain.md § Relationship types). Each carries a relationship confidence and a basis-risk score. |
| **Quotes** | The target's and each partner's book (YES bids and YES asks), each with a `fetched_at` timestamp, from one fetch round. |
| **Config** | Round ID, staleness and skew thresholds, minimum confidence, basis-penalty multiplier, and minimum book depth. |

A USD notional is converted to whole contracts at the target's touch (or the first candidate that has one), rounded down (internal/route/route.go:410–425).

## Eligibility

A candidate is eligible when it passes these checks (M3-01, FR-15, NFR-09):

### Quote-local checks (internal/route/eligibility.go:44–108)

1. **Staleness** (`IsEligible`): `cfg.Snapshot - q.FetchedAt > cfg.StaleAfter` excludes the quote as `stale` with detail "fetched N s ago". A missing or zero `FetchedAt` is ineligible. Default `STALE_AFTER_SECONDS=120` (2 minutes).

2. **Missing book**: A quote flagged `no_book` (the adapter fetch failed or the book was empty) or with both `YesAsks` and `YesBids` empty is excluded as `no_book`.

3. **Insufficient depth**: The side the order consumes (buy YES → YesAsks, sell YES → YesBids, buy NO → YesBids, sell NO → YesAsks) must have at least `cfg.MinBookDepth` contracts. `MIN_BOOK_DEPTH` applies to the consumed side only, not the total book depth. Below that threshold, the quote is excluded as `insufficient_depth` with detail "N contracts available on consumed side". Default `MIN_BOOK_DEPTH=1`.

### Cross-quote skew check (internal/route/eligibility.go:117–138)

4. **Skew** (`Skew`): When multiple candidates are eligible locally, the router finds the freshest quote as reference and checks each candidate against it. If `abs(q1.FetchedAt - q2.FetchedAt) > cfg.SkewMax`, the older quote is excluded as `skew` with detail "N s behind". Skew between `cfg.SkewWarn` and `cfg.SkewMax` includes the candidate but adds a warning "skew_warning: N s behind" to `Decision.Warnings`. Defaults: `SKEW_WARN_SECONDS=5`, `SKEW_MAX_SECONDS=30` (D-014).

### Relationship confidence filtering (internal/route/route.go:375–392)

5. **Low confidence**: A partner whose `relationship.Confidence < cfg.MinRouteConfidence` is excluded as `low_confidence` with detail "confidence X is below the Y floor". Default `MIN_ROUTE_CONFIDENCE=0.9` (domain.md rule 3).

6. **Not routable**: Only `equivalent`, `equivalent_given_facts`, and `complement` relationships are routable. `implies`, `related`, and `unrelated` are excluded as `not_routable`.

These checks run before any book is walked. A candidate excluded for eligibility never appears in `Alternatives`; it goes directly to `Exclusions` with its reason.

## Cost formula

The **all-in cost per contract** for a buy is (M3-02, domain.md rule 6):

```
all_in = VWAP + fee_per_contract + basis_penalty_per_contract
```

For a sell, the **net proceeds per contract** is:

```
net_proceeds = VWAP - fee_per_contract - basis_penalty_per_contract
```

### VWAP

The volume-weighted average price from the book walk (internal/route/walk.go:92–114). The router walks price levels from best to worst, consuming up to the order quantity or the book's depth, skipping levels the limit excludes. For a 100-contract buy that takes 50 contracts at 0.50 and 50 at 0.53, the VWAP is (50×0.50 + 50×0.53) / 100 = 0.515.

### Fee per contract

Computed from the instrument's `FeeSchedule` (a named formula plus parameters) per consumed level at that level's price, then summed and divided by the filled quantity (internal/route/route.go:505–515). For example, Polymarket charges `C × rate × p × (1−p)` per level (domain.md § Fees, D-058). The router never guesses; if a schedule is unknown or a parameter is missing, the candidate is excluded as `fee_unknown`.

### Basis penalty per contract

`relationship.BasisRisk.Score × cfg.BasisPenaltyMultiplier` (internal/route/route.go:470–481). The basis-risk score (from M2-04) is validated to [0,1] where 0 = no detectable risk and 1 = maximum risk (D-080). The score is multiplied by the penalty (default `BASIS_PENALTY_MULTIPLIER=0.01`), so a score of 0.5 adds $0.005 to the all-in cost, and the maximum penalty is $0.01 at score 1.0. The target has no relationship to itself, so its basis penalty is zero. Partners must have a `BasisRisk`; if nil, the candidate is excluded as `basis_risk_unknown`.

## Greedy sweep algorithm

When one venue cannot fill the order, the router sweeps across venues (M3-03, FR-13, internal/route/sweep.go:20–234).

### Algorithm (internal/route/sweep.go:120–234)

1. **Build ranked candidates**: Evaluate each eligible candidate by walking its book for the full order quantity (not just the first level), computing VWAP, fee, basis penalty, and all-in cost. A candidate that cannot fill any of the order (limit excluded all levels, or zero depth) is skipped.

2. **Rank by `better()`** (internal/route/route.go:538–565): full fills before partials, then quantized all-in cost (4 decimal places, M3-02), then larger fill, then depth within the limit, then lower basis score, then lexicographically first venue name, then instrument ID. This ranking prefers complete single-venue solutions.

3. **Full-fill shortcut** (internal/route/sweep.go:132–152): If the cheapest candidate (by quantized cost) can fill the entire order, return it immediately as the optimal single-venue solution. Otherwise, proceed to the greedy multi-venue sweep.

4. **Greedy multi-venue fill**: Re-rank candidates by quantized cost alone (no full-fill preference), then walk each candidate's book in cost order, filling as much as possible per venue until the remaining quantity is zero or all venues are exhausted. Each leg consumes only the quantity it can fill from its own book; the next leg fills the remainder from the next-cheapest venue. When costs are equal (same quantized value), the sweep re-rank breaks ties by depth within the limit, then lower basis score, then lexicographically first venue name, then instrument ID (internal/route/sweep.go:155–175). Unlike `better()`, this re-rank has no "full fill" or "larger fill" step—it fills greedily from the cheapest venues.

5. **Stop conditions**:
   - **IOC** (immediate-or-cancel): partial fills are permitted. The chosen legs are what the sweep filled, even if less than the full order.
   - **FOK** (fill-or-kill): partial fills are rejected. If the total available liquidity across all venues is less than the order quantity, no fill occurs (`Chosen` is empty, and every candidate is excluded with its specific reason, e.g., `insufficient_depth` with "FOK requires N contracts; M can fill K").

### Leg cost details (internal/route/route.go:489–501)

Each leg in `Chosen` records:
- `InstrumentID`, `Venue`, `Side`, `Outcome`: what was traded
- `Quantity`: contracts filled from this venue
- `VWAP`: the volume-weighted average price from this leg's book walk
- `FeePerContract`: the per-contract fee for this leg
- `BasisPenaltyPerContract`: the basis penalty for this leg (zero for the target)
- `AllInPerContract`: `VWAP ± FeePerContract ± BasisPenaltyPerContract` (buy adds, sell subtracts)
- `Route`: `"direct"` or `"complement"` (M3-04)

## Complement routing

When A and B are **complements**, buying YES on A is economically equivalent to buying NO on B (and vice versa), so the router compares the cost of both routes (M3-04, FR-14, US-04, domain.md rule 5).

### How it works (internal/route/route.go:150–176)

For each partner whose relationship type is `complement`, the router creates a candidate with the **opposite outcome**: if the order is buy YES, the complement candidate evaluates buy NO on the partner's instrument (consuming the partner's YES bids, re-priced as 1 − p per domain.md rule 4). The complement candidate is ranked by its all-in cost alongside direct equivalents, so a cheaper complement route can win.

The chosen leg's `Outcome` reflects the actual trade (e.g., NO on the complement instrument), and `Route` is set to `"complement"` (vs `"direct"` for non-complement). The explanation says: "Chose NO on [B] (complement of [A]) at venue X because the all-in cost (VWAP + fee + basis) was $Y per contract, vs $Z for YES on [A] at venue W."

## Tie-breaking

The router ranks candidates using `better(side, a, b)` (internal/route/route.go:538–565):

1. **Full fills before partial ones**: a candidate that can fill the entire order beats one that cannot, regardless of cost.
2. **Quantized all-in cost**: costs are quantized to 4 decimal places (`Round(4)`) for transitive comparison (M3-02, D-080). For buys, lower quantized cost wins; for sells, higher quantized net proceeds wins. Two candidates with the same `Round(4)` value are considered equal cost.
3. **Larger fill**: when costs are equal, more contracts filled is better.
4. **Deeper book**: when fills are equal, the candidate with more depth within the order's limit (total contracts at or better than the limit price) wins.
5. **Lower basis score**: when depth is equal, prefer lower resolution risk.
6. **Lexicographically first venue name**: deterministic tie-break (e.g., `kalshi` < `polymarket`).
7. **Instrument ID**: final tie-break when venues are equal.

For the greedy sweep (M3-03), candidates are first ranked by `better()` (which prefers full fills), then if the cheapest candidate cannot fill completely, they are re-ranked by quantized cost alone (ignoring the full-fill preference) to fill greedily from the cheapest venues (internal/route/sweep.go:155–175).

This ensures the router is deterministic: the same inputs always produce the same decision.

## How to read a routing trace

Every `Decision` includes:

| Field | Description |
|---|---|
| `RoundID` | The fetch round the quotes came from. |
| `TargetID` | The instrument the user named. |
| `Order` | The order as submitted. |
| `Chosen` | The legs the router chose, in cost order (empty if no venue could fill). |
| `Alternatives` | All viable single-venue options ranked by all-in cost, plus the multi-venue sweep if it differs from rank 1. The chosen option is marked with `Note: "chosen"`; partial fills under IOC are marked `"partial fill: N of M contracts"`. FOK excludes partials from Alternatives. |
| `Exclusions` | Candidates that could not be routed, with `Reason` (a code) and `Detail` (human-readable). |
| `Warnings` | Non-fatal issues: skew warnings, approximate fees, notional conversions, partial fills. |
| `Explanation` | A prose summary of the decision: what was chosen, why, cost breakdown per leg, ranked alternatives with their gap to the chosen cost, exclusions, and fee verification status. |
| `DecidedAt` | The UTC instant the decision was made. |
| `Inputs` | (UI-25, D-099) The input snapshot recorded at decision time: one entry per candidate considered (target + partners, eligible and excluded), with venue, instrument id, relationship type (empty for the target itself) and confidence, outcome side checked, best ask, ask depth (top 10 levels), quote times and round id. Empty for decisions stored before UI-25. |

### Exclusion reasons

| Reason | Detail example | Meaning |
|---|---|---|
| `stale` | "fetched 150 s ago" | Quote is older than `STALE_AFTER_SECONDS`. |
| `skew` | "30 s behind" | Quote is more than `SKEW_MAX_SECONDS` behind the freshest. |
| `no_book` | "no asks on the yes side" or "no book available" | Adapter fetch failed, or both sides empty, or the consumed side is empty. |
| `insufficient_depth` | "50 contracts available on consumed side" | Consumed side has less than `MIN_BOOK_DEPTH` contracts. |
| `low_confidence` | "confidence 0.85 is below the 0.9 floor" | Relationship confidence is below `MIN_ROUTE_CONFIDENCE`. |
| `not_routable` | "relationship type related is never routed" | Only `equivalent`, `equivalent_given_facts`, and `complement` are routable. |
| `limit_not_reachable` | "best asks is 0.6, outside the 0.55 limit" | Limit order's limit excludes all levels. |
| `fee_unknown` | `fee schedule "polymarket_v1": missing fee parameter: polymarket_v1 needs "rate"` | Fee cannot be computed (schedule unknown or parameter missing). |
| `basis_risk_unknown` | "partner relationship has no basis-risk score" | BasisRisk is nil on a partner. |
| `not_open` | "instrument is closed" | Instrument status is `closed` or `settled`. |
| `no_quantity` | "notional 10 buys less than one contract at the 0.02 touch" | Notional converts to zero or fractional contracts (no $ prefix). |

## Worked examples

### Example 1: Single-venue fill with cost breakdown

**Scenario**: Buy 100 YES on Kalshi `KXBTCD-26OCT0617-T120000`, with Polymarket partner `polymarket:1234567890` related as `equivalent` (confidence 0.97, basis score 0).

**Quotes** (both fetched at 2026-10-06 12:00:00 UTC):
- Kalshi: YES asks = [[0.52, 100]], YES bids = [[0.48, 200]]
- Polymarket: YES asks = [[0.50, 50], [0.53, 100]], YES bids = [[0.49, 80]]

**Fee schedules**:
- Kalshi: `kalshi_taker_v1`, multiplier 0.07, unverified (scenario() fixture)
- Polymarket: `polymarket_v1`, rate 0.07, verified

**Order**: Buy 100 YES, market, IOC

**Eligibility**:
- Both quotes are fresh (0 s old, threshold 120 s).
- No skew (same `fetched_at`).
- Both have sufficient depth (≥ 1 contract on consumed side, asks).
- Polymarket confidence 0.97 ≥ 0.9 floor.

**Book walks**:
- Kalshi (target): 100 contracts at 0.52 → VWAP 0.52
  - Fee: `ceil(0.07 × 100 × 0.52 × 0.48) = ceil(1.7472) = 1.75` → 0.0175/contract
  - Basis penalty: 0 (target has no relationship to itself)
  - All-in: 0.52 + 0.0175 + 0 = 0.5375

- Polymarket (partner): 50 at 0.50, 50 at 0.53 → VWAP (50×0.50 + 50×0.53)/100 = 0.515
  - Fee (per level): 50 × 0.07 × 0.50 × 0.50 = 0.875; 50 × 0.07 × 0.53 × 0.47 = 0.87185 → 1.74685 total → 0.0174685/contract
  - Basis penalty: 0 × 0.01 = 0 (basis score 0)
  - All-in: 0.515 + 0.0174685 + 0 = 0.5324685

**Decision**: Polymarket chosen (0.5324685 < 0.5375). Kalshi ranks #2, gap +0.0050315.

**Reproducibility**: `go test ./internal/route -run TestDocsExample1 -v` (fixture test asserting these exact numbers)

**Trace (from test output)**:
```
Simulated buy of 100 YES contracts on kalshi:KXBTCD-26OCT0617-T120000 as a market IOC order. Best route: polymarket (polymarket:1234567890) fills all 100 contracts at a VWAP of 0.5150 plus a fee of 0.0175 per contract, an all-in cost of 0.5325 per $1 payout Total cost: 53.2469. kalshi (kalshi:KXBTCD-26OCT0617-T120000) ranks #2 at 0.5375 per contract (VWAP 0.5200, fee 0.0175; 0.0050 worse). Fees are verified for polymarket but approximate for kalshi (schedule not yet verified against official documentation). Basis-risk penalty is applied as score × 0.01 per contract, and partners below a relationship confidence of 0.9 are excluded.
```

**Warnings**:
```
fees: approximate for kalshi (schedule not verified against official docs)
```

### Example 2: Two-venue greedy sweep

**Scenario**: Buy 500 YES on the same instruments (Kalshi target, Polymarket partner), both books too shallow to fill alone.

**Quotes** (same as Example 1):
- Kalshi: YES asks = [[0.52, 100]], YES bids = [[0.48, 200]]
- Polymarket: YES asks = [[0.50, 50], [0.53, 100]], YES bids = [[0.49, 80]]

**Fee schedules**: Kalshi unverified, Polymarket verified (scenario() fixture)

**Order**: Buy 500 YES, market, IOC

**Book walks** (per venue, for the full order):
- Polymarket: 50 at 0.50, 100 at 0.53 = 150 total available
  - VWAP: (50×0.50 + 100×0.53) / 150 = 0.52
  - Fee: 50 × 0.07 × 0.50 × 0.50 = 0.875; 100 × 0.07 × 0.53 × 0.47 = 1.7437 → 2.6187 total → 0.017458/contract
  - All-in: 0.52 + 0.017458 + 0 = 0.537458 (quantizes to 0.5375)
- Kalshi: 100 at 0.52 = 100 total available
  - VWAP: 0.52
  - Fee: ceil(0.07 × 100 × 0.52 × 0.48) = 1.75 → 0.0175/contract
  - All-in: 0.52 + 0.0175 + 0 = 0.5375

**Greedy sweep**: Both quantize to 0.5375 (tie), so the sweep re-rank breaks ties by depth within the limit.
1. Polymarket: 150 contracts at all-in 0.537458 (150 depth > 100 depth) → leg [1]
2. Kalshi: 100 contracts at all-in 0.5375 → leg [2]
3. Total filled: 250 of 500 (partial, IOC allows it)
4. Average all-in: (150×0.537458 + 100×0.5375) / 250 = 0.5375, total cost 134.3687

**Decision**: Multi-venue fill across 2 venues, 250 contracts, shortfall 250 (IOC).

**Reproducibility**: `go test ./internal/route -run TestDocsExample2 -v` (fixture test asserting these exact numbers)

**Trace (from test output)**:
```
Simulated buy of 500 YES contracts on kalshi:KXBTCD-26OCT0617-T120000 as a market IOC order. Best route: 250 of 500 fill across 2 venues (250 contracts at average all-in 0.5375, total cost 134.3687). [1] polymarket (polymarket:1234567890): 150 contracts at VWAP 0.5200 plus a fee of 0.0175 per contract, an all-in cost of 0.5375 per $1 payout [2] kalshi (kalshi:KXBTCD-26OCT0617-T120000): 100 contracts at VWAP 0.5200 plus a fee of 0.0175 per contract, an all-in cost of 0.5375 per $1 payout Shortfall: 250 contracts (IOC). polymarket (polymarket:1234567890) ranks #2 at 0.5375 per contract (VWAP 0.5200, fee 0.0175; 0.0000 worse). kalshi (kalshi:KXBTCD-26OCT0617-T120000) ranks #3 at 0.5375 per contract (VWAP 0.5200, fee 0.0175; 0.0000 worse). Fees are verified for polymarket but approximate for kalshi (schedule not yet verified against official documentation). Basis-risk penalty is applied as score × 0.01 per contract, and partners below a relationship confidence of 0.9 are excluded.
```

**Warnings**:
```
fees: approximate for kalshi (schedule not verified against official docs)
partial fill: chosen route fills 250 of 500 contracts (IOC)
```

### Example 3: Complement route wins over direct

**Scenario**: Buy 100 YES on Kalshi `A`, with Polymarket `B` as a complement (buying YES on A ≡ buying NO on B, confidence 0.95, basis score 0.3).

**Quotes** (both fetched at 2026-10-06 12:00:00 UTC):
- Kalshi A: YES asks = [[0.54, 100]], YES bids = [[0.46, 100]]
- Polymarket B (complement): YES asks = [[0.48, 100]], YES bids = [[0.52, 100]]

**Fee schedules**: Kalshi unverified, Polymarket verified (fixture)

**Order**: Buy 100 YES on A, market, IOC

**Book walks**:
- Direct (buy YES on A at Kalshi): 100 at 0.54 → VWAP 0.54
  - Fee: `ceil(0.07 × 100 × 0.54 × 0.46) = ceil(1.7388) = 1.74` → 0.0174/contract
  - Basis penalty: 0 (target)
  - All-in: 0.54 + 0.0174 + 0 = 0.5574

- Complement (buy NO on B at Polymarket): consume B's YES bids, re-priced as 1 − p
  - B's YES bid 0.52 → NO ask 1 − 0.52 = 0.48
  - 100 contracts at 0.48 → VWAP 0.48
  - Fee: 100 × 0.07 × 0.48 × 0.52 = 1.7472 (Polymarket rounds total to 5 dp → 1.74720) → 0.017472/contract
  - Basis penalty: 0.3 × 0.01 = 0.003 (basis score 0.3)
  - All-in: 0.48 + 0.017472 + 0.003 = 0.500472

**Decision**: Complement route chosen (0.500472 < 0.5574). Direct route ranks #2, gap +0.0569.

**Reproducibility**: `go test ./internal/route -run TestDocsExample3 -v` (fixture test asserting these exact numbers)

**Trace (from test output)**:
```
Simulated buy of 100 YES contracts on kalshi:A as a market IOC order. Chose NO on polymarket:B (complement of kalshi:A) at polymarket because the all-in cost (VWAP + fee + basis) was 0.5005 per contract, vs 0.5574 for YES on kalshi:A at kalshi fills all 100 contracts at a VWAP of 0.4800 plus a fee of 0.0175 and basis penalty of 0.0030 per contract, an all-in cost of 0.5005 per $1 payout Total cost: 50.0472. kalshi (kalshi:A) ranks #2 at 0.5574 per contract (VWAP 0.5400, fee 0.0174; 0.0569 worse). Fees are verified for polymarket but approximate for kalshi (schedule not yet verified against official documentation). Basis-risk penalty is applied as score × 0.01 per contract, and partners below a relationship confidence of 0.9 are excluded.
```

**Warnings**:
```
fees: approximate for kalshi (schedule not verified against official docs)
```

## Eligibility exclusion examples

### Stale quote

Quote `fetched_at` is 2026-10-06 10:00:00 UTC; snapshot (round finished) is 2026-10-06 12:05:00 UTC → age 7500 s > 120 s threshold → excluded as `stale` with detail "fetched 7500 s ago".

### Skew warning vs exclusion

Two quotes: A fetched at 12:00:00, B fetched at 12:00:07 → skew 7 s.
- Skew 7 s > `SKEW_WARN_SECONDS=5` → A (the older quote) gets warning "skew_warning: 7 s behind"
- Skew 7 s < `SKEW_MAX_SECONDS=30` → both are eligible, but A is warned

If A were fetched at 11:59:25 and B at 12:00:00 → skew 35 s > 30 s → A excluded as `skew` with detail "35 s behind".

### Shallow book

Quote has YES asks = [[0.50, 0.5]] (half a contract). Order wants to buy 10 YES contracts (consumes asks). Consumed side (asks) has 0.5 contracts < `MIN_BOOK_DEPTH=1` → excluded as `insufficient_depth` with detail "0.5 contracts available on consumed side".

## Sample routing trace JSON

This is the `Decision` structure. The JSON below is from Example 3's fixture test (`TestDocsExample3ComplementRoute`):

```json
{
  "round_id": 7,
  "target_id": "kalshi:A",
  "order": {
    "side": "buy",
    "outcome": "yes",
    "quantity": "100",
    "type": "market",
    "tif": "IOC"
  },
  "chosen": [
    {
      "instrument_id": "polymarket:B",
      "venue": "polymarket",
      "side": "buy",
      "outcome": "no",
      "quantity": "100",
      "vwap": "0.48",
      "fee_per_contract": "0.017472",
      "basis_penalty_per_contract": "0.003",
      "all_in_per_contract": "0.500472",
      "route": "complement"
    }
  ],
  "alternatives": [
    {
      "rank": 1,
      "legs": [
        {
          "instrument_id": "polymarket:B",
          "venue": "polymarket",
          "side": "buy",
          "outcome": "no",
          "quantity": "100",
          "vwap": "0.48",
          "fee_per_contract": "0.017472",
          "basis_penalty_per_contract": "0.003",
          "all_in_per_contract": "0.500472",
          "route": "complement"
        }
      ],
      "all_in_per_contract": "0.500472",
      "note": "chosen"
    },
    {
      "rank": 2,
      "legs": [
        {
          "instrument_id": "kalshi:A",
          "venue": "kalshi",
          "side": "buy",
          "outcome": "yes",
          "quantity": "100",
          "vwap": "0.54",
          "fee_per_contract": "0.0174",
          "basis_penalty_per_contract": "0",
          "all_in_per_contract": "0.5574",
          "route": "direct"
        }
      ],
      "all_in_per_contract": "0.5574"
    }
  ],
  "exclusions": [],
  "warnings": [
    "fees: approximate for kalshi (schedule not verified against official docs)"
  ],
  "explanation": "Simulated buy of 100 YES contracts on kalshi:A as a market IOC order. Chose NO on polymarket:B (complement of kalshi:A) at polymarket because the all-in cost (VWAP + fee + basis) was 0.5005 per contract, vs 0.5574 for YES on kalshi:A at kalshi fills all 100 contracts at a VWAP of 0.4800 plus a fee of 0.0175 and basis penalty of 0.0030 per contract, an all-in cost of 0.5005 per $1 payout Total cost: 50.0472. kalshi (kalshi:A) ranks #2 at 0.5574 per contract (VWAP 0.5400, fee 0.0174; 0.0569 worse). Fees are verified for polymarket but approximate for kalshi (schedule not yet verified against official documentation). Basis-risk penalty is applied as score × 0.01 per contract, and partners below a relationship confidence of 0.9 are excluded.",
  "decided_at": "2026-10-06T12:00:00Z"
}
```

**Reproducibility**: `go test ./internal/route -run TestDocsExample3 -v` generates this JSON via `json.MarshalIndent(dec, "", "  ")`.

**Field notes**:
- Decimals print without trailing zeros (e.g., `"0.48"` not `"0.4800"`).
- `chosen`: Empty array if no venue could fill (all excluded). Multi-venue sweeps have multiple legs in cost order.
- `alternatives[].note`: `"chosen"` marks rank 1; IOC partials are marked `"partial fill: N of M contracts"`; FOK excludes partials. Omitted (no key) when empty.
- `exclusions`: One entry per excluded candidate, with `reason` (code) and `detail` (human-readable).
- `warnings`: Skew warnings, approximate fees (when schedule unverified), notional conversions, partial fills.
- `explanation`: Human-readable summary of the whole decision, including cost breakdown, ranked alternatives, exclusions, and fee verification status.

## Configuration reference

| Variable | Default | Description |
|---|---|---|
| `STALE_AFTER_SECONDS` | 120 | Quote staleness limit (cfg.Eligibility.StaleAfter) |
| `SKEW_WARN_SECONDS` | 5 | Cross-venue skew warning threshold (cfg.Eligibility.SkewWarn) |
| `SKEW_MAX_SECONDS` | 30 | Cross-venue skew exclusion threshold (cfg.Eligibility.SkewMax) |
| `MIN_BOOK_DEPTH` | 1 | Minimum depth on consumed side in contracts (cfg.Eligibility.MinBookDepth) |
| `MIN_ROUTE_CONFIDENCE` | 0.9 | Minimum relationship confidence for routing (cfg.MinRouteConfidence) |
| `BASIS_PENALTY_MULTIPLIER` | 0.01 | Basis-risk penalty in dollars per contract per risk point (cfg.BasisPenaltyMultiplier) |

See [docs/environment.md](environment.md) for the full list and deployment notes.

## Code paths

| Concept | Implementation |
|---|---|
| Main routing entry point | `internal/route/route.go:98–371` `Route(order, target, partners, cfg, now)` |
| Eligibility checks | `internal/route/eligibility.go:44–108` `IsEligible(quote, side, outcome, cfg)` |
| Skew check | `internal/route/eligibility.go:117–138` `Skew(q1, q2, cfg)` |
| Book walking | `internal/route/walk.go:92–114` `walk(levels, side, qty, limit)` |
| Book side extraction | `internal/route/walk.go:55–76` `bookSide(quote, side, outcome)` (handles NO re-pricing) |
| Greedy sweep | `internal/route/sweep.go:20–234` `Sweep(order, candidates, cfg)` |
| Complement candidate creation | `internal/route/route.go:150–176` (opposite outcome, `RouteComplement`) |
| Cost calculation | `internal/route/route.go:520–526` `allIn(leg)` |
| Tie-breaking | `internal/route/route.go:538–565` `better(side, a, b)` |
| Explanation generation | `internal/route/route.go:600–764` `explainSweep(decision, legs, opts, sz, cfg)` |

## Related documents

- [docs/equivalence.md](equivalence.md): The matching logic that decides which instruments are `equivalent`, `equivalent_given_facts`, or `complement`.
- [docs/domain.md](domain.md): Domain glossary, relationship types, business rules, and fee schedules.
- [docs/architecture.md](architecture.md): The router's place in the system, its venue-agnostic constraint (D-028), and the cost terms (D-026, D-027).
- [docs/decisions.md](decisions.md): Decision history, including D-026 (all-in cost), D-027 (greedy sweep), D-028 (venue-agnostic), D-058 (fees as data), D-060 (crypto deadline veto).

## Decision IDs

- **D-026**: Routing minimises all-in cost per $1 payout (VWAP + fees + basis penalty), after eligibility filters.
- **D-027**: Greedy level-by-level sweep across venues by all-in marginal cost when one book can't fill.
- **D-028**: `internal/route` imports only `canonical`, `fees`, decimal and stdlib (enforced by test).
- **D-058**: Fees are computed from a FeeSchedule (formula as data); unverifiable schedules exclude as `fee_unknown`.
- **D-060**: Crypto deadline veto is stricter than the generic 24 h tolerance (any deadline difference is a veto).
