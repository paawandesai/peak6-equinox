# Equinox UI redesign: research brief

Web research, 8 Oct 2026. Every claim comes from a page under Sources; unverified items are flagged. Stack: Go html/template + htmx + Pico.css, no SPA.

**Verification gaps.** Kalshi and Polymarket layouts come from their help docs, not the live (client-rendered) screens. OddsJam's tool pages were blocked, so only its walkthrough is cited; for Oddschecker I saw only the league odds list. I found no public per-order IBKR SmartRouting display, only its TCA report. Allium was not researched. A search result reported Verso offline (Aug 2026); unconfirmed.

## 1. Patterns by area

### Prediction-market UIs (Kalshi, Polymarket, Manifold)
- **Rules come in layers, and the title is not the contract.** Kalshi shows a per-strike *rules summary*, then "View full rules", then *Additional Details* where clarifications get posted. Polymarket puts *Rules* under the order book and can publish an "Additional context" clarification. Its docs say plainly: "The market title describes the question, but the rules define how it resolves." OddsShopper documents a 90¢ Yes that settled No because traders read only the summary layer.
- **A shared rule anatomy.** Polymarket US splits rules into four parts: resolution criteria (including 50/50 or last-fair-price fallbacks), qualifying requirements and edge cases, resolution timeframe, and resolution sources (primary and backup). That is a ready-made row schema for a terms diff.
- **Calibration published alongside its method.** Manifold's calibration page plots predicted vs. actual against a diagonal, reports a Brier score, and spells out the sampling method and sample size (2% of trades hourly, ≥15 traders, 100k sample).
- *Anti-pattern:* surfacing only the title or one-line summary as if it were the contract.

### Cross-venue arbitrage and odds comparison
- **Freshness and scope in the header.** PredRadar opens with "LIVE · updated <timestamp> UTC" plus counters (live opportunities, average and largest spread, markets scanned). Oddpool says "Updates every 30s."
- **Net-of-fee numbers that show their assumptions.** PredRadar labels every spread as net of estimated fees (~2% Polymarket taker, ~1.5% Kalshi) and has a glossary for bps, ROI and direction ("BUY K → SELL P"). The Apify finder walks the order book at a set stake, so its prices are executable rather than top-of-book.
- **Match-quality gates and a suspicion flag.** The Apify finder runs hard gates (resolution-date window, strike overlap, deadline month, opposite-direction words, event stage), attaches `match_confidence` to every row, and sets `match_review_recommended` on large edges from imperfect matches: "a 20% 'arb' is almost always a wrong pairing." PredRadar puts a ⚠ on implausible spreads.
- **Match type and similarity as first-class filters.** Predicts.guru offers Exact / Related / All, defaults to Min Similarity 95, and has risk flags. Prediction Hunt keeps a banner warning that linked events "may resolve differently" and links each venue's rulebook.
- **Change semantics.** Apify's monitor mode emits only new / widened / narrowed / gone. OddsJam notes that some arbs last under 90 seconds.
- **Caveats stated up front.** The Dune "Prediction Markets" dashboard puts "Hyperliquid is manual refresh RN" in its title and notes that Kalshi data "will never contain the current day."
- *Anti-pattern:* PredRadar's default ROI sort puts a 9,402 bps / 2,410% "arb" on $584 of volume at the top. Ranking by edge rewards bad matches.

### Smart order routing, execution transparency, TCA
- **A collapsed summary line that expands into a route.** Jupiter's rate line expands to Network Fee, Platform Fee and Route ("1 route and 1 market"). Clicking Route shows the path and how the amount is split across venues. In the API, `routePlan` gives each leg as `bps`, summing to 10,000. 1inch reports a `part` % per protocol.
- **Quoted vs. executed, stated honestly.** Jupiter warns that JIT routing can change the venues or the split at execution. The CoW Explorer order view lists limit price, execution price, surplus, Filled %, fees, and a fixed status set (Open / Filled / Partially filled / Expired / Cancelled).
- **Benchmarks in bps with a clear sign rule.** IBKR's TCA marks each trade to benchmarks (price improvement, arrival, VWAP, +1s…+300s) in bps, where positive means favorable and negatives appear in parentheses. It drills down by venue, order type, size and live time. Active filters show as removable chips, and there is an Outlier Analysis section.
- **Named alternatives.** 1inch offers "Maximum return" vs. "Lowest gas" modes plus a comparative table, and falls back to other legs when one leg fails.
- *Anti-pattern:* animated node graphs (e.g. the community ReactFlow-based Jupiter Router Explorer): heavy JS, nothing a proportional bar can't show.

