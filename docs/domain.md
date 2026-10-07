<!-- OWNER: shared. Operator may update to match reality; every change is logged in docs/decisions.md. -->
# Domain glossary and business rules

## Terms

| Term | Meaning |
|---|---|
| **Venue** | A prediction-market exchange: here, `kalshi` or `polymarket` (international) |
| **Binary contract / instrument** | Pays $1 if its proposition resolves YES and $0 otherwise. This is our canonical unit (D-007). |
| **YES / NO** | The two sides of a binary. NO pays $1 when YES pays $0. |
| **Price** | Cost of one contract, in [0,1] dollars. It reads as the implied probability of YES. |
| **Bid / ask / touch** | The best price someone will pay (bid) or accept (ask). "Touch" is the best bid and ask. |
| **Mid** | (bid + ask) / 2. **Display only**; never used for execution. |
| **Book / depth** | All resting orders by price level. Depth is the size available at or better than a price. |
| **Book walk / VWAP** | Fill an order by consuming levels from best to worst. VWAP is the size-weighted average fill price. |
| **Taker fee** | A fee charged on the order that removes liquidity. Our simulated orders are always takers. |
| **Event** | A real-world occurrence grouping one or more contracts (e.g. "FOMC Oct 28, 2026 decision") |
| **Series** (Kalshi) | A recurring event family, e.g. `KXFED` |
| **Ladder / strike** | A set of binaries on one event at different thresholds ("above 4.00%", "above 4.25%", …), which is how Kalshi structures many markets |
| **Bucket** | A binary on a range or change ("decrease 25 bps"), which is how Polymarket often structures the same event |
| **negRisk** (Polymarket) | A multi-outcome event built as mutually exclusive binaries |
| **MVE** (Kalshi) | Auto-generated multivariate parlay markets. **Always excluded** (`mve_collection_ticker` non-empty). |
| **Terms** | Structured fields extracted from an instrument: subject, metric, comparator, threshold, unit, deadline + timezone, resolution source, edge rules, polarity |
| **Resolution source** | The authority that decides the outcome (e.g. "federalreserve.gov", "CoinDesk BTC price index") |
| **Fetch round** | One concurrent fetch of both venues, stored under one round ID; this is the unit of "snapshot" |
| **Pinned round** | The round used for the demo. While a round is pinned, nothing calls the venues to serve pages. |
| **Skew** | The absolute difference between two quotes' `fetched_at` (or venue timestamps) |
| **Staleness** | `now − fetched_at` on live data |
| **Basis risk** | Residual risk that two "equivalent" contracts resolve differently, e.g. because of cutoff gaps, source wording or early-close rules |
| **Fact** | An external value the matcher may use, from `config/facts.yaml`, with source and as-of date |
| **Golden set** | Human-confirmed labels on real pairs, used for precision and recall |
| **Settlement back-test** | Running the matcher on resolved contracts to see whether "equivalent" pairs settled the same |

## Relationship types (D-017)

These are listed strongest first. The LLM may only move a pair down this list (D-022).

| Type | Definition |
|---|---|
| `equivalent` | Buying YES on A and YES on B pays the same in every reachable world: same proposition, same resolution window (within tolerance), same or documented-identical source, same edge-case handling |
| `equivalent_given_facts` | `equivalent` once a stated fact is applied (e.g. "increase 25+ bps at the Oct meeting" ≡ "upper bound above 4.25% after Oct" given `fed_upper_bound=4.25%`) |
| `complement` | YES on A pays exactly when NO on B pays |
| `implies` | A=YES guarantees B=YES, but not the reverse (e.g. BTC > 120k ⇒ BTC > 110k on the same date and source) |
| `related` | Same event, but a different payout. This includes outcomes that depend on a prior event (e.g. Polymarket's December buckets depend on October's decision). **Never routed.** |
| `unrelated` | Everything else |

## Business rules

