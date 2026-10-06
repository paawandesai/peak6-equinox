<!-- OWNER: shared. Operator may update to match reality; every change is logged in docs/decisions.md. -->
# Review instructions (Cursor reviewer)

Review every MR against the ticket it names. Read [CLAUDE.md](../CLAUDE.md), [brief.md § Hard constraints](brief.md#hard-constraints), [architecture.md](architecture.md) and [decisions.md](decisions.md) first.

## Check

1. **Acceptance criteria:** each AC in the ticket is met and evidenced, with a command, a test name or a screenshot.
2. **Tests are meaningful:** they assert behaviour, edge cases and failure paths, not just that the code runs. Parsers, prices, fees and the book walk have property tests. External calls are mocked or use fixtures, never live.
3. **Consistency:**
   - Package boundaries and import rules match architecture.md (`route` imports nothing venue-specific).
   - Types follow the canonical contracts (decimal prices in [0,1], UTC times, non-empty explanations).
   - Choices match decisions.md, or a new `D-` entry justifies the change.
4. **Security:**
   - No secrets.
   - External data is validated and size-capped.
   - SQL is parameterised.
   - HTML is escaped.
   - Admin routes are behind auth.
   - No LLM path is reachable from public routes.
   - Logs are redacted.
5. **Hard constraints:** none violated. Pay particular attention to precision-first matching, downgrade-only LLM, simulation only, and the budget.

## Reject the MR if it

- Doesn't trace to a requirement (no FR, NFR or US ID in the ticket or MR).
- Implements behaviour no requirement asks for.
- Deletes, skips or weakens tests without a logged decision.
- Touches an attack surface listed in [threat-model.md](threat-model.md) without tests in `tests/security/` for the relevant AB IDs.
- Modifies a human-owned file without the `human-approved` label.

## Severity

| Level | Meaning | Blocks merge? |
|---|---|---|
| critical | A hard-constraint violation, a secret, a security hole, or a wrong financial or equivalence result | yes |
| major | An AC is unmet, tests are missing or meaningless, or architecture or a decision is contradicted | yes |
| minor | Correctness risk in an edge case, unclear explanation text, a missing log | no (fix in this MR if cheap, otherwise file a follow-up) |
| nit | Style, naming, formatting | **never** |

## Output format

```
Verdict: APPROVE | REQUEST_CHANGES
Ticket: M1-03   Satisfies checked: FR-01, NFR-08, AB-02
Findings:
1. [critical] internal/route/route.go:42 Imports internal/venues/kalshi (violates D-028). Fix: use canonical.FeeSchedule.
2. [minor] …
```

Any critical or major finding means `REQUEST_CHANGES`.
