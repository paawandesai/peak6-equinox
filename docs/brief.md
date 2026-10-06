<!-- OWNER: human only. Operator may propose changes via MR; only the human approves them. -->
# Equinox: Brief

Equinox is a prototype built for PEAK6 Labs, the team building Apex Fintech's Non-Traditional Assets Platform (NTAP), which brings prediction markets to broker-dealers. PEAK6 wrote the original brief, titled "Project Equinox: Cross-Venue Prediction Market Aggregation & Routing Simulation". It is the build PEAK6 uses to evaluate Paawan Desai, a Gauntlet AI fellow.

## Problem

The same real-world event trades on several prediction-market venues, but each venue uses different titles, contract structures, expiry rules and price formats. Nobody can say programmatically "these two contracts are the same bet" or compare their prices on a like-for-like basis, which means no one can route an order intelligently between venues.

## Goals

1. Ingest market metadata and order books from **Kalshi** and **Polymarket** into one venue-independent model.
2. Detect which contracts across venues are economically related, classify each relationship with a written definition, and explain every verdict.
3. Simulate routing a hypothetical order across venues that hold equivalent contracts, and explain the decision.
4. Prove the matcher is accurate, with measured precision on hand-labelled pairs and a back-test on contracts that have already settled.
5. Document the framing, the tradeoffs and every assumption. PEAK6 grades how the problem is framed, how the system is decomposed, how ambiguity is handled and how decisions are justified, along with readability and docs. They do not grade UI polish.

Detail is in [requirements.md](requirements.md). Domain terms are defined in [domain.md](domain.md).

## Non-goals

- Real-money trading, order placement, wallets, or authenticated venue APIs.
- Regulatory or compliance logic, user accounts, and a production-grade UI.
- Venues beyond Kalshi and Polymarket (international). The adapter interface allows more later.
- Real-time streaming (we poll), and scalar or range markets.
- Execution-quality optimisation beyond a greedy sweep.

## Deliverables & deadlines

**Hard deadline: Sunday 2026-10-11, 11:59 AM US Central (16:59 UTC).** Everything below is due by then, and it must work both for an async review and for a live walkthrough with PEAK6 reviewers.

| # | Deliverable | Where |
|---|---|---|
| 1 | Working prototype at a **live public URL**. It is read-only and open to anyone. | Cloud Run, linked from README |
| 2 | Source code | GitLab `labs.gauntletai.com/paawandesai/peak6-equinox`. PEAK6 reviewers have access to this GitLab. |
| 3 | Setup instructions | README.md |
| 4 | Architecture overview | docs/architecture.md, plus a short summary in the README |
| 5 | Written equivalence logic and routing logic | docs/equivalence.md, docs/routing.md (created in M2 and M3) |
| 6 | AI-usage disclosure covering runtime LLM use and AI tools used in development | docs/ai-usage.md (created in M1 and kept current) |
| 7 | Backup walkthrough recording, in case the live demo fails | Linked from README (M4) |

Code freeze is **Sunday 2026-10-11 at 09:00 CT**. After the freeze, only docs and demo fixes are allowed.

## Hard constraints

No agent or operator decision may violate these.

1. **Stack:** Go for all application code; GCP project `peak6-equinox-510816` (us-central1) for hosting; PostgreSQL for storage; GitLab for code and CI.
2. **Simulation only:** never place, sign or send an order to any venue, and never use venue credentials.
3. **Precision over recall:** a false "equivalent" is the worst possible failure. No change may lower measured precision at the `equivalent` tier below the target in requirements.md.
4. **Venue-agnostic routing:** the router must contain no venue-specific logic, and this is enforced by a test.
5. **LLM can only downgrade:** an LLM may confirm, downgrade or reject a rules-based relationship. It may never create or upgrade one. Public web requests never trigger an LLM call.
6. **Budget:** total spend across Anthropic and GCP must stay **≤ $50 through 2026-10-11**. Use the cheapest Anthropic model tier.
7. **Secrets:** never commit secrets. Real secrets live only in GCP Secret Manager and in local `.env` files, which are gitignored.
8. **Tests never call live or paid APIs.** Venue and LLM calls in tests use recorded fixtures or mocks.
9. **Deadline:** the deadlines above are fixed, and scope is cut in the order given in plan.md instead of slipping a date.
10. **Human-owned files** (the files listed in [operating-rules.md](operating-rules.md)) change only through an MR with the `human-approved` label.
