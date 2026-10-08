# UI-09 handoff: Equinox redesign build spec (Revision 1)

Revision 1 applies the page-by-page brief in RESEARCH.md (8 Oct 2026) to every page.

The stack does not change (D-031, D-081): Go `html/template` + htmx + Pico.css. There is no JS build, no SPA, no inline styles (CSP), and no new fonts or CDN assets.

The work touches:
- `internal/web/templates/*.html`
- `internal/web/static/app.css` (drop-in: `handoff/app.css`)
- a few view-model fields in `internal/web/viewmodel.go` and the page handlers

Three kinds of change are tagged throughout:
- **CSS**: classes in `handoff/app.css`.
- **VM**: a new view-model field computed from data the server **already stores**.
- **◆ NEW DATA**: something the server does **not** produce today. It is drawn with a dashed purple outline in the mocks, must not be shipped as if it exists, and is listed in full at the end.

All sample numbers in the mocks are illustrative.

## Files

| Design file | What it is |
|---|---|
| `Equinox Design System v2.dc.html` | Badge set, confidence, freshness stamp, health and outcome states, colour, type, design-only marker, page index |
| `Eq Badge.dc.html`, `Eq Conf.dc.html`, `Eq Fresh.dc.html` | Mock-only building blocks for the badge, confidence and stamp. In Go these become `{{template "badge"}}`, `{{template "conf"}}` and `{{template "stamp"}}` defines in `layout.html`. |
| `Eq Header v2.dc.html` | Layout header: banners, nav, freshness stamp, timezone form |
| `Matches v2` … `Error v2` | One file per page: a 1280 desktop frame, a 390 mobile frame and build notes |
| `handoff/app.css` | v1 CSS plus a Revision 1 section at the end. Every v1 class is kept. |
| v1 files (`Matches.dc.html` etc.) | The previous pass, kept for comparison |

The mocks use inline styles only because the design tool needs them. **Do not copy them into templates.** Use the classes named below.

## Design → template map