### Entity matching / record-linkage review
- **Evidence waterfall.** Splink's chart starts with a Prior bar, adds a signed bar for each comparison, and ends with a total match weight converted to probability. Its tooltip shows both values, the comparison level, the Bayes factor and the running probability.
- **Side-by-side "why", with detracting rows in red.** Senzing's `why` puts the two entities in columns with a score per feature and the best-scoring pair on top. A detracting DOB row turns red. The compact match key (`+NAME+ADDRESS`) names the principle that fired, and candidate keys appear below. `how` replays the merge one step at a time.
- **Graded match levels.** Senzing grades Matches / Possible matches / Possibly related / Ambiguous / Disclosed relations, close to Equinox's labels; `sz_audit` scores P/R/F1 against a truth set.
- **A steward review screen.** Reltio's column view compares up to 4 profiles, highlights similar attributes, lists the match rules with a relevance % and any negative rules, and offers Merge / Not a match, with a separate Not-a-match tab.
- *Anti-pattern:* two views that disagree. Splink issue #2529 shows the dashboard reporting 82.8% while the waterfall reported 30.8% for the same pair, because only one of them applied term-frequency adjustments.

### ML evaluation
- **Per-class table with support.** scikit-learn's classification report gives per-class precision, recall, F1 and support, plus macro and weighted averages.
- **Evidently's report layout.** In order: summary metrics, class representation, confusion matrix, quality by class, then a precision-recall table at thresholds (top-X% with count, threshold, P, R, TP, FP). It can also compare against a reference run.
- *Anti-pattern:* one headline accuracy number with no support counts, which hides rare classes such as *implies* and *complement*.

### Ops and admin health
- **Worst-component rollup.** Dagster gives each asset one status, Unknown / Healthy / Warning / Degraded, taken from the worst of its latest materialization, freshness and checks. Freshness policies have separate warn and fail windows, and the catalog groups and filters by status.
- **Freshness against an SLA.** dbt compares source freshness to the SLA and advises checking at twice the tightest SLA frequency.
- **History bars.** Statuspage has five component states and 90 daily bars running green→yellow→orange→red. Days with no data are **gray**, and hovering a day links to its incident.
- *Anti-pattern:* showing green when status is actually unknown. Dagster has an explicit Unknown state, and Statuspage uses gray for no data.

## 2. Design brief (opinionated)

**Shared system**
- **Relationship badge.** Six fixed labels, each conveyed by text plus a glyph and never by color alone: `≡ equivalent` (solid green), `≡* equivalent given facts` (green outline, with the facts listed on hover via Pico `data-tooltip`), `⇒ implies` and `¬ complement` (blue, directional), `~ related` (amber), `∅ unrelated` (gray). This follows Senzing's graded levels.
- **Confidence.** Always show the number with a word bin (e.g. "0.91 High"). Basis risk gets its own badge and is never folded into confidence.
- **Freshness stamp on every data panel.** A `<time datetime>` element reading "as of 12:04:31 CT · 14s ago", with states Fresh / Warn / Stale / Unknown driven by per-source warn and fail windows (Dagster). The server computes the age; the stamp refreshes with `hx-trigger="every 15s [!document.hidden]"`.
- **Stack mechanics.** Disclosure uses `<details>` (Pico accordion). Bars are server-rendered `<div style="width:NN%">` or inline SVG, so no chart library is needed. Lazy panels use `hx-trigger="revealed"`. Filter state lives in the URL via `hx-push-url`. Tables stack into cards at 390px.

**Matches list (P1, P2).** One row per pair: both venue titles, badge, confidence, basis risk, a gate strip (✓/✗ for date, strike, direction, stage, after Apify), net-of-fee gap and freshness. The header carries counters and an "updated" stamp (PredRadar). Default sort is by **review priority**, not edge. Show ⚠ when the edge is large and confidence is below threshold. Filters render as removable chips (IBKR).

**Pair detail (P1).** Two contract cards side by side under the verdict badge. The main body is a **terms diff** with one row per rule part (criteria, qualifying/edge cases, timeframe/cutoff, sources, strike/threshold, direction). Each row shows left value, right value, a match / differs / missing verdict, and a signed contribution, read in Splink's waterfall order, with detracting rows red (Senzing). Word-level diffs use server-side `<ins>`/`<del>`. Layer tabs (title / summary / full rules / clarifications) mirror the venues' own layers and flag any clarification. Basis risk links back to the rows that cause it. The LLM verifier rationale sits in a `<details>` with run ID and timestamp.

