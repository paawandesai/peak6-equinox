<!-- OWNER: shared. Design export; edit only to re-export from Claude Design. -->
# Claude Design v2 (UI-09)

The Claude Design pass on the Equinox UI asked for in issue #41 (UI-09). Paawan approved this design on 2026-10-08 at 1:41 PM CT ([D-098](../../decisions.md)). The build tickets are in [docs/tickets/UI-v2.md](../../tickets/UI-v2.md), and the summary of what changes is in [docs/design.md § 0](../../design.md#0-claude-design-v2-ui-09-supersedes-the-layouts).

## Provenance

- **Source:** Claude Design project https://claude.ai/design/p/e12067cf-36f4-4e61-98c1-384ed23f8f3d (standalone claude.ai/design, not the in-CLI `/design`).
- **Exported:** 2026-10-08, as `Peak6 Equinox UI-09.zip` (54 files).
- **Committed here:** `HANDOFF.md` (the build spec, Revision 1), `handoff/app.css` (the drop-in stylesheet), every v2 page, the v2 design system, the four building blocks the pages import (`Eq Badge`, `Eq Conf`, `Eq Fresh`, `Eq Header v2`), `support.js` (the design tool's page runtime, which every page loads), and `RESEARCH.md` (the research brief `HANDOFF.md` cites; it was uploaded to the project as `research_file-…-qj5r.md`).
- **v1 pages kept:** the previous pass (`Matches.dc.html`, `Pair.dc.html`, `Route.dc.html`, `Decision.dc.html`, `Near Misses.dc.html`, `Log.dc.html`, `Eval.dc.html`, `Admin.dc.html`, `Error.dc.html`, `Equinox Design System.dc.html`, `Eq Header.dc.html`). They are small text files and `HANDOFF.md` keeps them for comparison. Only v2 is approved for building.
- **Left out of the repo:**
  - `uploads/claude-design-bundle/`: the handoff bundle we fed in. It is a copy of our own `docs/brief.md`, `docs/design.md`, `docs/requirements.md`, `internal/web/static/app.css`, `internal/web/templates/*.html`, a `PROMPT.md`, and five PNG screenshots of the v0/UI-08 pages.
  - `uploads/research_file-…-1ebv.md`: byte-identical to that bundle's `PROMPT.md`.
  - `.thumbnail` (a WebP project thumbnail) and `scratch/h.jpg`: binaries (operator-rules § 5).

Every committed file is text and under 70 KB. gitleaks found no leaks in this directory.

## How to open the pages

The `*.dc.html` files are Claude Design canvases, not app templates. Each one loads `./support.js`. That runtime pulls React 18.3.1, ReactDOM and Babel standalone from unpkg and fetches each imported building block (for example `Eq Badge.dc.html`) from this directory. So the pages need a network connection and an HTTP server. `file://` cannot fetch the imports.

```sh
cd docs/design/claude-design-v2
python3 -m http.server 8765 --bind 127.0.0.1
# open http://127.0.0.1:8765/Matches%20v2.dc.html (or any other *.dc.html)
# stop the server afterwards (Ctrl-C) and check nothing is left on 8765
```

Each page shows a 1280 px desktop frame, a 390 px mobile frame and build notes. Start with `Equinox Design System v2.dc.html` (badges, confidence, freshness stamp, health and outcome states, page index), then follow `HANDOFF.md`.

## What is and is not real

- **The sample data is illustrative.** Pair ids, prices, confidences, counts, timestamps, fees and eval figures in the mocks are made up. Never quote them as Equinox results, and never use them as test fixtures or screenshots.
- **◆ NEW DATA** (dashed purple outline in the mocks) marks things the server does not produce today. They are not built as if they exist. Each one has a backend ticket in `docs/tickets/UI-v2.md`.
- **Inline styles and the unpkg runtime are design-tool artefacts.** They never reach the app: the app keeps Go `html/template` + htmx + Pico.css, no JS build, no SPA, and a CSP with no `'unsafe-inline'` (D-031, D-081). Templates use the classes in `handoff/app.css`.
- **Three calls Paawan made at approval override the mocks wherever they differ** (D-098):
  1. Complement badges are blue, not green.
  2. Favourable money differences read positive, and unfavourable ones appear in parentheses everywhere. The Log column is "Saving vs best ask", not "Saving vs best alt".
  3. The router stores its input quotes with each decision, so the Decision trace can show them.