| Page | Design | Templates | Ticket |
|---|---|---|---|
| Layout, header, banners | Eq Header v2, Design System v2 | `layout.html`, `app.css` | UI-02 |
| Matches | Matches v2 | `matches.html` | UI-03 |
| Pair detail | Pair v2 | `pair.html`, `history.html` | UI-04 |
| Route + result | Route v2 | `pair.html` (#route), `route.html`, `route_result.html` | UI-05 |
| Decision trace | Decision v2 | `decision.html`, `route_result.html` | UI-05 |
| Near misses | Near Misses v2 | `near_misses.html` | UI-06 |
| Decision log | Log v2 | `log.html` | UI-06 |
| Eval | Eval v2 | `eval.html` | UI-07 |
| Admin | Admin v2 | `admin.html` (through `layout.html`) | UI-08 |
| Error | Error v2 | `error.html` | UI-02 |

Order of work: replace `app.css` first; it is backwards compatible. Add the three shared defines to `layout.html`, then do the pages in the design.md §8 order.

## Shared system (layout.html + app.css)

### Badge: one set everywhere

`{{define "badge"}}<span class="badge {{.Class}}"><span class="g" aria-hidden="true">{{.Glyph}}</span>{{.Text}}</span>{{end}}`

**VM:** add a `Badge{Class, Glyph, Text}` helper in `viewmodel.go` (`TypeBadge`, `FreshBadge`, `HealthBadge`, `VerdictBadge`, `OutcomeBadge`). The glyph is markup, not CSS `content`, so it survives copy and is hidden from screen readers.

| Set | States (glyph, text, class) |
|---|---|
| Relationship | `≡ equivalent` `.b-equivalent` (solid) · `≡* equivalent_given_facts` `.b-equivalent_given_facts` (outline; facts listed in `data-tooltip`) · `¬ complement` and `⇒ implies` (blue) · `~ related` (amber) · `∅ unrelated` (gray outline) |
| Freshness | `✓ Fresh` `.f-fresh` · `◐ Warn` `.f-warn` · `✕ Stale` `.f-stale` · `? Unknown` `.f-unknown` · `◆ Pinned` `.f-pinned` (replaces the state while a round is pinned) |
| Health | `✓ Healthy` · `! Warning` · `✕ Degraded` · `? Unknown` (`.h-*`). Gray always means unknown. |
| Diff verdict | `✓ match` · `✕ differs` · `◇ closed by fact` · `? unknown` (`.v-*`) |
| Outcome | `✓ Routed` · `◐ Partially routed` · `✕ Not routed` (`.o-*`, solid). This is the fixed set. |
| Review flag | `⚠ review: large edge, low confidence` `.b-review` (◆ NEW DATA, see Matches) |

The text stays the server's type string (e.g. `equivalent_given_facts`), so tests that match on chip text keep passing. Complement moves from green to blue as in the brief. Routability is stated in words ("Routable pairs", "✓ yes").

### Confidence

`{{define "conf"}}`: `<span class="conf"><strong>0.93</strong> <span class="w-high">▲ High</span><meter min=0 max=1 low=.7 high=.9 optimum=1 value=.93></span>`. The bins are unchanged from design.md §4. **VM:** `ConfidenceValue float`. Basis risk is a separate label: score, then `■ moderate` in the warn colour. It is never folded into confidence.

### Freshness stamp

`{{define "stamp"}}`: `<span class="stamp" hx-get="/stamp?src=…" hx-trigger="every 15s [!document.hidden]" hx-swap="outerHTML">{{badge}} as of <time datetime="…">11:03:20 EDT</time> · 14 s ago</span>`. It is used in:
- the header context bar
- every quote row (Pair touch, Decision snapshot)
- Matches rows
- Log rows

Requirements:
- **VM:** `Age` (server computes now − AsOf, or FetchedAt − snapshot) and `FreshState`.
- Stale uses the existing D-077 threshold. The text "Data is 42 minutes old." stays in the stale banner, which moves to danger styling with a ✕ glyph.
- A small `GET /stamp` handler re-renders the define. It is optional; without it the stamp is static.
- ◆ **NEW DATA:** Warn needs a second, earlier window per source (config). Until it exists, only Fresh, Stale, Unknown and Pinned render.

### Header (`layout.html`) → Eq Header v2

- Banners gain a glyph: demo `◆` info, no-data `?` warn, stale `✕` danger. Their text is unchanged.
- The context bar is the stamp plus "· round N[, pinned]", with the timezone form at the right end.
- Everything else is as in v1: nav with `aria-current`, `Page` VM field, the `pagehead` define, the question in each page head.

## Matches → `matches.html` (Matches v2)

1. `.page-head`, then the counter `dl.strip`:
   - Round, As of, Pairs kept, Routable (`strip-key`), Implies, Related, truncation cell (all kept)
   - **new cell:** Flagged for review (◆ NEW DATA)
2. Toolbar `form.toolbar-chips` (GET):
   - Sort `<select name=sort>`: **Review priority** (default), Confidence, Pair id
   - removable `a.chip-filter.removable` chips for active filters (`min`, `q`)
   - the review-priority rule written out
   - Filter state lives in the URL (`hx-push-url="true"` if swapped with htmx).
3. Sections stay as before: Routable (with Route), implies, related collapsed in `details.fold`. Rows inside each section are sorted by review priority, **never by edge**.
4. Row columns:
   - Pair (`#id` + "round N")
   - Contracts
   - Relationship: badge, then "method · basis 0.38 moderate"
   - Confidence (`conf`)
   - **Checks**: `ul.checks` with ✓/✕/◇/? for date, strike, direction and source, then the existing VetoPhrase link to `#diff`
   - **Market**: net gap and freshness badge
   - Route
5. Flagged row: `tr.flagged` plus `.b-review` and the sentence "Likely a wrong pairing, not an arbitrage."
6. A checks key under the tables.
7. At 390px, rows become cards with the full-width Route button last.

**VM:**
- `BasisRisk` and its word per row
- `Checks[4]{Glyph, Class, Word, SR}`, derived from the stored DiffRows: deadline → date, threshold → strike, comparator → direction, source → source. Status match → ✓, differ → ✕, differ + egf → ◇, unknown → ?.
- `ReviewPriority` = flagged first, then routable pairs by (confidence − 0.90) ascending, then veto count descending
- `MaxQuoteAge` per pair
- `sort`, `min` and `q` handler params

**◆ NEW DATA:**
- net/apparent gap per pair (fee-adjusted price difference)
- the review flag and Flagged count that depend on it
- the per-venue fee estimates quoted in the key

Ship without the gap line and the flag until these exist.

## Pair detail → `pair.html` + `history.html` (Pair v2)

Order:
1. crumbs + h1 + question
2. **verdict panel** (`.verdict-panel`):
   - large badge + TypeLabel
   - Confidence, Method, Basis risk (with an "N diff rows" link to `#basis`), Routable ✓/no, Round + as of
   - the `.NearMiss` sentence when set
3. `details.types`, which is now the badge legend
4. the two contracts:
   - venue/title header, ID/Market/Event `dl`, category · status · close, quality flags
   - **rule layers** in `.layers` as a `details[name="layers-A"]` group: Title, Summary ◆, Full rules (open), Clarifications ◆
5. **Contract diff** as `table.terms`, one `tbody` per rule part, in this order:
   - Resolution criteria (subject, metric)
   - Strike / threshold
   - Direction (comparator)
   - Timeframe / cutoff (deadline)
   - Sources
   - Qualifying / edge cases (edge rules)

   Columns: Term · A · B · Verdict (badge + Annotation + `a.basis-link`) · Contribution ◆. Rows are edged by `tr.t-match|t-differ|t-closed|t-unknown`. Word-level `<del>`/`<ins>` show inside values. The intro sentence is unchanged.
6. Vetoes panel: the existing VetoHeading, sentence and table, or "No vetoes."
7. Facts and Basis risk side by side. The basis table gains a "Diff row" column.
8. Why: the explanation quote, then the LLM rationale `details` ◆
9. Current touch: the 8 columns kept; Fetched gains the freshness badge
10. Price history: `history.html` unchanged
11. Decision-log entries, with the kind as a badge
12. `#route`
13. Rail as in v1

**VM:**
- `DiffRow.Part` (field → rule part map) and `DiffRow.Class`
- `DiffRow.AHTML`/`BHTML`: a server-side word diff, emitted as `template.HTML` only from the escaped diff helper
- `BasisComponent.Row`
- diff counts line ("7 terms · 2 match · …")
- quote `Age`

**◆ NEW DATA:**
- Contribution column (prior, signed per-term weight, total → confidence). The matcher stores no weights.
- Summary and Clarifications rule layers. Only Title and Rules are ingested.
- LLM verifier rationale, run ID and timestamp

Hide all three until they exist.

## Route + result → `route.html`, `route_result.html`, `#route` in `pair.html` (Route v2)

- **Form:** all 7 fields plus the hidden pair, in one row at 1280 and stacked at 390. Hints sit in `<small>`.
- **Result** `article#route-result.trace`, as a cost report:
  1. Header meta: decision, round, as of, decided, plus a freshness badge for the oldest input and a permalink.
  2. Outcome badge `badge-l` + OutcomeSummary, then the Order line with partners.
  3. `dl.tiles`: Filled, **Avg all-in** (`.key`), Total cost, Fees, Basis penalty, Slippage vs quoted.
  4. **Split bar**: `svg.split` with one `<rect class="seg-N" x="…%" width="…%">` per leg plus `<text>`. These are SVG attributes, so the bar is CSP-safe. A partial fill adds `.seg-unfilled`. It carries an `aria-label` sentence.
  5. Chosen legs, `table.cost`: Venue, Instrument, Route, Filled, **Quoted**, VWAP (sim. fill), **Slippage**, + Fee, + Basis, = All-in (`.allin`). The totals row is kept, plus the basis multiplier note and the **sign rule** (`p.sign-rule`).
  6. Ranked alternatives: the chosen row first (`tr.chosen`, `b-chosen` badge), then the existing 10 columns. Gap is shown as `(0.71¢) · (130) bps`.
  7. `details` "How it was decided…": the Decision trace steps (below), collapsed here.
- **States** (all shown in the mock):
  - partial: split bar with the unfilled segment
  - not routed: "No venue can fill this order." in `strong.reason` and the step 2 `details` opened
  - validation errors: unchanged text in a danger panel

**VM:**
- `Leg.Quoted` (best price at the snapshot)
- `Leg.SlippageBps` and the weighted total
- `Leg.SharePct` and `Leg.X` for the split bar
- `TotalCost`, `TotalFees`, `TotalBasis`
- `Alt.GapBps`
- `MaxQuoteAge`

**◆ NEW DATA:** none.

## Decision trace → `decision.html` + `route_result.html` (Decision v2)

`decision.html` keeps the crumbs, h1 and the stored-simulation intro, and adds a question line. `route_result.html` renders `ol.trace-steps` (CSS counters draw the numbers and the rail). Each step is a `details`: open on `/decisions/{id}`, closed under the route form. Because one partial serves both pages, the two views cannot disagree.

1. **Order and input snapshot:** each candidate's venue, instrument, relationship badge, best ask, ask depth, and quote-age stamp.
2. **Eligibility:** "N of M candidates kept", then the existing Excluded table. The excluded title is in `<s class="excluded">` and the reason is `✕ {{.ReasonPhrase}}: {{.Detail}}`.
3. **Fill simulation and fee math:** one `dl.math` card per leg: VWAP over N levels + fee + basis (score × multiplier) = all-in.
4. **Ranked alternatives:** a proportional all-in comparison (an SVG bar per alternative), with Δ shown as `(¢) (bps)`. The full table is shared with the cost report.
5. **Decision:** the Warnings panel and the Explanation.

**VM:**
- `Snapshot[]{Venue, InstrumentID, Type, BestAsk, Depth, Age, FreshState}`
- `Leg.BasisScore`, `Leg.Levels`
- `KeptCount`/`CandidateCount`

**◆ NEW DATA:** none, *if* the router persists its snapshot inputs with each decision. UI-05 must confirm this. If they are not stored, step 1 needs them persisted at decision time.

## Near misses → `near_misses.html` (Near Misses v2)

- `.page-head`, then the strip: Count, Stopped by a veto (danger), and Fact did not close (new cell: Total − VetoCount). The definition paragraph and the truncation note (`p.note-warn`) are kept.
- Group chips (in-page anchors) and one `section` per failure reason, headed by the veto code with its count and a one-line description.
- Row columns:
  - **Veto that fired** (mono badge + `field: detail`)
  - **How close** (term gap)
  - Contracts (unclamped)
  - Explanation: the existing `details` "Why", plus a "Not-a-match record →" link

**VM:**
- `Groups[]{Code, Count, Rows}` (handler sort by first veto code; rows without a veto group under "fact did not close")
- `TermGap` from DiffRows (numeric threshold difference, deadline distance)
- `LogLink` (the verdict entry for the pair)

**◆ NEW DATA:** match score against the required score (the pre-veto score). Not stored.

## Eval → `eval.html` (Eval v2)

1. The h1 is kept, and a question line is added.
2. Head grid:
   - **Headline panel**: precision at equivalent, the verdict badge (`✓ meets the 0.95 target`, `✕ below …`, `? undefined`), exit-bar text and support
   - Next to it: the existing explanatory paragraph and the "Produced … by … on round … labelled pairs from … method …" line
3. **Quality by type**:
   - The existing 7 columns, plus **F1**. Precision and Recall get `meter` bars (`td.meter-cell`).
   - Then two rows: **Macro average** and **Weighted by support**.
4. **Confusion matrix** (`table.confusion` in `.confusion-wrap`): rows are labels and columns are predicted. Each cell is a link (`hx-get` to the pairs in it). Shading uses bucket classes `d1–d3` on the diagonal and `e1–e3` off it, never colour alone: the count is always printed and `aria-label` gives the sentence.
5. **P/R at confidence thresholds** (for equivalent, with the 0.90 floor row highlighted). The page states plainly that there is no calibration plot because confidence is a rules score, not a probability.
6. Notes from the run (kept).
7. States: Missing (`blocked` badge plus the 3-step `ol`, same words) and Broken (danger panel). "The summary has no per-type metrics." is kept.

**VM:**
- `F1` per row
- macro and weighted rows
- float P/R for the meters

**◆ NEW DATA:**
- confusion matrix counts and pair ids per (label, predicted)
- the `/eval/cell` drill-down
- threshold sweep
- golden-set version (labels file hash)
- previous run and Δ P per type
- rules vs rules+LLM columns (M3-06)

Until they exist, ship the headline, the type table and the notes.

## Decision log → `log.html` (Log v2)

- `.page-head` with a question line and the existing intro paragraph, in a `details` at 390.
- **Show** chips: `a.chip-filter[aria-current]` with `?kind=` for everything, routing, verdict, llm_verify and round. Keep a `<noscript>` submit and the GET form.
- **Group by** chips: none, venue, staleness, label ◆.
- "Latest N entries (at most L). Newest: …" (kept from v1).
- **Outliers** panel (`.outliers`): the worst slippage and the oldest input among the listed rows, each linked to its decision.
- Table when `kind=routing`:
  - When
  - About (Decision #id + pair link)
  - Order (side, size, type)
  - Outcome badge + venues
  - All-in
  - **Saving vs best alt** (positive is favourable, parentheses otherwise; the rule is printed under the table)
  - Max quote age (stamp)
  - Label at decision ◆
  - Summary (clamped, inside `details`)
- Every other kind keeps When / Kind (badge) / About (+ `AboutTitle`) / Summary.

**VM:**
- for routing entries, a join on the stored decision: pair, side, size, type, outcome, venues, average all-in, rank-2 gap, max quote age
- `group=venue|staleness`
- outliers
- `AboutTitle`

**◆ NEW DATA:** label at decision time (the pair's type when it was routed; decisions do not store it), and group-by label.

## Admin → `admin.html` (Admin v2)

Admin renders through `layout.html`, outside the public nav (`Page=""`), with the question "Is data flowing, which round is pinned, and what can the LLM verifier spend?".

1. The AB-15 banner (`.banner.banner-danger`, ✕) and the `.Message` flash (`role=status`).
2. **Health** `table.health`, one row per component: Collectors, Database, Matcher, Pinned round, LLM verifier.
   - Status = **worst signal** (degraded > warning > healthy). Any unknown signal with nothing worse makes the row **Unknown (gray)**, never Healthy.
   - Signals sit in `ul.signals` with ✓/!/✕/?.
   - The Collectors row has **recent-round bars** (`svg.bars`). Full height is ok, ¾ partial, ½ failed, an outline missing, so the state does not rely on colour. Each bar has a `<title>` and the strip has an `aria-label` summary.
   - The freshness windows are written under the table.
3. Two columns, with every sentence verbatim (tests assert MEETS/BELOW TARGET):
   - Collector success: the sentence plus the count chips ok / partial / failed / running / missing ticks, and the observed/scheduled line
   - Database: the sentence plus a size bar against the threshold (`meter`)
4. **Actions**: Ingest, Unpin, LLM Verification. Each `header` holds the state badge; forms, button labels, small text and disabled states are unchanged.
5. **LLM verifier runs** table ◆.
6. **Data notes** (`.data-notes`): static known gaps (no Polymarket venue time, pinned rounds not judged for freshness, approximate fees).
7. **Last N rounds**: the 10 columns and rowspans kept, status as a badge, pinned marker kept, empty state kept.

**VM:**
- `Health[]{Name, Badge, Signals[]{Glyph, Class, Text}}`, computed in `admin.go` from Collector, DB, the latest round, the pinned round and the LLM state
- `Bars[]` from the rounds query (last 24)

**◆ NEW DATA:**
- matcher health (last match run time and duration per round). Until then the Matcher row is Unknown.
- freshness warn window
- 90-day daily history with gray no-data days
- LLM verifier per-run history

## Error → `error.html` (Error v2)

`.error-page`: a danger badge `✕ {{.Status}}` (mono), `h1 {{.Status}} {{.Title}}`, `{{.Message}}`, then "Back to the matches page" as a button. It renders inside the layout, so banners and the stamp still show. No new data.

## ◆ Needs new data: full list

| # | Item | Page(s) | What the server would need |
|---|---|---|---|
| 1 | Net/apparent gap per pair | Matches | Fee-adjusted best-price difference per pair per round |
| 2 | Review flag and Flagged count | Matches | #1 plus a large-edge threshold |
| 3 | Venue fee estimates | Matches key | Documented fee schedule per venue |
| 4 | Freshness **Warn** state | All (stamp), Admin | A second, earlier window per source (config) |
| 5 | Per-term contribution (prior, weights, total) | Pair | Matcher stores a weight per compared term |
| 6 | Rules summary and clarifications layers | Pair | Ingest the venues' summary and clarification text |
| 7 | LLM verifier rationale + run id | Pair | Persist the verifier output per pair |
| 8 | Pre-veto match score vs required | Near misses | Matcher stores the score before vetoes |
| 9 | Confusion matrix + cell drill-down | Eval | `eval_summary.json` per-(label, predicted) counts and pair ids; `/eval/cell` |
| 10 | Threshold sweep | Eval | P/R/TP/FP at confidence cut-offs |
| 11 | Golden-set version, previous run, Δ | Eval | Labels file hash; keep the previous summary |
| 12 | rules vs rules+LLM columns | Eval | M3-06 eval output |
| 13 | Label at decision time, group by label | Log | Store the pair type on each decision |
| 14 | Matcher health | Admin | Record a match run (time, duration, round) |
| 15 | 90-day daily history | Admin | Daily rollup of round outcomes |
| 16 | LLM verifier runs table | Admin | Persist each run's summary |
| (17) | Decision snapshot inputs | Decision | Only needed if not already persisted with the decision; confirm in UI-05 |

## Nothing dropped: audit

Every item v1 renders still renders:
- **Layout:** demo, no-data and stale banners (same text); nav; brand and tagline; as-of, round and pinned; question; timezone form; footer.
- **Matches:**
  - no-round and no-relationships messages
  - every strip cell, including truncation
  - per row: id, round, both venues and titles, type, confidence and word, method, veto phrase, Route
  - implies label; related count and label, collapsed
- **Pair:**
  - every strip item, TypeLabel, near-miss sentence, types list
  - both instrument cards: IDs, category, status, close, rules, quality flags, the missing state
  - diff with annotations and the "no diff stored" row
  - veto heading, sentence and table, or "No vetoes."
  - facts table or the "None" sentence
  - basis heading, paragraph and components, or the not-scored sentence
  - explanation
  - touch table: 8 columns and the no-quote row
  - history: summary, chart, legend, data points
  - log entries and the full-log link
  - route form: 7 fields plus the hidden pair
- **Route result:**
  - errors; decision meta; outcome and summary; order and partners
  - legs (8 columns plus totals); multiplier note; no-fill message
  - alternatives (10 columns, rowspans); exclusions; warnings; explanation
- **Near misses:** both strip cells, definition, truncation, veto code, field and detail, the no-veto branch, contracts, Why.
- **Log:** intro, kind filter, empty state, count line, When / Kind / About / Summary.
- **Eval:**
  - intro and Produced line; precision strip and verdict
  - the 7 table columns and "no per-type metrics"
  - notes; Missing and Broken states
- **Admin:**
  - collector, DB and pinned values; AB-15 banner and detail
  - collector sentence and counts; message
  - the three actions with all states
  - rounds table, all columns and the empty state
- **Error:** status, title, message, link.
