<!-- OWNER: operator. Methodology doc for the equivalence engine (plan.md M2 exit deliverable; brief constraint 5). Update when a decision changes a rule; cite the D- entry. -->
# Equivalence logic

This document explains how Equinox decides that two binary contracts on different venues are the same bet, and how sure it is. It is the written logic that [brief.md](brief.md) asks for. The code it describes is `internal/normalize` (extraction), `internal/match` (classification) and `internal/facts` (external facts). The rules were fixed by human decisions D-017 to D-022 and refined by operator decisions D-060, D-061, D-066, D-067 and D-068 in [decisions.md](decisions.md); this page cites them rather than restating their rationale.

Sections: [definition](#1-the-definition) · [methodology](#2-methodology-block--veto--classify--facts--basis) · [veto catalogue](#3-veto-catalogue) · [facts file](#4-the-facts-file-contract) · [basis risk](#5-basis-risk-components) · [confidence](#6-confidence) · [reading eval and back-test output](#7-how-to-read-eval-and-back-test-output) · [current results](#8-current-results) · [known failure modes](#9-known-failure-modes) · [Fed walkthrough](#10-the-fed-demo-pair-walkthrough).

## 1. The definition

**Two contracts are `equivalent` if and only if buying YES on both pays out identically in every reachable world state** (D-017). That is a payout definition, not a text-similarity one: it is testable against settlements, it is conservative, and it is exactly what makes two contracts interchangeable for routing.

"Identically in every reachable world" breaks down into four things that must all hold, and they are the four things the matcher checks:

| What must match | Why it matters | Where it is checked |
|---|---|---|
| **The proposition**: subject, metric, comparator, threshold(s), unit, polarity | "BTC above 76,599.99" and "BTC above 76,600" pay differently at exactly 76,600 | hard vetoes (§3) |
| **The resolution window**: the deadline instant | A price read at 5 PM is a different random variable from a price read at noon | deadline veto (§3) |
| **The resolution source**: the authority that decides | CF Benchmarks' index and Binance's last trade can disagree at the same instant | source veto (§3) |
| **Edge-case handling**: early close, ties, cancellations | Two contracts can agree on the headline and diverge on a void | not vetoed yet; scored as basis risk (§5) |

### Relationship types

The six types are defined in [domain.md § Relationship types](domain.md#relationship-types-d-017) and encoded as `canonical.RelationshipType`. Strongest first, which is also the only direction the LLM verifier may move a pair (D-022):

| Type | Payout meaning | Routable? | How the matcher reaches it |
|---|---|---|---|
| `equivalent` | YES on A pays exactly when YES on B pays | yes | no veto fires |
| `equivalent_given_facts` | `equivalent` once a stated external fact is applied | yes | the Fed level-vs-change rule closes the gap (§4, §10) |
| `complement` | YES on A pays exactly when **NO** on B pays | yes (buy B's NO) | set algebra: same interval with opposite polarity, or two intervals that partition the line |
| `implies` | YES on A guarantees YES on B, not the reverse | no | set algebra: strict containment, in either direction |
| `related` | Same event, different payout; includes outcomes conditional on a prior event | **never** | any veto the derivations cannot explain away |
| `unrelated` | Everything else | no | fails blocking |

`implies` is recorded for both directions because the (A, B) order is the caller's venue order, not a semantic one. The explanation always names the direction: "YES on X forces YES on Y, not the reverse" (D-068).

### Terms: the domain type the definition is checked on

Every instrument carries `canonical.Terms`, the structured reading of its title and rules text:

| Field | Example (Kalshi `KXFED-26OCT-T4.25`) | Example (Polymarket "increase 50+ bps after October") |
|---|---|---|
| `subject` | `fed_funds_rate` | `fed_funds_rate` |
| `metric` | `rate_upper_bound` (the level after the meeting) | `rate_change` (the change decided at the meeting) |
| `comparator` | `gt` | `gte` |
| `threshold`, `threshold_high` | `4.25`, n/a | `0.5`, n/a |
| `unit` | `percent` | `percent` |
| `deadline`, `deadline_tz` | `2026-10-28T18:05:00Z`, `America/New_York` | `2026-10-29T03:59:00Z` (close time, flagged `deadline_defaulted`), unknown |
| `resolution_source` | `federalreserve_gov` | `federalreserve_gov` |
| `edge_rules` | `unknown` | `unknown` |
| `polarity` | `positive` | `positive` |

**Unknown is a value.** When a field cannot be read with confidence, the extractor records `unknown` and flags the instrument's `Quality`; it never guesses (CLAUDE.md, D-060 §2, D-067 §4). Two texts stating a field two different ways are also `unknown` (`terms_ambiguous`). This matters for the definition because business rule 2 says any `unknown` on a vetoed field caps the pair at `related`, marked uncertain for later adjudication.

## 2. Methodology: block → veto → classify → facts → basis

The pipeline is D-018's: normalise → block → hard vetoes → classify (with facts) → contract diff → basis risk → optional LLM verify. It is deterministic, and every step leaves a trace on the stored `Relationship` (the diff, the vetoes, the facts used, the basis components, the explanation).

```
normalise   each instrument's title+rules  ->  Terms (+ quality flags)        internal/normalize
block       is the pair a candidate at all?                                   internal/match/block.go
veto        which of the vetoed fields differ or are unknown?                 internal/match/veto.go
classify    no veto -> equivalent; else try derivations; else related         internal/match/classify.go
  derive    set algebra on one shared measurement -> complement / implies     internal/match/derive.go
  facts     metric gap closed by config/facts.yaml -> equivalent_given_facts  internal/match/facts.go
diff        every term field: match / differ / unknown                        internal/match/diff.go
basis       residual risk on every routable or implies verdict                internal/match/basis.go
explain     one human-readable sentence naming the deciding fields and facts  classify.go
```

**Normalise.** Extraction is per category through a registry (`normalize.Register`, D-067): crypto (D-060), Fed (D-067), politics (M2-02). The rules text is the binding document and is read first; the title is the fallback. This is why the Kalshi strike `$76,600 or above` in the subtitle becomes `gt 76599.99` from the rules. Match quality is capped by extraction quality, so each extractor ships with table-driven adversarial tests, property tests and a fuzz target.

**Block** (FR-07). A pair is a candidate only when both sides are in the same category, have the same known subject, and have known deadlines within ±7 days. Anything else is `unrelated` (confidence 0.95 on a known difference, 0.50 when a term was unknown). Blocking is cheap and is what keeps a 700 × 110 crypto universe tractable.

**Veto** (FR-08, D-019). Every vetoed field is compared on both sides and every failure is recorded as a `Veto{Code, Field, Detail}`; the full catalogue is §3. The vetoes are not short-circuited, so the pair page shows all of them.

**Classify** (D-017). With no veto, the pair is `equivalent`. With vetoes, the matcher tries the derived types, and only when the vetoes leave a shared measurement to reason about (D-068 §2): no veto came from an `unknown`, no veto is on subject, deadline or resolution source, the unit is known and equal, and both thresholds are known. Facts and inferences never override those shared-measurement vetoes. Within that envelope:

- a proposition is an interval of the metric plus a polarity: "above x" is (x, +∞), "between lo and hi" is [lo, hi], "eq x" is [x, x]; negative polarity is the complement of the interval;
- `complement` when the two YES sets are exact complements; `implies` when one is a strict subset of the other;
- when the only shared-field veto is the **metric** and the category is Fed, the facts rule (§4) places the change bucket on the level ladder and the same set algebra runs on the result, giving `equivalent_given_facts`, `complement` or `implies`;
- otherwise `related`, with confidence 0.80 when every veto is a known difference and 0.50 when any came from an unknown. A `related` verdict also lists every fact it consulted, so the diff shows why a fact did **not** apply.

There is no tolerance anywhere: one cent is a difference, and a double negation (`gt x` positive versus `lte x` negative) is logically the same set but stays `related` by business rule 1.

**Diff** (FR-09). Every term field is reported as `match`, `differ` or `unknown`. Deadlines match only when identical; a tolerated gap still shows as `differ`, so the page never hides a difference the veto chose to accept.

**Basis risk** (FR-11). Scored for `equivalent`, `equivalent_given_facts`, `complement` and `implies`, from the components in §5, and persisted on the row.

**Persistence** (D-061, Q-5). `equinox match` stores and logs every non-`related` result except `unrelated`, and stores a `related` result only when it is a near miss worth a human look: a fact was consulted, or the thresholds are known and equal or within 0.1 % of each other. Ladder neighbours one strike apart are not near misses. `--store-all` keeps the literal "one row per non-unrelated result" (about 50k rows per round on the recorded universe).

**LLM verify** (D-022, M3). Not part of this document's rules. When it runs it adjudicates `uncertain` pairs and second-opinions every rules `equivalent`, and it may only downgrade; see [ai-usage.md](ai-usage.md).

## 3. Veto catalogue

A `*_mismatch` code means both sides are known and differ; a `*_unknown` code means at least one side could not be parsed. Both are hard vetoes: neither can be `equivalent`. The difference is what happens next. A known mismatch on a proposition field (metric, comparator, threshold, polarity) still allows a derived type; an unknown, or a mismatch on subject, deadline or source, caps the pair at `related` (D-068 §2).

| Code | Field | Fires when | Can a derivation still apply? | Fixture that pins it |
|---|---|---|---|---|
| `subject_mismatch` | subject | different known subjects (also blocked, so in practice `unrelated`) | no | crypto V-05 (BTC vs ETH) |
| `subject_unknown` | subject | subject not stated (also blocked) | no | crypto V-15 |
| `metric_mismatch` | metric | e.g. close price vs touch high; level vs change | yes, Fed only, via facts | crypto V-14; fed FACT-01 |
| `metric_unknown` | metric | cut counts, path-dependent markets, dominance, ETFs | no | fed V-F8 |
| `comparator_mismatch` | comparator | `gt` vs `gte`, above vs below, range vs threshold | yes | crypto V-02, V-07, V-19, V-20, CMP-01 |
| `comparator_unknown` | comparator | no comparison stated, or two stated (`terms_ambiguous`) | no | crypto V-10, V-11, V-25 |
| `threshold_mismatch` | threshold / threshold_high | any decimal difference, no tick-size tolerance | yes | crypto V-01, V-08, V-21, IMP-01, IMP-02 |
| `threshold_unknown` | threshold / threshold_high | amount not stated; range upper bound missing | no | crypto V-10, V-11 |
| `deadline_gap` | deadline | known deadlines **24 h or more** apart ("within 24 h" is strict, D-060 §7) | no | crypto V-03, V-23, V-29 |
| `deadline_mismatch` | deadline | any difference at all in an exact-deadline category (crypto, D-060 §7) | no | crypto V-04, V-12, V-18, V-24 |
| `deadline_unknown` | deadline | no deadline could be read (also blocked) | no | unit test only |
| `source_mismatch` | resolution_source | different known sources, including the same provider's different index | no | crypto V-06, V-22, V-28; fed V-F4 |
| `source_unknown` | resolution_source | no authority named, or several candidates | no | crypto V-09 |
| `polarity_mismatch` | polarity | negated vs positive proposition | yes (gives `complement` on the same threshold) | crypto V-13, V-27; fed V-F5 |
| `polarity_unresolved` | polarity | polarity could not be read | no | crypto V-26 |

Rules the catalogue encodes beyond D-019's list:

- **Resolution source and polarity** were pulled forward from M2 into the M1 veto set because D-017 requires the same source and the veto suite requires "a different index source" to be non-equivalent (D-060 §6).
- **Crypto deadlines are exact.** The generic rule is "within 24 h, read strictly" with a 0.01 confidence deduction per hour of gap. For crypto the deadline is the measurement instant, so any gap is `deadline_mismatch` (D-060 §7; Q-4 option a confirmed for M1). This is stricter than D-019, never looser.
- **No documented-identical sources yet.** D-017 allows "same or documented-identical source". Nothing is documented as identical today: CF Benchmarks' BRTI is not Binance's BTC/USDT, and the matcher does not say otherwise (D-068 §8). The hook for a future Tier 2 decision is the `source_difference` basis component plus a facts-file entry, with the settlement back-test (§7) as the evidence it would need. See §9.
- **Facts never override a veto.** The Fed rule only runs once subject, deadline and source have already agreed; it explains a metric difference, it does not waive one.

## 4. The facts file contract

External facts live in `config/facts.yaml`, loaded by `internal/facts` from `FACTS_FILE` (D-021, D-066). A fact is reviewed data, not venue text, and every use is shown on the trace (FR-10).

```yaml
facts:
  - id: fed_upper_bound          # ^[a-z][a-z0-9_]{0,63}$; the key shown in facts_used
    value: "4.00"                # literal string, ≤128 bytes, in the unit the notes name
    source: https://www.federalreserve.gov/newsevents/pressreleases/monetary20260916a.htm
    as_of: 2026-09-16            # bare UTC date; a future date is rejected, not warned
    notes: >-                    # optional
      Percent. Target range 3.75-4.00 % after the September 16, 2026 FOMC decision ...
```

Contract, as enforced by the loader:

- One top-level `facts:` list; unknown keys at either level are errors, so `as-of` cannot silently drop a field. Duplicate ids are errors. `facts: []` is allowed (the matcher then never emits `equivalent_given_facts`); a missing list, an empty file or malformed YAML fails fast. The file is capped at 1 MiB.
- `value` is a string so the stored text is exactly the file text; a rule that needs a number parses it with `Fact.Decimal()`. The unit is documentation in `notes`, not a field.
- `source` must be an absolute http(s) URL. `as_of` is the date the value became true, not the date it was typed in.
- `equinox match` fails fast when the file is missing or invalid rather than matching without facts (D-068 §1). The zero `facts.Set` is "no facts", and the matcher then says which fact it would have needed.
- **Changing a value is a reviewed change**: new value, new source, new `as_of`, in one MR. Never guess; leave an unknown fact out.

Facts the matcher consults today (`internal/match/facts.go`):

| Id | Shipped value | Role in the rule | Source |
|---|---|---|---|
| `fed_upper_bound` | `4.00` (percent) | The level in force before the meeting. A change bucket "+d" is the level `fed_upper_bound + d`. | Fed press release, 2026-09-16 |
| `fed_next_meeting` | `2026-10-28` | Which FOMC decision that level precedes. The rule applies only when **both** deadlines sit within 48 h after that day's UTC midnight (`MeetingWindow`); a later meeting's bucket depends on this meeting's decision and stays `related`. | FOMC calendar |
| `fed_rate_step` | `0.25` (percentage points) | The grid the upper bound moves on. Closes open ends: "above 4.00" is "at or above 4.25" on the grid. Used only after exact equality has failed, and recorded as `grid_rounding` basis risk. | Fed open-market history |

The claims are tried strongest first: equal sets exactly, equal on the grid, then complement or containment exactly, then on the grid. A verdict lists only the facts it rests on; a `related` verdict lists every fact consulted (D-068 §3). After the October 28 decision all three facts are updated together.

## 5. Basis-risk components

Basis risk is the residual risk that two matched contracts resolve differently even though the matcher called them interchangeable ([domain.md](domain.md)). It is scored from documented components, each in [0, 1], summed and capped at 1, and persisted as `basis_risk` on every `equivalent`, `equivalent_given_facts`, `complement` and `implies` row. `related` and `unrelated` pairs get no score because they are never routed. The weights rank pairs; M3 turns the score into a per-contract penalty in the all-in cost (business rule 6). They are judgment, not estimates (D-068 §5).

| Component | Score | What it measures |
|---|---|---|
| `cutoff_gap` | 0.20 × gap / 24 h, capped at 0.20 | A tolerated deadline gap is a window in which the measurement can move |
| `cutoff_uncertain` | 0.05 per deadline taken from the close time (`deadline_defaulted`) | The rules did not state the instant, so the window is inferred |
| `source_difference` | 0 same known source · 0.25 unknown · 0.50 different | Two authorities can disagree. Today only 0 can reach a routable verdict, because a source veto blocks the rest; 0.25 and 0.50 are the hook for a future documented-identical decision (Q-4) |
| `edge_rule_difference` | 0 known and equal · 0.05 unknown on either side · 0.30 known and different | Early-close, tie and void handling is not yet extracted on most markets, so "not compared" is the usual state |
| `fact_dependency` | 0.05 per fact the verdict rests on | A fact can go stale (an inter-meeting move, a changed step) before the contracts resolve |
| `grid_rounding` | 0.05 when equality relied on the rate grid | An off-grid move resolves by each venue's own rounding rule, which is not compared |

Reading a score: the demo pair in §10 scores 0.383, most of it fact dependency and the Polymarket deadline that was taken from the close time; a same-source, same-instant crypto implication scores 0.05 (edge rules not compared). The pair page lists each component with its detail string.

## 6. Confidence

Confidence is the matcher's confidence in the type it returned, in [0, 1], and is deliberately conservative (D-020). It is not learned. `MIN_ROUTE_CONFIDENCE` (default 0.9) is the routing floor on top of the type's routability.

| Type | Base | Deductions |
|---|---|---|
| `equivalent` | 0.99 (rules can never prove two texts identical) | 0.01 per full hour of deadline gap; 0.02 per side for an unknown unit and for unknown edge rules; 0.05 per defaulted deadline |
| `equivalent_given_facts` | 0.97 | the unit and edge-rule deductions; **not** the gap or defaulted-deadline ones, because `fed_next_meeting` has confirmed both deadlines sit on one decision (that residual is basis risk instead) |
| `complement` | 0.97 | as `equivalent`, or as `equivalent_given_facts` when facts placed it; a further 0.02 when a fact took part |
| `implies` | 0.93 | same as `complement` |
| `related` | 0.80 known differences · 0.50 any unknown (uncertain) | none |
| `unrelated` | 0.95 blocked on a known difference · 0.50 blocked on an unknown | none |

Nothing goes below 0.5, so a long list of caveats never reads as "no confidence". The explanation lists the caveats the deductions stand for ("edge rules not compared; a deadline was taken from the close time").

## 7. How to read eval and back-test output

Three commands produce the accuracy evidence. Two of them are M2-05 and M2-06 deliverables and are **not merged at the time of writing** (see §8); their output format below is the ticket's contract and will be replaced by pasted output once they land.

**`equinox match --round N`** (shipped, M1-07/M2-04) prints one summary per run:

```
round <id>: <n> instruments, <m> cross-venue pairs compared in <duration>
  equivalent <a>, equivalent_given_facts <b>, complement <c>, implies <d>, related <e> (near misses stored <f>, other <g> not stored), unrelated <h>
  relationships stored <s>, decision-log entries <l>
```

Read it as: the first line is the universe size; the second is the type histogram, where the `related` figure splits into near misses that were stored and ladder neighbours that were counted only; the third says what reached the database and the decision log. The live round 3 run recorded in D-065 compared 538,053 cross-venue pairs over 1,881 instruments and stored 768 `related` near misses and 0 `equivalent`. A healthy Fed round shows non-zero `equivalent_given_facts`, `complement` and `implies`; a crypto-only round shows zero of each under today's source rule (§9).

**`equinox eval`** (M2-05, FR-18) reads human-confirmed labels from `testdata/golden/labels.yaml` and reports **precision and recall per relationship type** for the rules-only matcher. The exit gate is **precision ≥ 0.95 at `equivalent`** (D-020, plan M2). Precision is what matters: a false `equivalent` routes money to the wrong bet, a miss only loses an option, so recall is reported, not gated. `equinox golden draft` proposes about 40 candidate pairs into `candidates.yaml`; **only a human writes `labels.yaml`** (Tier 3), and the worker never invents labels. `--require-labels` makes the command exit non-zero when the labels file is empty, which is how CI can refuse to call an unlabelled run a pass.

**`equinox backtest`** (M2-06, FR-19, NFR-07) fetches settled markets from both venues (fixture-recorded for tests; live only behind an explicit flag), runs the matcher on settled cross-venue pairs, and lists every `equivalent` or `equivalent_given_facts` pair whose settlements disagree. The gate is **0 mismatches, or each one investigated and logged** as a decision. A mismatch on a pair the matcher called equivalent is the strongest possible evidence that a rule is wrong; an exception can be allowlisted only with a logged reason.

The fixture run of 2026-10-07 (M2-06, `go run ./cmd/equinox backtest --fixtures`; the full per-pair list follows the table on the terminal):

```
Settlement back-test: 1031 settled instruments (kalshi 411, polymarket 620), 207544 cross-venue pairs compared, 0 rejected records; facts available: fed_next_meeting, fed_rate_step, fed_upper_bound
Gate (NFR-07): 0 equivalent-tier pairs settled differently — PASS

| Type | Pairs | Checked | Agreed | Disagreed | Allowlisted | Settlement rule |
|---|---|---|---|---|---|---|
| equivalent | 0 | 0 | 0 | 0 | 0 | same outcome |
| equivalent_given_facts | 1 | 1 | 1 | 0 | 0 | same outcome |
| complement | 1 | 1 | 1 | 0 | 0 | opposite outcomes |
| implies | 22 | 22 | 22 | 0 | 0 | YES on the forcing side needs YES on the forced side |
| related | 206486 | – | – | – | – | no payout claim |
| unrelated | 1034 | – | – | – | – | no payout claim |

- equivalent_given_facts: kalshi:KXFED-26SEP-T4.00 settled no, polymarket:8891…2293 settled no (confidence 0.93; expected same outcome): agreed
  Will the upper bound of the federal funds rate be above 4.00% following the Fed's Sep 16,… | Will the Fed increase interest rates by 50+ bps after the September 2026 meeting?
- complement: kalshi:KXFED-26SEP-T3.25 settled yes, polymarket:9718…3603 settled no (confidence 0.91; expected opposite outcomes): agreed
  Will the upper bound of the federal funds rate be above 3.25% following the Fed's Sep 16,… | Will the Fed decrease interest rates by 50+ bps after the September 2026 meeting?
```

Read it as: the first line is the sample (every settled market both adapters returned for the recorded universe, after validation), the second is the NFR-07 gate, and the table is the type histogram with, for each type that makes a payout claim, how many verdicts the settlements confirmed or contradicted. `related` and `unrelated` claim nothing and are only counted. Every disagreement is listed under the table with both outcomes and titles; an allowlisted one (an investigated exception, `--allowlist`, which needs a reason and a `D-` entry per pair) is listed separately and does not fail the gate. The exit code is 1 when the gate fails. The sample's one meeting is the September 16, 2026 FOMC decision (Kalshi `KXFED-26SEP` ladder vs Polymarket "Fed Decision in September?"), matched with the facts in force before it (`testdata/fixtures/backtest/facts.yaml`, `fed_upper_bound=3.75`); the crypto pairs (`KXBTCD` vs "Bitcoin above X on October 6, 5PM ET?") resolve on different sources and stay `related`, so the gate says nothing about Q-4 either way. Fetch failures for series or tags with no recorded fixture (ethereum, D-057) are printed as warnings and shrink the sample visibly rather than silently.

**The veto suite** (`go test ./internal/match/... -run TestPairsFromFixtures -v`) is the third number and is already green: every `V-` case in `testdata/match/{crypto,fed}_pairs.yaml` is an adversarial near miss that must be non-equivalent, and the test fails on any `equivalent`-tier verdict for them (NFR-06, "0 veto-suite false positives").

## 8. Current results

_As of 2026-10-07, main at the M2-04 merge (`638daf9`)._

| Gate (plan M2 exit) | Status | Evidence |
|---|---|---|
| 0 veto-suite false positives | **met** | 37 `V-` cases (29 crypto, 8 Fed), 0 `equivalent`-tier verdicts; `TestPairsFromFixtures`, `TestPairSuiteSymmetric`, `TestPairSuiteWithoutFacts` pass on `internal/match` |
| Fixture positives | **met** | 4 `EQ-` crypto equivalents, 1 Fed `EQ-F1` equivalent; `FACT-01` equivalent_given_facts at confidence 0.93; `CMP-01`, `CMP-F1`, `V-13`, `V-F5` complements; `IMP-01/02`, `IMP-F1/F2/F3`, `V-01/02/08/19/21`, `V-F7` implications |
| Precision ≥ 0.95 at `equivalent` on human-confirmed labels | **pending human labels** | `equinox eval` and `testdata/golden/` are M2-05, not yet merged; no labels have been confirmed. **TODO (M2-05 worker, then human):** paste the `equinox eval` table here once `labels.yaml` is confirmed, and record the label count and date. |
| 0 back-test mismatches | **met on the fixture set** | `equinox backtest --fixtures` on the 2026-10-06 recording: 1,031 settled instruments (411 Kalshi, 620 Polymarket), 207,544 cross-venue pairs; 0 `equivalent`, 1 `equivalent_given_facts`, 1 `complement`, 22 `implies`, and every one of the 24 checked verdicts agreed with the settlements; 0 allowlisted exceptions. `TestBacktestFixtures` (internal/eval) and `TestBacktestCommandFixtures` (cmd/equinox) pin it; the pasted output is in §7. A `--live` run is operator-only and not yet recorded (D-072). |
| ≥ 10 true `equivalent` or `complement` pairs across ≥ 2 categories in a live round | **not yet measured** | Live round 3 (D-065): 1,881 instruments, 768 `related`, 0 `equivalent`, matched before M2-04 landed. Needs a Fed-inclusive round re-matched with the M2-04 matcher and the politics normaliser (M2-02). |

Fixture crypto equivalents are necessarily **synthetic in one venue's phrasing** (Polymarket wording on CF Benchmarks, Kalshi wording on Binance), because no recorded cross-venue crypto pair shares a source (D-061 §6; §9 below). The Fed fixtures are recorded text from both venues; `FACT-01`, `CMP-F1` and `IMP-F1` are real pairs.

## 9. Known failure modes

These are the ways the matcher is known to be wrong or silent today, with the decision that accepted each. "Wrong" here means a recall loss unless stated otherwise: by construction nothing in this list can produce a false `equivalent`.

**Q-4: recorded cross-venue crypto pairs are never `equivalent`.** In the recorded data Kalshi's daily BTC and ETH ladders resolve on CF Benchmarks' real-time index at 5 or 6 PM ET and Polymarket's daily markets on Binance's BTC/USDT at noon ET. Fixture `V-24` is the recorded pair with the same day and strike label; the matcher says:

```
related: same subject BTC with deadlines 5h0m0s apart (within 168h0m0s), but vetoed on
threshold (75999.99 vs 76000), deadline (2026-10-07T21:00:00Z vs 2026-10-07T16:00:00Z:
5h0m0s apart; crypto deadlines are the measurement instant and must be identical),
resolution_source (cf_benchmarks_brti vs binance_btc_usdt).
```

Three independent vetoes fire. Two of them are real payout differences, not conventions: a price at 5 PM is a different random variable from a price at noon, and two indices can print different numbers at the same instant. Live round 3 confirmed 0 `equivalent` and 768 `related` (D-065 §4). Option (a) of Q-4 was accepted for M1, and the M2 revisit is explicit: a documented source equivalence would be a Tier 2 decision backed by the settlement back-test, and nothing in M2-04 acted on it (D-068 §8). Until then the demo's routable pairs come from the Fed category.

**Q-4 near miss: the one-cent strike convention.** Kalshi writes its strikes as `X99.99` in the rules ("above 76599.99") and Polymarket as round numbers ("above $76,600"). With the same source and instant that pair is a true `implies` (`V-21`: "gt 76600 forces gt 76599.99", confidence 0.89, basis 0.05), never `equivalent`, because the two contracts pay differently when the index prints exactly 76,600.00. Tick-size normalisation was rejected as a guess (D-060 §1); a per-source precision fact is the M2+ route if the back-test shows the gap never bites.

**Q-4 near miss: ET versus UTC midnight, and defaulted deadlines.** `V-04` and `V-18` show the same calendar date and the same clock reading in different zones; `V-12` shows a market whose rules state no time, so its deadline falls back to the close time (`deadline_defaulted`). All three are `deadline_mismatch` under the exact crypto rule. The zone one is a true difference; the defaulted one may be a parse limitation, which is why defaulted deadlines also cost 0.05 confidence and 0.05 basis on pairs that do match.

**Metric unknown on path-dependent and count markets.** "Reach 5.0 % at any point", "2 rate cuts in 2026", "Hike–Pause–Hike", dominance, volatility and ETF titles all get an `unknown` metric (D-060 §3, D-067 §4). They can never be matched; `V-F8` pins the behaviour. This is deliberate: a guessed metric is the kind of error the back-test would catch only after money moved.

**Edge rules are not extracted.** `edge_rules` is `unknown` on nearly every market, so early-close, tie and void handling is never compared. It costs 0.02 confidence per side and 0.05 basis, and the explanation says "edge rules not compared". A pair that differs only in void handling would today be called `equivalent` with those caveats; this is the one place where the definition's fourth clause is scored rather than enforced.

**The December bucket is `related`, by design.** Polymarket's "Fed Decision in December?" buckets measure the change versus the level before the December meeting, which is October's decision. `fed_upper_bound` is the level before October, so applying it to December would place the bucket on a stale base. The rule refuses when either deadline is outside the `fed_next_meeting` window, lists the facts it consulted, and `match.NearMiss` stores the pair so it appears as a near miss (`V-F1`, §10). Once October's decision is published and the facts are updated, December becomes matchable and October becomes history.

**Grid equality is a stated assumption.** "Above 4.00" equals "at or above 4.25" only if the Fed moves in 25 bp steps. Every change since December 2008 has, and the Polymarket rules round off-grid changes to the nearest bracket, but the two venues' rounding rules are not compared. Every grid verdict carries `grid_rounding` 0.05 and names `fed_rate_step` so a reader can see the dependency.

**Deadline tolerance outside crypto hides nothing but costs confidence.** The Fed demo pair's deadlines are 9 h 54 min apart (Kalshi states 2:05 PM ET; Polymarket's rules state no time so its close time is used). The generic 24 h rule accepts that; `fed_next_meeting` confirms both sit on one decision. The diff still shows `deadline: differ`.

**Politics is thin.** The politics normaliser (M2-02, MR !22) is in review at the time of writing. Until it merges, politics instruments carry `UnknownTerms` and never pass blocking.

**The LLM has not run.** Every stored relationship today has method `rules`. The verifier (D-022) is wired but disabled in production until the human adds the key (status.md Q-3); when it runs it can only downgrade.

## 10. The Fed demo pair walkthrough

The plan's demo: Kalshi `KXFED-26OCT-T4.25` ("Will the upper bound of the federal funds rate be above 4.25% following the Fed's Oct 28, 2026 meeting?") against Polymarket "Will the Fed increase interest rates by 50+ bps after the October 2026 meeting?" Both texts are recorded venue text (`testdata/match/fed_pairs.yaml`, case `FACT-01`); the output below is `match.Compare` on them with the shipped `config/facts.yaml`.

**Step 1, normalise.** Kalshi's rules name the metric (upper bound), comparator and threshold ("greater than 4.25%"), the meeting ("Oct 28, 2026") and the time ("2:05 PM ET"), and the authority ("Federal Reserve's official website"). Polymarket's shared rules name the metric as a change in basis points versus the level before the October meeting and the authority as the FOMC statement; the bucket itself ("increase … 50+ bps") is read from the title, giving `rate_change gte 0.5` in percentage points (D-067 §3, §5). Polymarket states no time, so its deadline is the close time, flagged `deadline_defaulted`.

**Step 2, block.** Same category (`fed`), same subject (`fed_funds_rate`), deadlines 9 h 54 min apart: a candidate.

**Step 3, veto.** Three vetoes fire, all on the proposition and all known differences:

```
metric_mismatch      rate_upper_bound vs rate_change
comparator_mismatch  gt vs gte
threshold_mismatch   4.25 vs 0.5
```

Subject, deadline (within 24 h), source (`federalreserve_gov` on both) and polarity agree, the unit is `percent` on both, and both thresholds are known. The pair is therefore derivable, and because the metric differs in the Fed category the facts rule runs.

**Step 4, facts.** `fed_upper_bound=4.00` places the bucket: "gte 0.5" on the change is "gte 4.5" on the level. `fed_next_meeting=2026-10-28` confirms both deadlines sit within 48 h after that day's UTC midnight (Kalshi 18:05 Z on the 28th, Polymarket 03:59 Z on the 29th). Exact equality fails (`gt 4.25` is not `gte 4.5`), so `fed_rate_step=0.25` closes the open end: on the grid `gt 4.25` is `gte 4.5`. The two sets coincide.

**Step 5, verdict.** The stored relationship:

```
type        equivalent_given_facts
confidence  0.93        (0.97 base − 0.04 edge rules unknown on both sides; no deadline deductions
                         because the meeting fact confirmed one decision)
explanation equivalent_given_facts: the same payout once the facts are applied. Shared: subject
            fed_funds_rate, source federalreserve_gov, unit percent; deadlines 2026-10-28T18:05:00Z
            and 2026-10-29T03:59:00Z are 9h54m0s apart. Differs on metric (rate_upper_bound vs
            rate_change), comparator (gt vs gte), threshold (4.25 vs 0.5). Reasoning: given
            fed_upper_bound=4.00 (level before the 2026-10-28 meeting, fed_next_meeting),
            polymarket:2589814 gte 0.5 is the level gte 4.5; on the 0.25 grid (fed_rate_step)
            gt 4.25 is gte 4.5 and gte 4.5 is gte 4.5: the same set. Facts used:
            fed_upper_bound=4.00 (as of 2026-09-16), fed_next_meeting=2026-10-28 (as of 2026-10-06),
            fed_rate_step=0.25 (as of 2026-10-06). Caveats: edge rules not compared; a deadline was
            taken from the close time.
facts_used  fed_upper_bound=4.00      federalreserve.gov press release monetary20260916a   as of 2026-09-16
            fed_next_meeting=2026-10-28  federalreserve.gov FOMC calendar                  as of 2026-10-06
            fed_rate_step=0.25        federalreserve.gov open-market history               as of 2026-10-06
basis_risk  0.383
            cutoff_gap            0.083  deadlines 9h54m0s apart
            cutoff_uncertain      0.05   deadline taken from the close time on polymarket:2589814
            source_difference     0      both resolve on federalreserve_gov
            edge_rule_difference  0.05   edge rules not compared (unknown on at least one side)
            fact_dependency       0.15   rests on fed_upper_bound, fed_next_meeting, fed_rate_step
            grid_rounding         0.05   equality relies on the rate grid
```

Confidence 0.93 clears `MIN_ROUTE_CONFIDENCE` 0.9, and the type is routable, so `POST /route` on either side considers the other venue. Note the plan's wording ("above 4.25% ≡ increase 25+ bps given 4.25") predates the September hike; with the shipped fact of 4.00 the matching bucket is 50+ bps, and "increase 25 bps" (`eq 0.25`, the level `eq 4.25`) is instead disjoint from `gt 4.25` and stays `related` (`V-F3`), while it `implies` `KXFED-26OCT-T4.00` (`IMP-F1`, confidence 0.87).

**The neighbours.** The same facts give a complement and an implication on real text: "decrease 50+ bps" (`lte −0.5`, the level `lte 3.5`) is exactly NO on `KXFED-26OCT-T3.50` (`CMP-F1`, `complement`, confidence 0.91, basis 0.283), and "increase 25 bps" forces `KXFED-26OCT-T4.00` (`IMP-F1`).

**The near miss.** Kalshi `KXFED-26DEC-T4.00` against Polymarket "increase 50+ bps after the December 2026 meeting" (`V-F1`) has the same three proposition vetoes and the same shared fields, but the rule refuses:

```
type        related   (confidence 0.80; near miss, stored)
explanation related: same subject fed_funds_rate with deadlines 4h54m0s apart (within 168h0m0s),
            but vetoed on metric (rate_upper_bound vs rate_change), comparator (gt vs gte),
            threshold (4 vs 0.5). Facts: fact fed_upper_bound=4.00 is the level before the
            2026-10-28 meeting (fed_next_meeting), but kalshi:KXFED-26DEC-T4.00 resolves on
            2026-12-09: a later meeting's change depends on the prior decision, so the fact
            does not apply.
facts_used  fed_upper_bound=4.00, fed_next_meeting=2026-10-28
basis_risk  none (not routable)
```

That is domain.md's "outcomes that depend on a prior event": December's change is measured against whatever October decides. The pair is stored because a fact was consulted, so the near-misses view shows it next to the demo pair with the veto that stopped it and the facts that were tried.