1. **Never** classify as `equivalent` when any hard veto fires: a different subject, a different comparator or threshold, deadlines more than 24 h apart, or unresolvable polarity (D-019).
2. Any `unknown` term on a field a veto checks means the pair cannot be `equivalent`. At best it is `related`, marked "uncertain" for LLM adjudication.
3. Only `equivalent`, `equivalent_given_facts` and `complement` pairs are routing candidates. A pair below `MIN_ROUTE_CONFIDENCE` is excluded.
4. **Buy at the ask, sell at the bid.** On Kalshi, the YES ask is 1 − the best NO bid, and the NO ask is 1 − the best YES bid (Kalshi publishes bid-only books per side; verify in M1-03).
5. **Complement route:** buying YES on A is equivalent to buying NO on B when A and B are complements, so the cost is B's NO ask.
6. **All-in cost per $1 payout** = VWAP + fee per contract + basis penalty per contract. The lowest cost wins, and ties go to the deeper book.
7. A quote is ineligible if it is stale (> `STALE_AFTER_SECONDS`), skewed against the comparison venue by more than `SKEW_MAX_SECONDS`, missing a book, or too shallow to fill any of the order. Skew above `SKEW_WARN_SECONDS` adds a warning.
8. USD and USDC are treated as 1:1 (D-012).
9. Times are stored in UTC, and venue rules are usually written in ET. Deadline comparisons use UTC instants.
10. Polymarket `outcomes`, `outcomePrices` and `clobTokenIds` are JSON strings inside JSON, so they must be decoded twice. Ignore resting orders at 0.001 and 0.999 that are outside a sanity band (configurable) when computing the touch. Never drop them from stored raw data.

## Fees (FR-16)

Fee formulas are data: each instrument carries a `FeeSchedule{ID, Params, Verified, Source}` filled by its adapter, and `internal/fees.Compute` evaluates it (D-058). Our simulated orders are always takers.

| Venue | Schedule | Formula (taker, per order) | Status |
|---|---|---|---|
| Kalshi | `kalshi_taker_v1`, params `multiplier` (= 0.07 × the series' `fee_multiplier`), optional `round_to` (dollars, default 0.01) | round up to the next `round_to` of `multiplier × C × P × (1 − P)`, with C contracts at price P. The fee is symmetric in P, so buys and sells at the same price pay the same. | **VERIFIED 2026-10-07** (M3-05) against the Kalshi API reference (https://docs.kalshi.com/openapi.yaml: `Series.fee_type` / `fee_multiplier` where `quadratic` = "the General Trading Fees Table" with `fee_multiplier` "applied to the fee calculations") and the public `GET /series/{ticker}` endpoint (`fee_type: quadratic`, `fee_multiplier: 1` for `KXBTCD`, `KXETHD`, `KXFED`). The `0.07 × C × P × (1 − P)` formula and round-up rule are consistent across independent reproductions of the official schedule. The official PDF (https://kalshi.com/docs/kalshi-fee-schedule.pdf, July 2026 update) remains behind a bot checkpoint (HTTP 429) and could not be read first-hand by automated workers; the formula and rounding increment (next cent, 0.01) were verified by a human against the PDF (D-073). |
| Polymarket | `polymarket_v1`, params `rate` (the market's `feeSchedule.rate`), optional `exponent` (`feeSchedule.exponent`, default 1) | `C × rate × (p × (1 − p))^exponent`, rounded to 5 decimal places (smallest fee 0.00001 USDC; smaller rounds to zero). Makers are never charged. Fee-free markets (`feesEnabled` false, or rate 0) cost nothing. | **VERIFIED 2026-10-07** (M3-05) against https://docs.polymarket.com/trading/fees (formula, precision, the per-category table: Crypto 0.07, Sports 0.05, Finance/Politics/Mentions/Tech 0.04, Economics/Culture/Weather/Other 0.05, Geopolitics 0) and https://docs.polymarket.com/market-data/market-details#trading-fees (per-market fields on Gamma `/markets`: `feesEnabled`, `feeSchedule.rate`, `feeSchedule.exponent`, `feeSchedule.takerOnly`, `feeSchedule.rebateRate`). The official worked example (100 shares at 50¢, rate 0.07 → $1.75) is covered by tests in `internal/fees`. |

Whenever a leg's schedule is not `Verified`, the router says "fees: approximate for <venue>" in the decision's warnings and explanation.
