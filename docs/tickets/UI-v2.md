<!-- OWNER: operator. -->
# UI-v2 tickets: build the Claude Design v2 UI (UI-09, D-098)

Paawan approved the Claude Design v2 UI on 2026-10-08 at 1:41 PM CT, together with three calls ([D-098](../decisions.md)). The spec is [design.md § 0](../design.md#0-claude-design-v2-ui-09-supersedes-the-layouts) plus the build spec [design/claude-design-v2/HANDOFF.md](../design/claude-design-v2/HANDOFF.md) and the v2 mocks beside it ([README](../design/claude-design-v2/README.md)). Read [CLAUDE.md](../../CLAUDE.md) first. These tickets follow the wave 9 format in [M3.md](M3.md) and extend that track. UI-02..UI-08 are merged.

- **Page tickets.** UI-10 is the shared design system and goes first. UI-11..UI-17 are one per page or page group.
- **Backend tickets.** UI-18..UI-26 cover the 17 "◆ Needs new data" items in `HANDOFF.md` and wire each one into its page.

## Dependency order

| Wave | Tickets | Starts when |
|---|---|---|
| A | **UI-10** design system (first); **UI-25** router stores inputs per decision (backend only, no shared files with UI-10) | now (after this docs MR merges) |
| B | UI-11 matches, UI-12 pair, UI-14 near misses, UI-16 eval, UI-17 admin | UI-10 merged |
| B | UI-13 route + decision trace, UI-15 decision log | UI-10 **and** UI-25 merged |
| C | UI-18 (after UI-11), UI-19 (after UI-17), UI-20 (after UI-12, UI-14 and #39/!58), UI-21 (after UI-12), UI-22 (after UI-12, UI-17), UI-23 (after UI-16), UI-26 (after UI-17) | listed deps merged |
| D | UI-24 | UI-23 merged |

**Order when slots are short.** Use at most 3 workers, all Sonnet. Run UI-10 first, then UI-25, then the demo path: UI-11, UI-12, UI-13, then UI-15, UI-14, UI-16, UI-17, then wave C in table order.

**File locks.** A ticket that is dependency-ready but shares a file with an in-flight ticket waits (operating-rules, the loop, step 2). Each ticket's "Shares files with" line names its overlaps.

| Ticket | Title | Kind | Tier | Depends on |
|---|---|---|---|---|
| UI-10 | Design system v2: app.css, badge/conf/stamp defines, header, error page | page (shared) | 1 | — |
| UI-11 | Matches page v2 | page | 1 | UI-10 |
| UI-12 | Pair detail v2 (incl. the `#route` form) | page | 1 | UI-10 |
| UI-13 | Route result + Decision trace v2 | page | 1 | UI-10, UI-25 |
| UI-14 | Near misses v2 | page | 1 | UI-10 |
| UI-15 | Decision log v2 ("Saving vs best ask") | page | 1 | UI-10, UI-25 |
| UI-16 | Eval page v2 | page | 1 | UI-10 |
| UI-17 | Admin page v2 (health rollup) | page | 1 | UI-10 |
| UI-18 | Net gap per pair, review flag, venue fee estimates (◆ 1–3) | new data | 2 (new config key) | UI-11 |
| UI-19 | Freshness Warn window per source (◆ 4) | new data | 2 (new config keys) | UI-10, UI-17 |
| UI-20 | Matcher evidence: per-term contribution + pre-veto score (◆ 5, 8) | new data | 2 (schema) | UI-12, UI-14, #39 |
| UI-21 | Rule layers: summary + clarifications (◆ 6) | new data | 2 (schema) | UI-12 |
| UI-22 | LLM verifier persistence: per-pair rationale + runs table (◆ 7, 16) | new data | 2 (schema) | UI-12, UI-17 |
| UI-23 | Eval artefacts: confusion matrix + drill-down, threshold sweep, golden-set version + Δ (◆ 9–11) | new data | 2 (eval contract, D-070) | UI-16 |
| UI-24 | Eval: rules vs rules+LLM columns (◆ 12) | new data | 2 (eval contract) | UI-23 |
| UI-25 | Router stores input quotes and label with each decision (◆ 13, 17; D-098 call 3) | new data | 2 (stored decision schema) | — |
| UI-26 | Admin health data: match runs + 90-day daily history (◆ 14, 15) | new data | 2 (schema) | UI-17 |

GitLab issues #42–#58 (UI-10 = #42 … UI-26 = #58) are listed at the end of this file.

---

## Rules shared by every UI-v2 ticket

- **Spec.** The spec for each ticket is its `HANDOFF.md` section and its v2 mock. The three D-098 calls win wherever the mock differs:
  1. Complement badges are blue (`--eq-info`).
  2. Favourable money differences read positive, unfavourable ones go in parentheses, e.g. `(0.71¢) · (130) bps`. The rule is printed under every table that uses it (`p.sign-rule`). The log column is "Saving vs best ask".
  3. The Decision trace reads the input quotes stored on the decision (UI-25).
  The Cursor reviewer checks each MR against design.md (§ 0 first), `HANDOFF.md` and the ticket.
- **Stack and security (unchanged from wave 9, M1-09 and the threat model).**
  - Go `html/template` + htmx + Pico.css. No JS build, no SPA, and no new dependency, font or CDN asset. Any of those is Tier 2 with its own D- entry. Pico and htmx stay pinned with SRI.
  - `html/template` escaping only, and no `template.HTML` on external data (AB-07). Word-level `<del>`/`<ins>` are emitted by the template around escaped tokens, never as pre-built HTML.
  - The CSP keeps no `'unsafe-inline'`: no inline scripts, `on*` handlers or `style` attributes. **The mocks' inline styles are never copied.** Use the classes in `handoff/app.css`. Bars are SVG attributes or `meter`.
  - These keep passing: `TestTemplatesHaveNoInlineScriptOrStyle`, `TestLayoutPinsCDNAssetsWithSRI`, `tests/security/ab07_*`, `TestStaleRoundWarning` and every test a UI-02..UI-08 AC names.
- **Nothing the current UI shows is dropped.** Each page ticket's MR description copies its page's bullets from `HANDOFF.md` "Nothing dropped: audit", ticks each one against the rendered page, and names the element that now shows it. Text that tests assert (banner text, MEETS/BELOW TARGET, "No vetoes.", empty states, chip text with the server's type string) stays verbatim.
- **Real screenshots, no fake ones.**
  - Before and after screenshots at **1280 px and 390 px** go in the MR. They are captured from the running app at the MR head: `make seed && make dev`, then a headless or real browser at that viewport width.
  - **No fake screenshots.** That means no Claude Design frames, no mock renders, no edited or composited images, and no HTML written only for the screenshot. The MR states the capture command and viewport.
  - Attach them to the MR rather than committing them, unless each is under 1 MB and the total is under 5 MB.
  - Stop the dev server afterwards and check nothing is left listening on the `make dev` port (docs/environment.md).
- **Checkable ACs (every ticket, unchanged from wave 9):**
  - keyboard reaches every link, control and `summary`, with a visible focus ring
  - every coloured state also has a text label and a glyph (design.md § 4)
  - WCAG AA (≥ 4.5:1) for text in light and dark mode
  - server-side p95 within NFR-01 (500 ms for pages, 300 ms for `POST /route`), quoted in the MR
  - `make check` green
- **◆ NEW DATA is never faked.** A page ticket ships with every ◆ element hidden, with no placeholder numbers and no "coming soon" stubs. The backend ticket that produces the data also renders it on the page. Sample numbers in the mocks are illustrative and never become fixtures or copy.
- **Tier 2 rules for backend tickets.** Any schema change counts: a new table, a new column, or a new field in a persisted JSON document (`routing_decisions.decision`, `relationships.diff`, `eval_summary.json`). So does a new config key or default (shared file `docs/environment.md`). Each one needs:
  - a `D-` entry in `docs/decisions.md`, numbered from the next free D- on main **and** in open MRs at push time
  - an **"Awaiting human veto"** row in `docs/status.md`
  - `docs/architecture.md` (data model) updated
  - a new `migrations/000N_*.sql` (the next free number at push time; `0001_init.sql` is never edited), with up and down tested in `internal/store/migrate_test.go`
  The D- entry and status row ship in the same MR. No waiting, unless the change touches a hard constraint, security or an abuse-case mitigation, or the equivalence definition. Then it is **2-critical**: stop and wait for Paawan's explicit approval.
- **Never:** `.gitlab-ci.yml` or any human-owned file (operating-rules.md); live venue or Anthropic API calls from tests; `ANTHROPIC_API_KEY` in a worker's env (workers are spawned with `env -u ANTHROPIC_API_KEY`).
- **File ownership.** Each ticket owns only the files it lists. `layout.html`, `app.css` and `viewmodel.go` belong to UI-10. Page tickets add view-model fields in their own handler files and new per-ticket test files, and edit only their named test functions in `pages_test.go` and `public_test.go`, as in wave 9. Every ticket appends to `docs/log.md`.

---
## UI-10: Design system v2: app.css, badge/conf/stamp defines, header, error page

**Context:** `HANDOFF.md` "Shared system", "Header" and "Error". The mocks are `Equinox Design System v2`, `Eq Header v2`, `Eq Badge`, `Eq Conf`, `Eq Fresh` and `Error v2`. Every page ticket builds on this one, so it goes first and stays backwards compatible: pages not yet migrated must look no worse.

**Satisfies:** FR-21, US-01 (round on every page), US-10 (timezone beside every as-of time), NFR-16

**Acceptance criteria**
- [ ] `internal/web/static/app.css` is replaced by `docs/design/claude-design-v2/handoff/app.css`, adjusted only where needed:
  - Complement badges are **blue**: `.b-complement` uses `--eq-info` (D-098 call 1).
  - Every class main's current `app.css` defines still exists. The MR lists the old selectors and shows none is missing, e.g. a sorted `grep -o` diff.
  - No `@import`, web font or remote `url()` is added.
- [ ] `layout.html` gains the three shared defines from `HANDOFF.md`:
  - `{{define "badge"}}`: the glyph is a span with `aria-hidden="true"` and the text is the server's string.
  - `{{define "conf"}}`: the number, the word bin and a `meter` with low .7, high .9, optimum 1. Bins are unchanged from design.md § 4.
  - `{{define "stamp"}}`: freshness badge, "as of" `<time datetime>` and age.
- [ ] `internal/web/viewmodel.go` adds `Badge{Class, Glyph, Text}` and `TypeBadge`, `FreshBadge`, `HealthBadge`, `VerdictBadge`, `OutcomeBadge`, plus `ConfidenceValue` and the stamp's `Age`/`FreshState`. The existing `ConfidenceWord`, `BasisRiskWord`, `TypeChipClass` and `OutcomePhrase` keep their behaviour, except that complement maps to the blue class. `viewmodel_test.go` covers:
  - every relationship type, with complement → blue
  - every freshness, health, verdict and outcome state
  - the confidence boundaries 0.69/0.70/0.89/0.90
- [ ] Freshness renders only Fresh, Stale, Unknown and Pinned in this ticket. Warn comes in UI-19. Stale uses the existing D-077 threshold.
- [ ] The header follows `Eq Header v2`:
  - The banners gain a glyph: demo `◆` info, no-data `?` warn, stale `✕` danger. Their text is unchanged ("Data is N minutes old.").
  - The context bar is the stamp plus "· round N[, pinned]", with the M3-08 timezone form at its right end.
  - The nav keeps `aria-current`.
  - The stamp is static. `GET /stamp` and the 15 s htmx refresh are out of scope, because a new public route would need its own ticket.
- [ ] `error.html` follows `Error v2`: a danger badge `✕ {{.Status}}`, `h1 {{.Status}} {{.Title}}`, the message, and "Back to the matches page" as a button. It renders inside the layout.
- [ ] Contrast: the MR has a table of every token (light and dark) with its ratio against Pico's light and dark backgrounds, including the new neutrals and the blue complement badge. All text uses are ≥ 4.5:1.
- [ ] Nothing dropped (Layout and Error bullets of the audit): demo, no-data and stale banners with the same text; nav; brand and tagline; as-of, round and pinned; question; timezone form; footer; error status, title, message and link.
- [ ] `layout_test.go` covers the stamp, the banner glyphs with unchanged text, no `style` attribute or inline script, and both CDN assets with SRI. `TestLayoutNavLinksNewPages` and `TestStaticCSS` are updated here.
- [ ] Real before/after screenshots at 1280 px and 390 px of `/`, `/pairs/{id}` and an error page from `make seed && make dev`. No fake screenshots.

**Verification:** `go test ./internal/web/... ./tests/security/... -run 'Template|Layout|CSP|AB07|ViewModel|Stale' -v && make check`

**Dependencies:** none (UI-02..UI-08 merged)
**Shares files with:** UI-19 (`layout.html` stamp, `viewmodel.go`); nothing else in wave A or B
**Files likely touched:** `internal/web/static/app.css`, `internal/web/templates/layout.html`, `internal/web/templates/error.html`, `internal/web/viewmodel.go`, `internal/web/viewmodel_test.go`, `internal/web/layout_test.go`, `internal/web/templates_test.go` (only if an assertion must move), `internal/web/banner.go` (glyph field only), `internal/web/pages_test.go` (`TestLayoutNavLinksNewPages` only), `internal/web/public_test.go` (`TestStaticCSS` only), `docs/log.md`

---

## UI-11: Matches page v2

**Context:** `HANDOFF.md` "Matches" and the `Matches v2` mock. P1 judges rigour and P2 finds a routable pair. Rows are sorted by review priority, never by edge.

**Satisfies:** FR-21, US-01, NFR-16

**Acceptance criteria**
- [ ] `.page-head` with the question, then the counter `dl.strip`: Round, As of, Pairs kept, Routable (`strip-key`), Implies, Related and the truncation cell. The "Flagged for review" cell stays hidden until UI-18.
- [ ] The toolbar is `form.toolbar-chips` (GET):
  - a Sort `select name=sort` with **Review priority** (default), Confidence and Pair id
  - removable `a.chip-filter.removable` chips for the active `min` and `q`
  - the review-priority rule written out
  Filter state lives in the URL and works without JS.
- [ ] Sections as before: Routable with Route, then implies, then related collapsed in `details.fold`. Rows are sorted by `ReviewPriority` (flagged first once UI-18 lands, then routable by confidence − 0.90 ascending, then veto count descending).
- [ ] Row columns:
  - Pair (`#id` + round) and Contracts
  - Relationship: badge, then "method · basis 0.38 moderate"
  - Confidence (`conf`)
  - **Checks**: `ul.checks` with ✓/✕/◇/? for date, strike, direction and source, derived from stored DiffRows (deadline, threshold, comparator, source), plus the VetoPhrase link to `#diff`
  - Market: freshness badge only (the net gap line comes in UI-18)
  - Route
  A checks key sits under the tables. At 390 px rows become cards with the full-width Route button last.
- [ ] Handler params: `sort`, `min` and `q` are validated (bad values fall back to defaults; AB-08). No new store queries beyond the round's existing data. `MaxQuoteAge` comes from stored quote times.
- [ ] Nothing dropped (Matches bullets of the audit):
  - the no-round and no-relationships messages
  - every strip cell, including truncation
  - per row: id, round, both venues and titles, type, confidence and word, method, veto phrase, Route
  - the implies label; the related count and label, collapsed
- [ ] `handlers_matches_test.go` covers the sort orders, the Checks mapping (match/differ/egf/unknown), `min`/`q` filtering with chips, and a complement row's blue badge. `TestPublicMatches*` and `TestGroupPairsOrdering` are updated here.
- [ ] Real before/after screenshots at 1280 px and 390 px. No fake screenshots. p95 for `GET /` is quoted.

**Verification:** `go test ./internal/web/... -run 'Matches|GroupPairs' -v && make check`

**Dependencies:** UI-10
**Shares files with:** UI-18
**Files likely touched:** `internal/web/templates/matches.html`, `internal/web/handlers_matches.go`, `internal/web/handlers_matches_test.go`, `internal/web/public_test.go` (`TestPublicMatches*`, `TestGroupPairsOrdering` only), `docs/log.md`

---

## UI-12: Pair detail v2 (incl. the `#route` form)

**Context:** `HANDOFF.md` "Pair detail" and the route-form part of "Route + result". The mocks are `Pair v2` and `Route v2` (form). The route form lives in `pair.html`, so this ticket owns the form markup and UI-13 owns the result.

**Satisfies:** FR-21, FR-25, US-02, US-07, NFR-16

**Acceptance criteria**
- [ ] Section order follows `HANDOFF.md`:
  1. crumbs, h1 and question
  2. `.verdict-panel`: large badge + TypeLabel; Confidence, Method, Basis risk with an "N diff rows" link to `#basis`, Routable ✓/no, Round + as of; the near-miss sentence
  3. `details.types` as the badge legend
  4. the two contract cards with the `.layers` `details` group, showing Title and Full rules only (Summary and Clarifications come in UI-21)
  5. the Contract diff
  6. Vetoes
  7. Facts and Basis side by side
  8. Why (explanation quote only; the LLM rationale comes in UI-22)
  9. Current touch
  10. Price history (`history.html` unchanged)
  11. Decision-log entries with the kind as a badge
  12. `#route`
- [ ] Contract diff as `table.terms`:
  - one `tbody` per rule part, in the `HANDOFF.md` order (via a `DiffRow.Part` map)
  - rows edged `t-match|t-differ|t-closed|t-unknown`, with the verdict as badge + annotation + `a.basis-link`
  - the diff counts line
  - word-level `<del>`/`<ins>` built from a server-side token list (`[]DiffToken{Op, Text}`) that the template wraps. **No `template.HTML`** (AB-07).
  - The Contribution column stays hidden until UI-20.
- [ ] The basis table gains a "Diff row" column (`BasisComponent.Row`). The Fetched column in the touch table gains the freshness badge (quote `Age`).
- [ ] The route form: all 7 fields plus the hidden pair, in one row at 1280 px and stacked at 390 px, with hints in `<small>`. The `hx-*` attributes and swap target are unchanged.
- [ ] Nothing dropped (Pair bullets of the audit):
  - every strip item, TypeLabel, near-miss sentence and types list
  - both instrument cards (IDs, category, status, close, rules, quality flags, the missing state)
  - the diff with annotations and the "no diff stored" row
  - the veto heading, sentence and table, or "No vetoes."
  - the facts table or "None"
  - the basis heading, paragraph and components, or the not-scored sentence
  - the explanation
  - the touch table: 8 columns and the no-quote row
  - history: summary, chart, legend and data points
  - log entries and the full-log link
  - the route form: 7 fields plus the hidden pair
- [ ] `handlers_pairs_test.go` covers the part grouping and order, the row classes, the escaping of a `<script>` token inside a word diff, the "N diff rows" link, and the section order. `TestPublicPairPage` and `TestPairPageShowsHistoryFactsBasisAndLog` are updated here.
- [ ] Real before/after screenshots at 1280 px and 390 px of an `equivalent_given_facts` pair and a `related` near miss. No fake screenshots. p95 for `GET /pairs/{id}` is quoted.

**Verification:** `go test ./internal/web/... ./tests/security/... -run 'Pair|AB07' -v && make check`

**Dependencies:** UI-10
**Shares files with:** UI-20, UI-21, UI-22 (`pair.html`, `handlers_pairs.go`)
**Files likely touched:** `internal/web/templates/pair.html`, `internal/web/templates/history.html` (no visible change expected), `internal/web/handlers_pairs.go`, `internal/web/history.go` (only if needed), `internal/web/handlers_pairs_test.go`, `internal/web/public_test.go` (`TestPublicPairPage` only), `internal/web/pages_test.go` (`TestPairPageShowsHistoryFactsBasisAndLog`, `TestBuildHistoryChart` only), `docs/log.md`

---

## UI-13: Route result + Decision trace v2

**Context:** `HANDOFF.md` "Route + result" and "Decision trace", and the `Route v2` and `Decision v2` mocks. One partial (`route_result.html`) serves `POST /route` and `/decisions/{id}`, so the two views cannot disagree. Step 1 reads the input quotes stored on the decision (D-098 call 3, UI-25).

**Satisfies:** FR-21, US-03, US-04, US-05

**Acceptance criteria**
- [ ] `article#route-result.trace` as a cost report:
  1. Header meta: decision, round, as of and decided, plus the freshness badge of the oldest input and a permalink.
  2. The outcome badge `badge-l` + OutcomeSummary, then the Order line with partners.
  3. `dl.tiles`: Filled, **Avg all-in** (`.key`), Total cost, Fees, Basis penalty, Slippage vs quoted.
  4. **Split bar**: `svg.split` with one `rect.seg-N` per leg (x and width as SVG attributes, CSP-safe), `.seg-unfilled` for a partial fill, and an `aria-label` sentence.
  5. `table.cost`: Venue, Instrument, Route, Filled, **Quoted**, VWAP, **Slippage**, + Fee, + Basis, = All-in. It keeps the totals row and the multiplier note and adds the printed **sign rule**.
  6. Ranked alternatives: the chosen row first (`tr.chosen`, `b-chosen`), then the existing 10 columns, with the gap as `(0.71¢) · (130) bps` per D-098 call 2.
  7. `details` "How it was decided…" with the trace steps, collapsed.
- [ ] `ol.trace-steps`, each step a `details` (open on `/decisions/{id}`, closed under the form):
  1. **Order and input snapshot**, from the decision's stored inputs: venue, instrument, relationship badge, best ask, ask depth, quote-age stamp. A decision stored before UI-25 says "Input quotes were not stored for decisions made before <date>." It does not re-query `quotes`.
  2. **Eligibility**: "N of M candidates kept", with the Excluded table, `<s class="excluded">` titles and `✕ {{.ReasonPhrase}}: {{.Detail}}`.
  3. **Fill simulation and fee math**: a `dl.math` card per leg (VWAP over N levels + fee + basis = all-in).
  4. **Ranked alternatives**: a proportional SVG bar per alternative, with Δ as `(¢) (bps)`.
  5. **Decision**: Warnings and Explanation.
- [ ] States, each tested and shown in screenshots:
  - routed
  - partially routed (unfilled segment)
  - not routed ("No venue can fill this order." in `strong.reason`, step 2 open)
  - validation errors (unchanged text in a danger panel)
  - a complement leg (blue badge)
  - a two-leg sweep
- [ ] VM fields in `handlers_route.go`: `Leg.Quoted`, `Leg.SlippageBps` + weighted total, `Leg.SharePct`/`X`, `TotalCost`/`TotalFees`/`TotalBasis`, `Alt.GapBps`, `MaxQuoteAge`, `Snapshot[]`, `Leg.BasisScore`/`Levels`, `KeptCount`/`CandidateCount`. These are computed from the stored decision only. No router changes in this ticket.
- [ ] Nothing dropped (Route result bullets of the audit):
  - errors; decision meta; outcome and summary; order and partners
  - legs (8 columns plus totals); the multiplier note; the no-fill message
  - alternatives (10 columns, rowspans); exclusions; warnings; explanation
- [ ] `handlers_route_test.go` gains a case per state above, plus the sign rule (a saving renders positive, a loss in parentheses) and the pre-UI-25 decision message. `TestDecisionPage` and `TestCountPartners` are updated here.
- [ ] Real before/after screenshots at 1280 px and 390 px of the routed, partial and not-routed traces and of `/decisions/{id}`. No fake screenshots. p95 for `POST /route` (≤ 300 ms) and `GET /decisions/{id}` is quoted.

**Verification:** `go test ./internal/web/... -run 'Route|Decision|CountPartners' -v && make check`

**Dependencies:** UI-10, UI-25
**Shares files with:** none in wave B (`pair.html` `#route` belongs to UI-12)
**Files likely touched:** `internal/web/templates/route.html`, `internal/web/templates/route_result.html`, `internal/web/templates/decision.html`, `internal/web/handlers_route.go`, `internal/web/handlers_route_test.go`, `internal/web/pages_test.go` (`TestDecisionPage`, `TestCountPartners` only), `docs/log.md`

---

## UI-14: Near misses v2

**Context:** `HANDOFF.md` "Near misses" and the `Near Misses v2` mock. This is the "why not" view: it leads with the veto that fired and groups rows by failure reason.

**Satisfies:** FR-21, US-01 (near-miss AC)

**Acceptance criteria**
- [ ] `.page-head` with the question, then the strip: Count, Stopped by a veto (danger), and the new cell "Fact did not close" (Total − VetoCount). The definition paragraph and the `p.note-warn` truncation note are kept.
- [ ] Group chips (in-page anchors), then one `section` per failure reason (`Groups[]{Code, Count, Rows}`), headed by the veto code, its count and a one-line description. Rows without a veto group under "fact did not close".
- [ ] Row columns:
  - **Veto that fired** (mono badge + `field: detail`)
  - **How close** (`TermGap` from DiffRows: numeric threshold difference, deadline distance; "—" when not numeric)
  - Contracts (unclamped)
  - Explanation: the `details` "Why" plus a "Not-a-match record →" link (`LogLink` to the pair's verdict entry)
  Rows stack at 390 px.
- [ ] The pre-veto score stays hidden until UI-20.
- [ ] Nothing dropped (Near misses bullets of the audit): both strip cells, definition, truncation, veto code, field and detail, the no-veto branch, contracts, Why.
- [ ] `handlers_nearmisses_test.go` covers the grouping, the count cells, `TermGap` for a threshold and a deadline, and the no-veto group. `TestNearMissesPage`, `TestNearMissesEmptyStates` and `TestNearMissRowsOrdering` are updated here.
- [ ] Real before/after screenshots at 1280 px and 390 px. No fake screenshots. p95 is quoted.

**Verification:** `go test ./internal/web/... -run 'NearMiss' -v && make check`

**Dependencies:** UI-10
**Shares files with:** UI-20 (`near_misses.html`, `handlers_nearmisses.go`)
**Files likely touched:** `internal/web/templates/near_misses.html`, `internal/web/handlers_nearmisses.go`, `internal/web/handlers_nearmisses_test.go`, `internal/web/pages_test.go` (`TestNearMiss*` only), `docs/log.md`

---

## UI-15: Decision log v2 ("Saving vs best ask")

**Context:** `HANDOFF.md` "Decision log" and the `Log v2` mock, modelled on a TCA report. D-098 call 2 renames the routing column to **"Saving vs best ask"** and applies the sign rule. The best ask comes from the input quotes UI-25 stores on each decision.

**Satisfies:** FR-17, FR-21, US-08 (audit every decision)

**Acceptance criteria**
- [ ] `.page-head` with the question and the existing intro paragraph. The intro sits in a `details` at 390 px.
- [ ] **Show** chips (`a.chip-filter[aria-current]`, `?kind=`) for all and for each kind the log actually stores (`relationship`, `routing`, `llm`, `ingest`), with readable words. No new kinds are invented. A `<noscript>` submit and the GET form stay.
- [ ] **Group by** chips: none, venue, staleness, and label (label uses the stored label from UI-25; older decisions group under "label not stored"). The "Latest N entries (at most L). Newest: …" line is kept.
- [ ] An **Outliers** panel (`.outliers`) shows the worst saving and the oldest input among the listed rows, each linked to its decision.
- [ ] Table for `kind=routing`:
  - When
  - About (Decision #id + pair link)
  - Order (side, size, type)
  - Outcome badge + venues
  - All-in
  - **Saving vs best ask**: the per-contract difference between the best ask in the decision's stored input snapshot and its average fill. Positive is favourable, otherwise parentheses. The exact formula and the sign rule are printed under the table. A decision without stored inputs shows "not stored".
  - Max quote age (stamp)
  - Label at decision
  - Summary (clamped, in `details`)
  Every other kind keeps When / Kind (badge) / About (+ `AboutTitle`) / Summary.
- [ ] The VM joins each routing entry to its stored decision (one batched query for the listed rows, not one per row) for pair, side, size, type, outcome, venues, average all-in, best ask, max quote age and label. `group=` and `kind=` are validated (AB-08).
- [ ] Nothing dropped (Log bullets of the audit): intro, kind filter, empty state, count line, When / Kind / About / Summary.
- [ ] `handlers_log_test.go` covers:
  - the kind chips and `aria-current`
  - the routing table columns
  - the sign rule for a saving and for a loss
  - "not stored" for an old decision
  - grouping by venue, staleness and label
  - the outliers
  `TestDecisionLogPage` and `TestLogEntryLink` are updated here.
- [ ] Real before/after screenshots at 1280 px and 390 px. No fake screenshots. p95 for `GET /log` and `GET /log?kind=routing` is quoted.

**Verification:** `go test ./internal/web/... -run 'Log' -v && make check`

**Dependencies:** UI-10, UI-25
**Shares files with:** none in wave B
**Files likely touched:** `internal/web/templates/log.html`, `internal/web/handlers_log.go`, `internal/web/handlers_log_test.go`, `internal/store/decisionlog.go` (read-only join query, if needed), `internal/web/pages_test.go` (`TestDecisionLogPage`, `TestLogEntryLink` only), `docs/log.md`

---

## UI-16: Eval page v2

**Context:** `HANDOFF.md` "Eval" and the `Eval v2` mock. The headline and its verdict come first, followed by a per-type table with support, F1 and averages.

**Satisfies:** FR-21 (eval results page), FR-18, US-06

**Acceptance criteria**
- [ ] The h1 is kept and a question line is added.
- [ ] Head grid:
  - a **Headline panel**: precision at equivalent and the verdict badge (`✓ meets the 0.95 target`, `✕ below …`, `? undefined`), plus the exit-bar text and support
  - next to it, the existing explanatory paragraph and the "Produced … by … on round … labelled pairs from … method …" line
- [ ] **Quality by type**: the existing 7 columns plus **F1** (n/a when undefined), with `meter` bars for Precision and Recall (`td.meter-cell`). It is followed by **Macro average** and **Weighted by support** rows, computed in the handler from the summary's existing fields.
- [ ] Notes from the run are kept. The Missing state (a `blocked` badge plus the 3-step `ol`, same words) and the Broken state (danger panel) are kept. "The summary has no per-type metrics." is kept.
- [ ] The page states that there is no calibration plot because confidence is a rules score, not a probability.
- [ ] The confusion matrix, `/eval/cell`, threshold sweep, golden-set version, previous run Δ and rules+LLM columns stay hidden until UI-23 and UI-24.
- [ ] Nothing dropped (Eval bullets of the audit): intro and Produced line; precision strip and verdict; the 7 table columns and "no per-type metrics"; notes; the Missing and Broken states.
- [ ] `handlers_eval_test.go` covers F1 (including undefined), macro and weighted averages, the meter values and the three headline states. `TestEvalPage` is updated here.
- [ ] Real before/after screenshots at 1280 px and 390 px. No fake screenshots.

**Verification:** `go test ./internal/web/... -run 'Eval' -v && make check`

**Dependencies:** UI-10
**Shares files with:** UI-23, UI-24
**Files likely touched:** `internal/web/templates/eval.html`, `internal/web/handlers_eval.go`, `internal/web/handlers_eval_test.go`, `internal/web/pages_test.go` (`TestEvalPage`, `TestFormatPercentAndHTTPURL` only), `docs/log.md`

---

## UI-17: Admin page v2 (health rollup)

**Context:** `HANDOFF.md` "Admin" and the `Admin v2` mock. P3 sees data health at a glance. A row's status is its worst signal, and unknown is gray, never green.

**Satisfies:** FR-21 (admin page), FR-22 (actions stay behind basic auth, unchanged), US-09

**Acceptance criteria**
- [ ] Renders through `layout.html` with `Page=""` (outside the public nav), keeps `noindex`, and carries the question "Is data flowing, which round is pinned, and what can the LLM verifier spend?". No new routes.
- [ ] The AB-15 banner (`.banner.banner-danger`, ✕) and the `.Message` flash (`role=status`) come first.
- [ ] **Health** `table.health` with one row each for Collectors, Database, Matcher, Pinned round and LLM verifier:
  - Status is the worst signal (degraded > warning > healthy). Any unknown signal with nothing worse makes the row **Unknown (gray)**.
  - The Matcher row is Unknown until UI-26.
  - Signals sit in `ul.signals` with ✓/!/✕/?.
- [ ] The Collectors row has recent-round bars (`svg.bars`) from the existing rounds query (last 24): full height = ok, ¾ = partial, ½ = failed, outline = missing. Each bar has a `<title>` and the strip has an `aria-label` summary. The freshness windows are written under the table.
- [ ] Two columns with every sentence verbatim (tests assert MEETS/BELOW TARGET):
  - Collector success: the sentence, the count chips and the observed/scheduled line
  - Database: the sentence and a size `meter` against the threshold
- [ ] **Actions**: Ingest, Unpin and LLM Verification, each with a state badge in its `header`. Forms, button labels, small text and disabled states are unchanged, and the D-097 last-run summary still shows.
- [ ] **Data notes** (`.data-notes`, static): no Polymarket venue time, pinned rounds not judged for freshness, approximate fees.
- [ ] **Last N rounds**: the 10 columns and rowspans are kept, status is a badge, and the pinned marker and empty state are kept.
- [ ] The LLM verifier runs table and the 90-day history stay hidden until UI-22 and UI-26.
- [ ] Nothing dropped (Admin bullets of the audit):
  - collector, DB and pinned values; the AB-15 banner and detail
  - the collector sentence and counts; the message
  - the three actions with all states (including D-097's last run)
  - the rounds table, all columns and the empty state
- [ ] `admin_test.go` covers the worst-signal rollup (including unknown → gray), the bar classes and titles, and every existing verifier state. The existing assertions are updated here. `tests/security/ab15_*` keeps passing.
- [ ] Real before/after screenshots at 1280 px and 390 px. No fake screenshots. p95 for `GET /admin` is quoted.

**Verification:** `go test ./internal/web/... ./tests/security/... -run 'Admin|AB15|Collector' -v && make check`

**Dependencies:** UI-10
**Shares files with:** UI-19, UI-22, UI-26 (`admin.go`, `admin.html`)
**Files likely touched:** `internal/web/templates/admin.html`, `internal/web/admin.go`, `internal/web/admin_test.go`, `internal/web/collector_stats.go` (bars only), `internal/web/collector_stats_test.go`, `docs/log.md`

---
## UI-18: Net gap per pair, review flag, venue fee estimates (◆ 1–3)

**Context:** `HANDOFF.md` ◆ items 1–3 (Matches). The research brief: ranking by edge rewards bad matches, and a large edge on an imperfect match is "almost always a wrong pairing". This ticket computes a fee-adjusted best-price gap per pair per round, flags large edges on imperfect matches, and shows the per-venue fee assumptions in the checks key.

**Satisfies:** FR-21, FR-16, US-01, NFR-16

**Acceptance criteria**
- [ ] **Net gap per pair** (◆ 1): computed from the round's stored quotes and each instrument's stored, verified fee schedule (D-073) at a stated reference size, e.g. 100 contracts. Fees use `internal/fees`, so there are no new formulas and no venue calls. A pair whose quotes are missing or stale shows "no gap: <reason>", never 0.
- [ ] Choose between computing at read time (Tier 1, if p95 for `GET /` stays ≤ 500 ms on the seeded round) and persisting per round (a schema change: Tier 2, migration, D- entry). The MR records which and why.
- [ ] **Review flag and Flagged count** (◆ 2):
  - A pair is flagged when its net gap is at least a new config key (e.g. `REVIEW_EDGE_MIN`, documented in `docs/environment.md`) **and** it is not `equivalent` at confidence ≥ 0.90.
  - Flagged rows get `tr.flagged`, `.b-review` "⚠ review: large edge, low confidence" and the sentence "Likely a wrong pairing, not an arbitrage." They sort first in Review priority.
  - The strip shows "Flagged for review".
- [ ] **Venue fee estimates** (◆ 3): the checks key quotes each venue's fee assumption from the verified schedule with its source link and "approximate" where the schedule says so. Nothing is hard-coded.
- [ ] The Market column shows the net gap with the sign rule (D-098 call 2), and the rule is printed under the table.
- [ ] **Tier 2:** the new config key (shared `docs/environment.md`, and a schema change if persisted) gets a D- entry and an "Awaiting human veto" row in `status.md`.
- [ ] Tests: gap math on fixture books (both directions, fee rounding), missing/stale quote → no gap, flag boundary at the threshold and at confidence 0.89/0.90, sort with flagged first, config default and override.
- [ ] Nothing the current UI shows is dropped (the UI-11 matches audit still passes). Real before/after screenshots at 1280 px and 390 px from `make seed && make dev`, with no fake screenshots. p95 for `GET /` is quoted.

**Verification:** `go test ./internal/web/... ./internal/fees/... ./internal/config/... -run 'Matches|Gap|Flag|Fee|Config' -v && make check`

**Dependencies:** UI-11
**Shares files with:** UI-11 (`matches.html`, `handlers_matches.go`)
**Files likely touched:** `internal/web/handlers_matches.go`, `internal/web/templates/matches.html`, `internal/web/handlers_matches_test.go`, `internal/web/netgap.go` (NEW) + test, `internal/config/config.go` + test, `docs/environment.md`, `docs/decisions.md`, `docs/status.md`, (if persisted) `migrations/000N_pair_gaps.sql`, `internal/store/*`, `docs/architecture.md`, `docs/log.md`

---

## UI-19: Freshness Warn window per source (◆ 4)

**Context:** `HANDOFF.md` ◆ item 4 (all pages via the stamp, plus Admin). It follows Dagster's model: a warn window comes before the fail (stale) window. Today only the stale threshold exists (D-077, `STALE_AFTER_SECONDS`, `STALE_ROUND_WARN_MINUTES`).

**Satisfies:** FR-21, US-05, NFR-09

**Acceptance criteria**
- [ ] New config keys give an earlier warn window per source (Kalshi, Polymarket, round), each strictly below its stale threshold. Config load rejects warn ≥ stale. The keys are documented in `docs/environment.md`.
- [ ] `FreshBadge`/`FreshState` return `◐ Warn` (`.f-warn`) between the warn and stale windows. Pinned still replaces the state while a round is pinned.
- [ ] The stamp, the Matches Market column, the Pair touch table, the trace snapshot and the Log max-age column all show Warn through the shared define. No page template forks the logic.
- [ ] The Admin freshness windows line under the health table states both windows per source, and a warn-window signal feeds the worst-signal rollup as `!`.
- [ ] **Tier 2:** new config keys and defaults (shared `docs/environment.md`) get a D- entry and an "Awaiting human veto" row. No schema change. The stale threshold and its banner text are unchanged (`TestStaleRoundWarning`).
- [ ] Tests cover the boundaries (warn−1 s, warn, stale−1 s, stale), pinned overriding warn, a config validation error, and the admin rollup with a warn signal.
- [ ] Nothing the current UI shows is dropped. Real before/after screenshots at 1280 px and 390 px of a page with a Warn stamp and of `/admin`, with no fake screenshots.

**Verification:** `go test ./internal/web/... ./internal/config/... -run 'Fresh|Stale|Admin|Config|Layout' -v && make check`

**Dependencies:** UI-10, UI-17
**Shares files with:** UI-10 (`layout.html`, `viewmodel.go`), UI-17/UI-22/UI-26 (`admin.go`)
**Files likely touched:** `internal/config/config.go` + test, `internal/web/viewmodel.go` + test, `internal/web/templates/layout.html` (stamp define only), `internal/web/admin.go`, `internal/web/templates/admin.html`, `internal/web/admin_test.go`, `docs/environment.md`, `docs/decisions.md`, `docs/status.md`, `docs/log.md`

---

## UI-20: Matcher evidence: per-term contribution + pre-veto score (◆ 5, 8)

**Context:** `HANDOFF.md` ◆ item 5 (the Pair Contribution column: prior, signed per-term weight, total → confidence) and ◆ item 8 (Near misses: the match score before vetoes against the required score). The matcher stores no weights or pre-veto score today. This ticket records what the matcher already computes. It must not change what the matcher decides.

**Satisfies:** FR-09, FR-21, US-01, US-02

**Acceptance criteria**
- [ ] The matcher records, per compared pair:
  - the prior
  - each compared term's signed contribution
  - the total that maps to the stored confidence
  - the pre-veto score and the required score
  These are stored with the relationship, as new fields in `relationships.diff`/a new jsonb column or as columns via a new migration. If the rules confidence is not additive, the ticket records the actual computation honestly (e.g. per-term factors) and the column header says what the numbers are. No invented weights.
- [ ] **No verdict, type or confidence changes.** The golden eval and back-test outputs are byte-identical before and after on the fixture round, and the MR shows that diff. If any classification would change, that touches the equivalence definition: **2-critical**, so stop and ask Paawan.
- [ ] Pair page: the Contribution column in `table.terms` shows each row's signed contribution (detracting rows use the danger class with a text sign), plus a footer "prior + Σ = total → confidence 0.93".
- [ ] Near misses: a "Score" cell reads "0.62 of 0.90 required".
- [ ] Relationships stored before this ticket show "not recorded", and their columns stay hidden.
- [ ] **Tier 2:** schema change → D- entry, "Awaiting human veto" row, `docs/architecture.md` data model, a new migration with up and down tested.
- [ ] Tests:
  - contribution sums reproduce the stored confidence for fixture pairs of every type
  - the pre-veto score for a vetoed pair
  - old rows render "not recorded"
  - the migration round trip
  - unchanged classification on the golden fixtures
- [ ] Nothing the current UI shows is dropped (the UI-12 and UI-14 audits still pass). Real before/after screenshots at 1280 px and 390 px of a pair and of `/near-misses`, with no fake screenshots. p95 for both pages is quoted.

**Verification:** `go test ./internal/match/... ./internal/store/... ./internal/eval/... ./internal/web/... -run 'Match|Classify|Contribution|NearMiss|Pair|Migrat|Backtest' -v && make check`

**Dependencies:** UI-12, UI-14, #39 (!58 changes the matcher; start only after it merges)
**Shares files with:** UI-12/UI-21/UI-22 (`pair.html`, `handlers_pairs.go`), UI-14 (`near_misses.html`), UI-26 (the match command)
**Files likely touched:** `internal/match/classify.go`, `internal/match/match.go`, `internal/match/*_test.go`, `internal/canonical/*` (relationship type), `internal/store/relationships.go` + test, `migrations/000N_match_evidence.sql`, `internal/web/handlers_pairs.go`, `internal/web/templates/pair.html`, `internal/web/handlers_nearmisses.go`, `internal/web/templates/near_misses.html`, their tests, `docs/architecture.md`, `docs/decisions.md`, `docs/status.md`, `docs/log.md`

---

## UI-21: Rule layers: summary + clarifications (◆ 6)

**Context:** `HANDOFF.md` ◆ item 6 (Pair rule layers). Kalshi has a rules summary and "Additional Details" clarifications. Polymarket has rules and "Additional context". Only the title and rules text are ingested today (`instruments.rules_text`). The title is not the contract, so P1 needs every layer.

**Satisfies:** FR-01, FR-02, FR-05, FR-21, US-02

**Acceptance criteria**
- [ ] The Kalshi and Polymarket adapters capture the summary and clarification text from fields the existing public API responses already return. No new endpoints if avoidable; any new endpoint is called once per market per round within the existing rate limits.
- [ ] Each field is sanitised and length-capped like `rules_text`, with the AB-02 caps. Fixtures are re-recorded or extended with `make` targets, and tests make no live calls.
- [ ] They are stored in new `instruments` columns (`rules_summary`, `clarifications`, plus the clarification timestamp when the venue gives one) via a new migration.
- [ ] Not fed to the matcher, the LLM prompt or the cache key in this ticket. Feeding them would change the equivalence inputs and the AB-01/AB-11 surface, which is 2-critical in its own ticket.
- [ ] Pair page: the `.layers` group shows Title, Summary, Full rules (open) and Clarifications. A clarification posted after the market opened is flagged "clarified <date>". An empty layer reads "venue publishes none". Everything is escaped by `html/template` (AB-07). An XSS fixture string renders inert.
- [ ] **Tier 2:** schema change → D- entry, "Awaiting human veto" row, `docs/architecture.md`, a new migration with up and down tested. `docs/domain.md`/`docs/architecture.md` describe the new fields.
- [ ] Tests: adapter parsing from fixtures for both venues (present, absent, oversized), store round trip, pair page layers and the XSS fixture.
- [ ] Nothing the current UI shows is dropped. Real before/after screenshots at 1280 px and 390 px, with no fake screenshots.

**Verification:** `go test ./internal/venues/... ./internal/store/... ./internal/web/... ./tests/security/... -run 'Kalshi|Polymarket|Instrument|Migrat|Pair|AB07|AB02' -v && make check`

**Dependencies:** UI-12
**Shares files with:** UI-12/UI-20/UI-22 (`pair.html`, `handlers_pairs.go`)
**Files likely touched:** `internal/venues/kalshi/*`, `internal/venues/polymarket/*`, `internal/canonical/*` (Instrument), `internal/store/instruments.go` + test, `migrations/000N_rule_layers.sql`, `testdata/fixtures/*` (re-recorded), `internal/web/handlers_pairs.go`, `internal/web/templates/pair.html`, `internal/web/handlers_pairs_test.go`, `docs/architecture.md`, `docs/decisions.md`, `docs/status.md`, `docs/log.md`

---

## UI-22: LLM verifier persistence: per-pair rationale + runs table (◆ 7, 16)

**Context:** `HANDOFF.md` ◆ item 7 (Pair: the LLM verifier rationale, run id and timestamp) and ◆ item 16 (Admin: the LLM verifier runs table). D-097 stores only the last run's summary (singleton) and rejected run history as bloat. This ticket needs bounded history, so its D- entry references D-097.

**Satisfies:** FR-20, FR-21, FR-22, US-02, US-09

**Acceptance criteria**
- [ ] A new `llm_verify_runs` table: id, started/finished, model, prompt version, verified, downgraded, cache hits, calls and tokens used, and the outcome (ok / cap reached / error).
  - Retention is capped (e.g. the last 100 runs, pruned in the same transaction), so AB-15 growth stays bounded.
  - D-097's singleton either stays as is or becomes a view of the latest run. The D- entry says which.
- [ ] Each verified pair stores its rationale, run id and timestamp, linked to the existing `llm_verdicts` cache row. No duplicate verdict storage.
- [ ] The rationale is model output and treated as untrusted:
  - strict-decoded as today (AB-13)
  - length-capped
  - escaped by `html/template` (AB-07)
  - labelled "LLM verifier rationale (model output)"
- [ ] Pair page: the Why section gains a `details` with the rationale, model, run id and timestamp. It is hidden when the pair was never verified.
- [ ] Admin: the "LLM verifier runs" table (latest first, at most 20 rows) with an empty state.
- [ ] No live Anthropic calls in tests: the mock client only, and `ANTHROPIC_API_KEY` is never set in the worker env. Spend rules are unchanged (daily cap, cache first, cheapest model, operator-rules § 3).
- [ ] **Tier 2:** schema change → D- entry (referencing D-097), "Awaiting human veto" row, `docs/architecture.md`, a new migration with up and down tested. If the change alters any AB-04/AB-11/AB-13 mitigation, it is **2-critical**: stop and ask.
- [ ] Tests: run persistence and pruning, rationale storage and the link to the cache row, an injected-HTML rationale renders inert, the admin table and empty state, and the pair `details` present and absent.
- [ ] Nothing the current UI shows is dropped (the UI-12 and UI-17 audits, including D-097's last-run line). Real before/after screenshots at 1280 px and 390 px of a verified pair and of `/admin`, with no fake screenshots.

**Verification:** `go test ./internal/llm/... ./internal/store/... ./internal/web/... ./tests/security/... -run 'LLM|Verif|Admin|Pair|Migrat|AB07|AB13|AB15' -v && make check`

**Dependencies:** UI-12, UI-17
**Shares files with:** UI-12/UI-20/UI-21 (`pair.html`), UI-17/UI-19/UI-26 (`admin.go`, `admin.html`)
**Files likely touched:** `internal/llm/verifier.go`, `internal/llm/pgcache.go`, `internal/store/llm.go` + test, `migrations/000N_llm_verify_runs.sql`, `internal/web/admin.go`, `internal/web/templates/admin.html`, `internal/web/admin_test.go`, `internal/web/handlers_pairs.go`, `internal/web/templates/pair.html`, `internal/web/handlers_pairs_test.go`, `docs/architecture.md`, `docs/decisions.md`, `docs/status.md`, `docs/log.md`

---

## UI-23: Eval artefacts: confusion matrix + drill-down, threshold sweep, golden-set version + Δ (◆ 9–11)

**Context:** `HANDOFF.md` ◆ items 9–11 (Eval). D-070 fixed `eval_summary.json` as a read-only contract with "no other field". Extending it is a contract change, so this ticket gets a Tier 2 entry.

**Satisfies:** FR-18, FR-21, US-06, NFR-06

**Acceptance criteria**
- [ ] `equinox eval --out` adds these fields, all optional so old summaries still load:
  - `confusion`: counts and pair ids per (label, predicted), including `unrelated`
  - `thresholds`: P/R/TP/FP for `equivalent` at confidence cut-offs, including the 0.90 floor
  - `labels_sha256` (the golden-set version)
  - `previous`: the previous summary's `generated_at`, `labels_sha256` and per-type P/R. The previous file is rotated to `config/eval_summary.prev.json`, which is generated and never committed.
- [ ] Eval page:
  - The **Confusion matrix** is `table.confusion` with `d1–d3`/`e1–e3` bucket classes and always prints the count, with an `aria-label` sentence per cell.
  - Each non-zero cell links to **`GET /eval/cell?label=…&predicted=…`**, a new read-only public GET route. Params are validated against the type list (AB-08), it is rate-limited like other public pages (AB-03), and it lists the pairs linking to Pair detail. It works without JS, with htmx enhancing it.
  - **P/R at confidence thresholds** has the 0.90 row highlighted.
  - The head shows the golden-set version and "Δ vs previous run" per type with the sign rule. When the label hash differs it says "labels changed, Δ not comparable".
- [ ] **Tier 2:** a change to the D-070 eval contract (and a new public route) gets a D- entry and an "Awaiting human veto" row. No DB schema. Labels stay human-owned (`testdata/golden/labels.yaml` is not edited).
- [ ] Tests:
  - confusion counts on a fixture labels file
  - the threshold sweep numbers
  - hash stability
  - rotation of the previous summary
  - an old summary without the new fields still renders the UI-16 page
  - `/eval/cell` with valid and invalid params (400) and rate limiting
- [ ] Nothing the current UI shows is dropped (the UI-16 eval audit). Real before/after screenshots at 1280 px and 390 px from a real `equinox eval` run on the seeded round (fixture labels, never invented numbers), with no fake screenshots. p95 for `/eval` and `/eval/cell` is quoted.

**Verification:** `go test ./internal/eval/... ./internal/web/... ./tests/security/... -run 'Eval|Summary|Confusion|Threshold|RateLimit' -v && make check`

**Dependencies:** UI-16
**Shares files with:** UI-16, UI-24 (`eval.html`, `handlers_eval.go`, `internal/eval/summary.go`)
**Files likely touched:** `internal/eval/summary.go`, `internal/eval/metrics.go`, their tests, `cmd/equinox/eval.go` + test, `internal/web/handlers_eval.go`, `internal/web/templates/eval.html`, `internal/web/handlers_eval_test.go`, `internal/web/server.go` (route registration), `docs/decisions.md`, `docs/status.md`, `docs/log.md`

---

## UI-24: Eval: rules vs rules+LLM columns (◆ 12)

**Context:** `HANDOFF.md` ◆ item 12. UI-07 shipped single-method rendering because M3-06 writes no rules+llm metrics (`summary.go`). This ticket makes `equinox eval` able to report rules+llm alongside rules and renders the side-by-side columns with Δ.

**Satisfies:** FR-18, FR-20, FR-21, US-06

**Acceptance criteria**
- [ ] `equinox eval --method rules+llm` (or `--with-llm`) re-runs the verifier over the labelled pairs **through the existing cache and daily cap**, and writes per-type metrics for both methods into the summary (optional fields).
- [ ] With the verifier disabled or the cap reached, it writes rules only plus a note. It never fails silently.
- [ ] Eval page: the rules and rules+llm columns sit side by side with Δ under the sign rule (D-098 call 2), and "rules only: <reason>" shows when the summary has no LLM figures.
- [ ] **Spend:** tests use the mock client only. CI never calls Anthropic. A real rules+llm run is a manual admin action (`make llm-eval-live` style), on the cheapest model, cache first, under the daily cap (operator-rules § 3, NFR-15). The MR states the projected calls for the golden set. Spend projected above $35 is **2-critical**.
- [ ] **Tier 2:** an eval contract change (summary fields) gets a D- entry and an "Awaiting human veto" row.
- [ ] Tests: both-method summary from mock verdicts, disabled/cap-reached notes, Δ rendering, and an old summary still renders.
- [ ] Nothing the current UI shows is dropped. Real before/after screenshots at 1280 px and 390 px from a mock-backed or cached real run, labelled as such, with no fake screenshots.

**Verification:** `go test ./internal/eval/... ./internal/llm/... ./internal/web/... -run 'Eval|LLM|Summary' -v && make check`

**Dependencies:** UI-23 (M3-06 is merged)
**Shares files with:** UI-23, UI-16
**Files likely touched:** `internal/eval/summary.go`, `internal/eval/*.go` + tests, `cmd/equinox/eval.go` + test, `internal/web/handlers_eval.go`, `internal/web/templates/eval.html`, `internal/web/handlers_eval_test.go`, `docs/decisions.md`, `docs/status.md`, `docs/log.md`

---

## UI-25: Router stores input quotes and label with each decision (◆ 13, 17; D-098 call 3)

**Context:** D-098 call 3 and `HANDOFF.md` ◆ items 13 and 17. `canonical.Decision` (stored as `routing_decisions.decision` jsonb) holds the chosen legs, alternatives and exclusions but not the quotes the router read or the relationship type of each candidate at decision time. Re-querying `quotes` later can disagree with what the router saw. This ticket stores the inputs so the Decision trace (UI-13) and the log (UI-15) show the router's actual view.

**Satisfies:** FR-12, FR-15, FR-17, US-03, US-05, US-08

**Acceptance criteria**
- [ ] `canonical.Decision` gains `Inputs []DecisionInput` with JSON `inputs,omitempty`, one per candidate considered (eligible or not):
  - venue and instrument id
  - relationship type and confidence at decision time (`self` for the target)
  - outcome side read
  - best ask and ask depth (top N levels, capped, e.g. 10)
  - quote `fetched_at` and `venue_ts`
  - the quote's round id
  Also `TargetType`, the target pair's relationship type at decision time.
- [ ] Filled by the router from the same in-memory quotes it routed on, never re-read afterwards. Stored by `InsertRoutingDecision` in the same jsonb, so no SQL migration is needed unless the worker shows one is required.
- [ ] **No routing outcome changes.** Every existing `internal/route` test (including `docs_examples_test.go` and complement/sweep) passes unchanged, apart from new assertions on `Inputs`.
- [ ] Size is bounded: levels and candidates are capped, and the MR quotes the decision size before and after for the fixture sweep (AB-15).
- [ ] Old decisions decode with empty `Inputs`. `GetRoutingDecision` and `ListRoutingDecisions` round-trip the new fields. `docs/architecture.md` (decision shape) and `docs/routing.md` (what is stored) are updated.
- [ ] **Tier 2:** a change to the stored decision schema (persisted JSON contract, shared canonical type) gets a D- entry and an "Awaiting human veto" row. Mark it 2-critical only if it touches the AB-10 mitigation's behaviour (it should only add a record of it).
- [ ] Tests:
  - inputs recorded for a single-venue fill, a two-venue sweep, a complement route and a not-routed order (excluded candidates included)
  - the stored inputs equal the quotes passed to the router
  - the cap is respected
  - an old decision JSON without `inputs` decodes
  - p95 for `POST /route` stays ≤ 300 ms (quoted)
- [ ] No UI change in this ticket (UI-13 and UI-15 render it), so no screenshots are needed. Nothing the current UI shows changes: the existing `/decisions/{id}` tests still pass.

**Verification:** `go test ./internal/route/... ./internal/canonical/... ./internal/store/... ./internal/web/... -run 'Route|Decision|Routing|Sweep|Complement|Docs' -v && make check`

**Dependencies:** none (backend only; can run beside UI-10)
**Shares files with:** none in wave A
**Files likely touched:** `internal/canonical/order.go`, `internal/route/route.go`, `internal/route/sweep.go`, `internal/route/*_test.go`, `internal/store/routing.go`, `internal/store/crud_test.go`, `docs/architecture.md`, `docs/routing.md`, `docs/decisions.md`, `docs/status.md`, `docs/log.md`

---

## UI-26: Admin health data: match runs + 90-day daily history (◆ 14, 15)

**Context:** `HANDOFF.md` ◆ items 14 and 15 (Admin). The Matcher health row is Unknown until match runs are recorded. The Statuspage-style 90-day bars need a daily rollup of round outcomes, with gray for days with no data.

**Satisfies:** FR-21, FR-22, US-09, NFR-03

**Acceptance criteria**
- [ ] A new `match_runs` table (round id, started, finished, duration, pairs compared, relationships stored, outcome) is written by `equinox match` and by any scheduled match path. Retention is capped (e.g. 500 rows).
- [ ] The Admin Matcher row derives its signals from it: last run age against a window, last duration, and failures. It stays Unknown (gray) when no run is recorded. It is never Healthy by default.
- [ ] **90-day daily history:** daily ok/partial/failed/missing counts from `fetch_rounds`. Use a read-time query (Tier 1) if `fetch_rounds` is retained for 90 days and p95 for `/admin` stays ≤ 500 ms. Otherwise use a `daily_round_rollup` table (Tier 2). The MR says which.
- [ ] The 90-day bars render as `svg.bars` with a `<title>` per day, gray outline for no-data days, an `aria-label` summary, and shape cues that are not colour alone.
- [ ] **Tier 2:** schema change (`match_runs`, plus a rollup table if used) → D- entry, "Awaiting human veto" row, `docs/architecture.md`, a new migration with up and down tested.
- [ ] Tests: run recording (ok and failed), retention, the Matcher row states (no run → Unknown, recent → Healthy, old → Warning, failed → Degraded), daily rollup counts with gaps, and bar classes and titles.
- [ ] Nothing the current UI shows is dropped (the UI-17 admin audit). Real before/after screenshots at 1280 px and 390 px of `/admin`, with no fake screenshots. p95 is quoted.

**Verification:** `go test ./internal/store/... ./internal/web/... ./cmd/equinox/... -run 'Match|Admin|Rollup|Migrat|Rounds' -v && make check`

**Dependencies:** UI-17
**Shares files with:** UI-17/UI-19/UI-22 (`admin.go`, `admin.html`), UI-20 (the match command)
**Files likely touched:** `cmd/equinox/match.go` + test, `internal/store/rounds.go`, `internal/store/match_runs.go` (NEW) + test, `migrations/000N_match_runs.sql`, `internal/web/admin.go`, `internal/web/templates/admin.html`, `internal/web/admin_test.go`, `docs/architecture.md`, `docs/decisions.md`, `docs/status.md`, `docs/log.md`

---

## AC carry-over (UI-02..UI-08 → UI-v2)

No UI-02..UI-08 acceptance criterion is dropped. Each carries into the ticket that now owns its page, unchanged or stricter. The wave 9 shared rules (security/CSP, SRI, 1280/390 screenshots, keyboard focus, text + colour, WCAG AA, NFR-01 p95, `make check`, file ownership) carry into every UI-v2 ticket through the shared rules above.

| Wave 9 ticket | Its ACs now live in | Notes |
|---|---|---|
| UI-02 layout, tokens, components, view-model helpers | UI-10 | Tokens and components kept and extended. `.asof` becomes the stamp. Stale/demo banners keep their text. Helpers keep their behaviour except complement → blue (D-098). |
| UI-03 matches | UI-11 (+ UI-18) | Strip, routable grouping, related collapsed, Route link and stacking at 390 px kept. Checks and sort added. |
| UI-04 pair | UI-12 (+ UI-20, UI-21, UI-22) | Section order, diff annotations, veto heading variants, facts/basis and `#route` kept. Diff grouped by rule part. |
| UI-05 routing trace | UI-13 (+ UI-25) | Outcome strip, legs with all cost terms and totals, alternatives with gap, exclusion phrases, one shared partial and unchanged `hx-*` kept. |
| UI-06 near misses + log | UI-14, UI-15 | Veto-first rows, kind chips, clamped summaries and the filter form kept. |
| UI-07 eval | UI-16 (+ UI-23, UI-24) | Headline with verdict, type chips, empty state and the M3-06 delta (now UI-24) kept. |
| UI-08 admin | UI-17 (+ UI-19, UI-22, UI-26) | Layout, noindex, health values with AB-15, the actions with all four verifier states and the D-097 last run, and the rounds table kept. |

## GitLab issues

Each ticket is a GitLab issue labelled `ui` and `UI-v2`, created 2026-10-08. The issue body repeats the ticket. This file is the source of truth.

| Ticket | Issue | Depends on (issues) |
|---|---|---|
| UI-10 | #42 | — |
| UI-11 | #43 | #42 |
| UI-12 | #44 | #42 |
| UI-13 | #45 | #42, #57 |
| UI-14 | #46 | #42 |
| UI-15 | #47 | #42, #57 |
| UI-16 | #48 | #42 |
| UI-17 | #49 | #42 |
| UI-18 | #50 | #43 |
| UI-19 | #51 | #42, #49 |
| UI-20 | #52 | #44, #46, #39 |
| UI-21 | #53 | #44 |
| UI-22 | #54 | #44, #49 |
| UI-23 | #55 | #48 |
| UI-24 | #56 | #55 |
| UI-25 | #57 | — |
| UI-26 | #58 | #49 |