**Near misses (P1).** This is Senzing's "why not": lead with the gate or term that failed and the distance to threshold, and group by failure reason. A "not a match" decision is a viewable record, as in Reltio's tab.

**Eval (P1).** Header: golden-set version, run time, and delta vs. the previous run (Evidently's reference comparison). Below it, a per-class P/R/F1 table **with support** (scikit-learn), then a 6×6 confusion matrix as an HTML table shaded by inline style. Each cell `hx-get`s the misclassified pairs, which link to Pair detail. Finish with a P/R-at-threshold table. Add a calibration plot (Manifold) only if confidences are probabilities.

**Route + result (P2).** One summary line ("Kalshi · 62.4¢ all-in · 2 venues") that expands, as Jupiter's does. Show a split bar whose segments sum to 100%, quoted vs. simulated fill, fees, slippage in bps with the sign rule written on screen (IBKR), and a status from a fixed set (CoW).

**Decision trace (P2).** Ordered steps, like Senzing's `how`: input snapshot with the age of each quote; eligibility, with stale quotes struck through and the reason given; fee math; then a ranked alternatives table with the chosen row highlighted and Δ vs. chosen in ¢ and bps (1inch comparative table).

**Decision log / history (P2, P3).** Modeled on TCA: chip filters, group-by (venue, label, staleness), an outliers section, and a link from each row to its trace. Columns: time, pair, side, size, venues, all-in price, Δ vs. best alternative, max quote age, and the label at decision time.

**Admin (P3).** One health row each for collectors, matcher and LLM verifier, using the Dagster rollup with the warn and fail windows shown. Add Statuspage-style history bars (gray = no data), an LLM verifier runs table, and a "data notes" block for known gaps, as the Dune dashboard does.

**Avoid:** default sorting by edge, color-only badges, unlabeled sign conventions, animated route graphs, and green for unknown. Never let a score show without its reasons, or two views disagree on the same number.

## Sources (visited)
- https://help.kalshi.com/en/articles/13823823-rules-summary
- https://help.kalshi.com/en/articles/13823824-additional-details
- https://help.kalshi.com/en/articles/13823822-market-rules
- https://docs.polymarket.com/concepts/resolution
- https://docs.polymarket.us/learn/advanced/rule-structure
- https://www.oddsshopper.com/articles/prediction-markets/kalshi-rules-summary-vs-full-rules
- https://manifold.markets/calibration
- https://predradar.com/arbs
- https://www.predicts.guru/arbitrage
- https://www.predictionhunt.com/arbitrage
- https://apify.com/antndev/polymarket-kalshi-arbitrage-finder
- https://www.oddpool.com/arb-dashboard (search-result extract)
- https://oddsjam.com/betting-education/oddsjam-walkthrough-how-i-use-oddsjam
- https://www.oddschecker.com/football/english/premier-league
- https://pmwisdom.com/projects/verso
- https://dune.com/datadashboards/prediction-markets
- https://docs.jup.ag/user-docs/trade/spot/ultra-mode
- https://1inch.com/blog/post/introducing-1inch-v2-defis-fastest-and-most-advanced-aggregation-protocol
- https://docs.cow.fi/cow-protocol/tutorials/cow-explorer/order
- https://www.ibkrguides.com/brokerportal/performanceandstatements/understandingtrans.htm
- https://moj-analytical-services.github.io/splink/charts/waterfall_chart.html
- https://github.com/moj-analytical-services/splink/issues/2529 (search-result extract)
- https://senzing.com/docs/tutorials/eda/eda_basic_exploration/
- https://www.senzing.com/docs/tutorials/eda/eda_snapshot/ (search-result extract)
- https://docs.reltio.com/en/applications/hub/profiles-at-a-glance/profile-perspectives-tabs/profile-perspectives-navigation/potential-matches-perspective/potential-matches-perspective-navigation
- https://scikit-learn.org/stable/modules/generated/sklearn.metrics.classification_report.html
- https://docs-old.evidentlyai.com/presets/class-performance
- https://docs.dagster.io/guides/observe/asset-health-status
- https://docs.dagster.io/guides/observe/asset-freshness-policies
- https://docs.getdbt.com/docs/deploy/source-freshness
- https://support.atlassian.com/statuspage/docs/display-historical-uptime-of-components/
- https://htmx.org/attributes/hx-trigger/
- https://picocss.com/docs/accordion
- https://picocss.com/docs/tooltip
