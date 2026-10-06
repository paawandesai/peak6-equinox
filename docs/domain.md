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

## Fees (FR-16): to verify

| Venue | Model as currently understood | Status |
|---|---|---|
| Kalshi | Taker fee per order = round up to the cent of `0.07 × contracts × P × (1 − P)` for general markets. Some series use different multipliers. | **UNVERIFIED.** Confirm against Kalshi's official fee schedule in M1-07, and record the URL and date here. |
| Polymarket | Most markets are fee-free for takers; some markets charge a taker fee published per market | **UNVERIFIED.** Confirm in M1-07 where the per-market fee rate is exposed, and record the URL and date here. |

Until verified, the router shows "fees: approximate" in every explanation.
