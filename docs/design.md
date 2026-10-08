<!-- OWNER: shared. -->
# UI design spec (UI-01, D-081)

A clarity pass over the v0 pages, not a redesign. The stack stays `html/template` + htmx + Pico.css with no JS build and no SPA (D-031, D-081). Everything here is about faster comprehension for the personas in [requirements.md](requirements.md): P1 judging rigour, P2 trusting a routing decision, P3 keeping the admin page legible. The reviewer checks every UI MR against this document and its ticket (UI-02 onward in [tickets/M3.md](tickets/M3.md)).

Grounding: [brief.md](brief.md), [plan.md](plan.md) (each milestone's demoable outcome), [domain.md](domain.md), FR-21 to FR-25 and US-01 to US-11, the v0 templates under `internal/web/templates/`, and a walk of the running app on 2026-10-08 (seeded fixture round, 2453 instruments, 1 `equivalent_given_facts`, 1 `complement`, 25 `implies`, 973 `related`). Screenshots at 1280 px and 390 px are attached to the UI-01 MR, not committed.

**The demo path, in order.** Matches grouped by relationship type → pair contract diff (terms, vetoes, facts used, basis risk) → route form → decision trace (eligibility, VWAP, fees, basis penalty, chosen venue and why) → decision log → eval. Every choice below is judged by whether it shortens that path or makes a step on it more legible.

## 1. What the walk found

| Page | What gets in the way today |
|---|---|
| `/` matches | 8-column table, 1000 rows; the 2 routable pairs sit above 973 `related` rows; titles repeat 60 characters per cell; "As of" repeated on every row; confidence is a bare number; type is coloured text only. At 390 px the table collapses to one word per line. |
| `/pairs/{id}` | The verdict is in the `h1`, but the diff (the demo moment) is the fourth section; for `equivalent_given_facts` the page says "Vetoes (3): each veto on its own blocks equivalent" above a verdict that says the pair *is* equivalent, which reads as a contradiction until the explanation prose is read. The route form is at the bottom with no pointer to it. |
| `POST /route` trace | The outcome is a `<mark>` line; the excluded-candidates table overflows at 1280 px because of 78-character Polymarket IDs; the explanation repeats those IDs; the only hint of basis risk is a v0 warning. |
| `/near-misses` | The veto is in column 5 of 6; the reason a reviewer wants first is last. |
| `/log` | Fine; summaries are long and the "about" link is a bare pair number. |
| `/eval` | Fine; the empty state does not say what unblocks it (Q-7). |
| `/admin` | Unstyled standalone HTML, `border="1"` table, no header or as-of line. |
| All pages | The round and time sit inside the nav and wrap; the timezone is UTC by text only; no stale or demo banner yet (M3-01 added `StaleWarning`, M3-07 adds demo mode). |

## 2. Page jobs, per persona

| Page | P1 reviewer | P2 trader / ops | P3 admin |
|---|---|---|---|
| Matches `/` | Judge coverage and rigour in one screen: how many pairs per type, which are routable. | Find a routable pair and get to its route in one click. | — |
| Near misses | See what was rejected and which veto did it. | — | — |
| Pair `/pairs/{id}` | See exactly why the verdict is what it is, term by term, and what facts carried it. | Check both books, basis risk, and route an order. | — |
| Route trace | Check the trace is complete: eligibility, cost terms, alternatives. | Trust the chosen venue without reading code. | — |
| Decision log | Audit every verdict and order. | Find a past order. | — |
| Eval | Read measured precision per type. | — | — |
| Admin | — | — | See data health, the pinned round, and the LLM verifier state at a glance. |

## 3. Information hierarchy, per page

What is read first, second, third. "Strip" means a one-line summary block directly under the `h1`.

| Page | First | Second | Third |
|---|---|---|---|
| Matches | Strip: round, as-of + timezone, pairs kept, **routable pairs** (count, link). | The routable groups (`equivalent`, `equivalent_given_facts`, `complement`) as full rows with a **Route** link per row. | `implies`, then `related` collapsed behind a `details` with its count; truncation notice. |
| Near misses | Strip: count, how many stopped by a veto. | Rows led by the **veto that fired** (code + term + detail), then the two titles. | Explanation prose, collapsed per row. |
| Pair | Verdict strip: type chip, confidence, method, basis-risk score, "routable: yes/no", as-of. | **Contract diff** with vetoes and facts inline, then basis-risk components. | Explanation prose, current touch, price history, decision-log entries, route form. |
| Route trace | Outcome strip: chosen venue(s), filled quantity, all-in per contract, or "not routed" with the one reason. | **Per-leg cost table**: VWAP + fee + basis penalty = all-in, with the basis score and multiplier; ranked alternatives with the gap. | Exclusions (reason as text), warnings, explanation prose, the decision-log link. |
| Decision log | Kind filter and the latest entry's time. | Rows: when, kind chip, **about** (pair or decision with its title), summary clamped to two lines. | Full summary on expand. |
| Eval | Headline: precision at `equivalent` with a text verdict against the 0.95 target. | Per-type table; when M3-06 lands, rules vs rules+LLM side by side with the delta. | Notes from the run; the empty state names Q-7 and the command. |
| Admin | Health strip: collector success rate vs target, DB size vs threshold, pinned round. | Actions: ingest, unpin (M3-07), LLM verify (M3-06) with their state. | Rounds table. |

## 4. How states are shown

Text plus colour, never colour alone. Every state has a visible word; colour is a second channel. The "as of" round time and its timezone appear on every page in the header strip (section 5), in the selected timezone once M3-08 lands (default ET per US-10; UTC until then).

| State | Text | Token | Where |
|---|---|---|---|
| Relationship type | the type name in a chip, e.g. `equivalent_given_facts`; routable types also get the word "routable" in the strip | `--eq-ok` for `equivalent`, `equivalent_given_facts`, `complement`; `--eq-info` for `implies`; `--eq-warn` for `related`; `--eq-muted` for `unrelated` | matches rows, pair strip, eval rows, log rows |
| Confidence | the number to two decimals plus a bucket word: "high" ≥ 0.90 (the `MIN_ROUTE_CONFIDENCE` floor), "medium" 0.70 to 0.89, "low" below | number in body colour; the bucket word in `--eq-ok` / `--eq-warn` / `--eq-muted` | matches, pair strip |
| Veto fired | "veto: `threshold_mismatch` on threshold (4.25 vs 0.5)"; on an `equivalent_given_facts` pair the heading reads "3 term differences, closed by facts" | `--eq-danger` for a veto that stands; `--eq-info` when facts closed it | diff rows, near-miss rows, pair |
| Unknown term | the word "unknown" in italics; the diff row status reads "unknown" | `--eq-muted` | diff |
| Match / differ | "match" / "differ" | `--eq-ok` / `--eq-danger` | diff rows |
| Facts used | "fact: `fed_upper_bound` = 4.00, federalreserve.gov, as of 2026-09-16" | `--eq-info` | pair, diff |
| Basis risk | score to two decimals plus a word: "low" < 0.20, "moderate" 0.20 to 0.49, "high" ≥ 0.50; "not scored" when absent | `--eq-ok` / `--eq-warn` / `--eq-danger` / `--eq-muted` | pair strip, route legs |
| Staleness | "as of 01:35:52 UTC, 3 min ago"; stale-round banner: "Stale data: the latest round is 42 min old (warn after 10 min)" (D-077) | `--eq-warn` banner | header, every page |
| Demo mode (M3-07) | banner: "Demo mode: data as of <time> (round <id>). Unpin from /admin" | `--eq-info` banner | header, every page |
| Quote eligibility | reason as a phrase, not a code alone: "stale: fetched 140 s before the snapshot", "skew: 31 s behind kalshi", "no book", "too shallow: 12 contracts available", "not routable: related pairs are never routed", "low confidence: 0.87 is below the 0.9 floor" | `--eq-danger` reason word, body colour detail | route exclusions |
| Skew warning, approximate fees | listed under "Warnings" with the same phrasing | `--eq-warn` | route trace |
| Routing outcome | "Routed: buy 100 YES on kalshi at 0.5312 all-in"; split: "Routed across 2 venues: 60 + 40 contracts"; partial (IOC): "Partially routed: 80 of 100 contracts"; complement: "Routed as NO on polymarket (complement of the target)"; none: "Not routed: every candidate was excluded" | `--eq-ok` / `--eq-warn` (partial) / `--eq-danger` (not routed) | route strip, decision page |
| Eval pass / fail | "meets the 0.95 target" / "below the 0.95 target" / "undefined: no equivalent predictions" | `--eq-ok` / `--eq-danger` / `--eq-muted` | eval |
| LLM verifier (M3-06) | "disabled: ANTHROPIC_API_KEY not set", "ready: 12 of 200 calls used today", "cap reached: 200 of 200", "last run: 14 verified, 2 downgraded, 9 cache hits" | `--eq-muted` / `--eq-ok` / `--eq-danger` / `--eq-info` | admin |
| Rate limited (M3-09) | the JSON body stays as shipped; the route form's htmx config does not swap on 4xx, so the full-page fallback shows the error text | — | route form |

## 5. Tokens and shared components (UI-02)

A small set of CSS custom properties layered over Pico in `internal/web/static/app.css`. Pico supplies type, spacing and the light/dark switch; we add only semantic colour, one data-density step, and a few components. No new fonts, no new CDN assets, no inline styles (CSP). Dark mode follows Pico's `prefers-color-scheme` and `[data-theme]`; every token has a dark value chosen for ≥ 4.5:1 against Pico's dark background (`#13171f`) and ≥ 4.5:1 against its light background (`#fff`) for text use. Backgrounds for chips and banners are tints with the full-strength colour as text or border, so the text label always carries the contrast.

```css
:root {
  --eq-ok: #1b6e3a;      /* routable, match, pass */
  --eq-info: #1a56a8;    /* implies, facts, demo mode */
  --eq-warn: #8a5a00;    /* related, stale, partial, approximate */
  --eq-danger: #b3261e;  /* veto, excluded, fail, not routed */
  --eq-muted: var(--pico-muted-color); /* unknown, unrelated, not scored */
  --eq-ok-bg: #e6f4ea; --eq-info-bg: #e8f0fb; --eq-warn-bg: #fff3d6; --eq-danger-bg: #fbe9e7;
  --eq-space-1: 0.25rem; --eq-space-2: 0.5rem; --eq-space-3: 1rem; --eq-space-4: 2rem;
  --eq-text-s: 0.875rem; --eq-text-m: 1rem; --eq-text-l: 1.25rem; --eq-text-xl: 1.75rem;
  --eq-table-pad: 0.4rem;
}
@media (prefers-color-scheme: dark) { :root:not([data-theme=light]) {
  --eq-ok: #6fd08c; --eq-info: #8ab4f8; --eq-warn: #f2c14e; --eq-danger: #ff8a80;
  --eq-ok-bg: #12301c; --eq-info-bg: #142338; --eq-warn-bg: #3a2c05; --eq-danger-bg: #3b1512;
} }
[data-theme=dark] { /* same values as the dark block */ }
```

Components, each a class in `app.css` and a template define in `layout.html`:

- **`.strip`**: the one-line summary under an `h1`, a `dl` of label/value pairs laid out inline (`dl.strip > div { display:inline-block }`), wrapping at 390 px.
- **`.chip`** with `.chip-ok | .chip-info | .chip-warn | .chip-danger | .chip-muted`: the state word with its tint; used for types, outcomes, buckets. Never a chip without text.
- **`.banner`** with the same variants and `role="status"` (stale, demo mode) or `role="alert"` (errors): full-width line under the nav.
- **`.asof`**: the header line "Data as of 2026-10-08 01:35 UTC (round 1, 3 min ago)"; M3-08's timezone selector sits at its right end.
- **`table.data`**: compact padding (`--eq-table-pad`), numeric columns right-aligned (`td.num`), IDs in `code` with `overflow-wrap:anywhere` so nothing overflows at 1280 px.
- **Stacked rows under 640 px**: `table.data.stack` turns each row into a block and each cell into "label: value" using `td::before { content: attr(data-label) }`; templates add `data-label` to every `td`. CSS only, no JS. Column order on the narrow layout is the hierarchy in section 3.
- **`.clamp-2`**: two-line clamp for titles and summaries (`-webkit-line-clamp`), with the full text in `title`; the pair page never clamps.
- **Focus**: Pico's `:focus-visible` outline kept; chips that are links get the same outline.
- **Reduced motion**: no motion is added anywhere; htmx swaps are instant.

## 6. Before and after

### Matches `/`

Before (1280 px): nav with round text wrapping; question line; `h1`; one paragraph; `<mark>` truncation; then per type an `h2` and an 8-column table (`Pair | A | B | Type | Confidence | Method | Vetoes | As of`) down to 1000 rows. At 390 px every cell is a column of single words.

After:

```
Equinox  Matches  Near misses  Decision log  Eval        Data as of 2026-10-08 01:35 UTC (round 1, 3 min ago) [tz]
Which Kalshi and Polymarket contracts are the same bet, and how sure is Equinox?
Matches
[round 1 | 1000 pairs kept | routable: 2 | implies: 25 | related: 973 | showing first 1000]
Routable pairs (2)                                   ← the h2 says "routable"; the chip says the type
#856  KALSHI  Will the upper bound … Oct 28 …        [equivalent_given_facts] 0.93 high  rules  3 closed by facts  Route
      POLYMARKET  Will the Fed increase … 50+ bps …
#840  KALSHI  …                                      [complement] 0.91 high  rules  3 closed by facts  Route
implies (25)   One resolving YES forces the other YES.
#824 …                                               [implies] 0.87 medium  rules  3 vetoes
▸ related (973): same event, different payout; never routed. Show
```

Changes: the strip replaces the paragraph; the two titles stack in one cell (venue in small caps, title clamped to two lines, round shown as text per US-01); the type is a chip; confidence gets a bucket word; the vetoes column becomes a phrase ("3 vetoes" linking to `#diff` on the pair, or "closed by facts"); a **Route** link goes to `/pairs/{id}#route`, so the landing page reaches the diff in one click and the trace in two (NFR-16); `related` is still listed (US-01) but collapsed; "As of" leaves the rows and lives in the header. Under 640 px the rows stack: titles, chip, confidence, Route.

### Pair `/pairs/{id}`

Before: `h1` with the type coloured; a `details` of all types; two contract cards; "Why: the explanation" prose; "Vetoes (3)" table with "each veto on its own blocks equivalent"; facts table; basis table; log entries; then "Contract diff"; touch; history; route form.

After:

```
Pair #856
[equivalent_given_facts] confidence 0.93 high | method rules | basis risk 0.38 moderate | routable: yes | round 1, as of 01:35 UTC    Route an order ↓(link to #route)
Same bet once a documented external fact is applied.  ▸ all relationship types
The two contracts            (cards as today; IDs wrap)
Contract diff                (moved up; this is the demo moment)
Term         Kalshi                     Polymarket                 Status
subject      fed_funds_rate             fed_funds_rate             [match]
metric       rate_upper_bound           rate_change                [differ] veto metric_mismatch, closed by fact fed_upper_bound=4.00
comparator   gt                         gte                        [differ] veto comparator_mismatch, closed by fact fed_rate_step=0.25
threshold    4.25                       0.5                        [differ] veto threshold_mismatch, closed by facts
deadline     2026-10-28 18:05 UTC       2026-10-29 03:59 UTC       [match] 9h54m apart, within 24 h
source       federalreserve_gov         federalreserve_gov         [match]
edge rules   unknown                    unknown                    [unknown] not compared
Facts used (3): fed_upper_bound = 4.00 (federalreserve.gov, as of 2026-09-16) …
Basis risk 0.38 moderate: components table
Why (prose)  ·  Current touch  ·  Price history  ·  Decision-log entries
Route a hypothetical order   (form as today, anchored #route)
```

Changes: a verdict strip; the diff directly after the contracts with veto and fact annotations on the row they belong to (the veto table and facts table stay as the source of truth below the diff, the row annotation is a short phrase); the vetoes heading depends on the verdict ("3 term differences, closed by facts" vs "Vetoes (3): each blocks equivalent"); "routable: yes/no" in words; "Route an order" link in the strip. Nothing is removed.

### Route trace `POST /route`, `/decisions/{id}`

Before: `article` header with decision id, round, target code and time; "As of"; order line; "Chosen route" table or a `<mark>`; alternatives; exclusions table (overflows); warnings; explanation prose.

After:

```
Decision #1 · round 1 · as of 01:35:52 UTC · decided 01:37:46 UTC
[Routed] buy 100 YES on KALSHI KXFED-26OCT-T4.25 at 0.5312 all-in per $1 payout, filled 100 of 100
   or  [Partially routed] 80 of 100 …   or  [Not routed] every candidate was excluded (3 stale, 2 not routable)
Order: buy 100 YES, market IOC. Partners considered: 5.
Chosen legs
Venue       Instrument              Route                 Filled   VWAP    Fee     Basis   All-in
KALSHI      KXFED-26OCT-T4.25       direct                60       0.5200  0.0112  0.0000  0.5312
POLYMARKET  Will the Fed … (id…)    complement: NO on B   40       0.5300  0.0100  0.0038  0.5438
Total 100 contracts, average all-in 0.5362; basis penalty = score × 0.01 per contract.
Ranked alternatives (rank, venue, filled, VWAP, fee, basis, all-in, gap to chosen, note)
Excluded (venue, instrument, reason phrase)
Warnings · Explanation (prose, instruments named by venue + short title; full IDs in the tables)
```

Changes: an outcome strip in words; the leg table shows all four cost terms (M3-02) and totals, a `Route` column that reads "direct" or "complement: NO on B" (M3-04), one row per leg of a sweep (M3-03) with a totals line and the shortfall for IOC; exclusion reasons are phrases; instrument cells carry the venue, a short title and the full ID in wrapping `code`. The explanation prose keeps its sentences but the template renders instrument names from the view-model where it has them.

## 7. Designing for in-flight and upcoming M3 work

| Work | State | What the design does with it |
|---|---|---|
| M3-01 stale-round warning and snapshot "as of" (merged, D-077) | `StaleWarning` already rendered as `<mark>` in the layout | Becomes the `--eq-warn` banner with the phrasing in section 4; the snapshot line moves into `.asof`. |
| M3-02 basis penalty in the trace (MR !40) | adds `BasisPenaltyPerContract` per leg and the multiplier sentence | The leg table gets the Basis column and a totals line; the pair strip's basis-risk word matches the penalty's inputs. |
| M3-03 sweep legs | not started | Multiple rows under "Chosen legs", a totals line, and the partial-fill outcome strip. |
| M3-04 complement routes | not started | The `Route` column reads "complement: NO on B"; the outcome strip says "routed as NO on … (complement of the target)". |
| M3-06 LLM verifier on `/admin` (MR !38) | adds a POST form and a JSON summary | Admin shows the four verifier states from section 4 and the last run's summary as text; `method` chips read `rules+llm` where applicable; eval shows the delta. |
| M3-07 demo mode banner and `/admin` unpin | not started; touches `layout.html`, all templates, `admin.go`, `app.css` | The banner is the `.banner.chip-info` component; UI-02 is sequenced after M3-07 and inherits its markup rather than racing it. |
| M3-08 timezone selector | not started; touches `layout.html`, all templates | The selector sits in `.asof`; all times render through its `formatTime`. UI-02 is sequenced after M3-08. |
| M3-09 rate limiting (merged) | JSON 429 with `Retry-After` | Unchanged; documented in section 4. |
| M3-10 `docs/routing.md` | not started | The trace vocabulary here (reason phrases, cost terms) is what that doc should use. |

## 8. Cut order and freeze

From the brief: this track never blocks or delays an M2 or M3 exit criterion. Must-ship: UI-02 (layout and tokens), UI-03 (matches), UI-04 (pair), UI-05 (route trace). Cut, in this order, if the schedule is tight: UI-08 (admin), UI-07 (eval), UI-06 (near misses and log). Each cut is a Tier 2 entry. Code freeze is Sunday 2026-10-11 09:00 CT; after it only demo-critical UI fixes go in.

## 9. What was proposed and not built

Recorded in [status.md](status.md) under "Open questions" (Q-9): the frontend-design plugin's method asks for a distinctive typeface and palette. A new web font would be a new CDN asset (Tier 2, D-082 reserved) and is polish the brief does not grade (D-031); the recommendation is to keep Pico's system font stack. The palette above is semantic, not decorative.

## 10. The UI-02 onward split

Tickets live in [tickets/M3.md](tickets/M3.md) as wave 9. Summary of ownership so no two in-flight tickets share a file:

| Ticket | Owns | After |
|---|---|---|
| UI-02 layout, tokens, shared components | `internal/web/templates/layout.html`, `internal/web/static/app.css`, `internal/web/templates_test.go`, `internal/web/layout_test.go` (new) | M3-07, M3-08 |
| UI-03 matches | `templates/matches.html`, `handlers_matches.go`, `handlers_matches_test.go` | UI-02 |
| UI-04 pair | `templates/pair.html`, `handlers_pairs.go`, `handlers_pairs_test.go` | UI-02 |
| UI-05 route trace | `templates/route.html`, `templates/route_result.html`, `templates/decision.html`, `handlers_route.go`, `handlers_route_test.go` | UI-02, M3-02, M3-03, M3-04 |
| UI-06 near misses and decision log | `templates/near_misses.html`, `templates/log.html`, `handlers_nearmisses.go`, `handlers_log.go`, their tests | UI-02 |
| UI-07 eval | `templates/eval.html`, `handlers_eval.go`, its test | UI-02, M3-06 |
| UI-08 admin | `templates/admin.html`, `admin.go`, `admin_test.go` | UI-02, M3-06, M3-07 |
