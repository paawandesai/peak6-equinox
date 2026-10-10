#!/usr/bin/env node
// mobile-overflow-check.cjs: fails when any public page scrolls sideways on a phone.
//
// Manual/dev check. It is not wired into CI and needs a local Playwright install
// (no dependency is added to this repo):
//
//   npm i --prefix /some/dir playwright@1.63.0 && npx --prefix /some/dir playwright install chromium
//   PLAYWRIGHT_MODULE=/some/dir/node_modules/playwright \
//     node scripts/mobile-overflow-check.cjs http://localhost:8080 [decisionIDs] [pairIDs]
//
// decisionIDs and pairIDs are comma-separated (or set DECISION_IDS / PAIR_IDS);
// defaults: 96,97,98,99 and 2406,2390.
//
// At 375x667 and 390x844 it exits 1 when document.documentElement.scrollWidth >
// window.innerWidth on any route, listing the widest elements that stick out.
// It also reports (never fails) whether the answer-first blocks sit on the first
// screen: .verdict-card and .next-step at 375x667; .verdict-card, .compare,
// .next-step, and on pair pages .pair-meaning and #route form, at 1440x900.

const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');

const base = (process.argv[2] || 'http://localhost:8080').replace(/\/$/, '');
const list = (arg, env, def) => (arg || process.env[env] || def).split(',').map((s) => s.trim()).filter(Boolean);
const decisionIDs = list(process.argv[3], 'DECISION_IDS', '96,97,98,99');
const pairIDs = list(process.argv[4], 'PAIR_IDS', '2406,2390');

const routes = ['/', '/near-misses', '/log', '/log?kind=routing', '/eval', '/glossary', '/about',
  ...decisionIDs.map((id) => `/decisions/${id}`), ...pairIDs.map((id) => `/pairs/${id}`)];
const phones = [[375, 667], [390, 844]];

// offenders lists the widest elements whose right edge passes the viewport,
// skipping those inside a horizontal scroll container (allowed by design).
function offenders() {
  const vw = window.innerWidth;
  const inScroller = (el) => {
    for (let p = el.parentElement; p && p !== document.body; p = p.parentElement) {
      const ox = getComputedStyle(p).overflowX;
      if ((ox === 'auto' || ox === 'scroll' || ox === 'hidden') && p.clientWidth <= vw) return true;
    }
    return false;
  };
  const out = [];
  for (const el of document.body.querySelectorAll('*')) {
    const r = el.getBoundingClientRect();
    if (r.width === 0 || r.right <= vw + 0.5 || inScroller(el)) continue;
    out.push({ tag: el.tagName.toLowerCase(), cls: String(el.className && el.className.baseVal !== undefined ? el.className.baseVal : el.className || ''), id: el.id || '', width: Math.round(r.width), right: Math.round(r.right) });
  }
  // Long unbreakable text overflows its box without widening any element.
  if (!out.length) {
    const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
    for (let n = walker.nextNode(); n; n = walker.nextNode()) {
      if (!n.textContent.trim() || inScroller(n.parentElement)) continue;
      const range = document.createRange();
      range.selectNodeContents(n);
      const r = range.getBoundingClientRect();
      if (r.right <= vw + 0.5) continue;
      const el = n.parentElement;
      out.push({ tag: el.tagName.toLowerCase() + ' text "' + n.textContent.trim().slice(0, 40) + '"', cls: String(el.className || ''), id: el.id || '', width: Math.round(r.width), right: Math.round(r.right) });
    }
  }
  out.sort((a, b) => b.right - a.right || b.width - a.width);
  return out.slice(0, 8);
}

// tops reports, per selector, the top of the first match (null when absent).
function tops(selectors) {
  const res = {};
  for (const s of selectors) {
    const el = document.querySelector(s);
    res[s] = el ? Math.round(el.getBoundingClientRect().top + window.scrollY) : null;
  }
  return res;
}

async function main() {
  const browser = await chromium.launch();
  let failed = 0;
  const fold = [];
  try {
    for (const [w, h] of [...phones, [1440, 900]]) {
      const ctx = await browser.newContext({ viewport: { width: w, height: h } });
      const page = await ctx.newPage();
      for (const r of routes) {
        const resp = await page.goto(base + r, { waitUntil: 'networkidle', timeout: 60000 });
        const status = resp ? resp.status() : 0;
        if (w !== 1440) {
          const { sw, iw } = await page.evaluate(() => ({ sw: document.documentElement.scrollWidth, iw: window.innerWidth }));
          if (sw > iw) {
            failed++;
            console.log(`FAIL ${w}x${h} ${r} (HTTP ${status}): scrollWidth ${sw} > innerWidth ${iw}`);
            for (const o of await page.evaluate(offenders)) {
              console.log(`     <${o.tag}${o.id ? ' id=' + o.id : ''}${o.cls ? ' class="' + o.cls.slice(0, 60) + '"' : ''}> width ${o.width} right ${o.right}`);
            }
          } else {
            console.log(`ok   ${w}x${h} ${r} (HTTP ${status})`);
          }
        }
        let sel = [];
        const answerPage = r.startsWith('/decisions/') || r.startsWith('/pairs/');
        if (w === 375 && answerPage) sel = ['.verdict-card', '.next-step'];
        if (w === 1440 && answerPage) {
          sel = ['.verdict-card', '.compare', '.next-step'];
          if (r.startsWith('/pairs/')) sel.push('.pair-meaning', '#route form');
        }
        if (sel.length) {
          const t = await page.evaluate(tops, sel);
          for (const s of sel) fold.push({ size: `${w}x${h}`, route: r, selector: s, top: t[s], fold: h });
        }
      }
      await ctx.close();
    }
  } finally {
    await browser.close();
  }
  console.log('\nFirst-screen report (informational):');
  console.log('size       route                 selector        top    on first screen');
  for (const f of fold) {
    const where = f.top === null ? 'absent' : f.top < f.fold ? 'yes' : 'NO';
    console.log(`${f.size.padEnd(10)} ${f.route.padEnd(21)} ${f.selector.padEnd(15)} ${String(f.top ?? '-').padEnd(6)} ${where}`);
  }
  console.log(failed ? `\n${failed} page(s) scroll sideways on a phone.` : '\nNo horizontal page scroll at 375 or 390.');
  process.exit(failed ? 1 : 0);
}

main().catch((e) => { console.error(e); process.exit(2); });
