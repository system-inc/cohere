# `@typescript-eslint/prefer-regexp-exec`

|                        |                      |
| ---------------------- | -------------------- |
| **Recommendation**     | **No**               |
| Violations in ahra     | **170**              |
| Plugin                 | `@typescript-eslint` |
| Auto-fixable           | yes                  |
| Needs type information | yes                  |

## What it checks

Enforce `RegExp#exec` over `String#match` if no global flag is provided

## Why this recommendation

Stylistic with a cleanup cost that is not obviously worth the convention it buys.

## What fixing looks like

Auto-fixable. `eslint --fix` rewrites these, so the cleanup is a command plus a review of the diff.

## Status: ported 2026-09-06, registered and NOT enabled

The audit's 170 and the port's 179 differ, and the port is the one that matches the installed rule:
driving 8.67.0 over the same 87 files gives 179, agreeing with cohere on all 179 positions. The audit
number was measured with inline disable comments active and against a differently-scoped file set.

Getting there took a correction worth recording, because the corpus could not see it. A first draft
resolved a bound identifier and then asked "could this construction carry a `g`", which agreed with
all 37 corpus rows and over-reported 8 real sites, every one
`const r = new RegExp(templateWithSubstitution)`. Upstream instead EVALUATES the reference and
requires a real value back, so a construction from a runtime pattern is unreportable while the same
construction written inline is reportable. A second correction followed the same way: the fold
requires the binding to be written exactly once, not to be declared `const`.

Registered so it compiles in and appears in `cohere --rules`; deliberately not enabled, because
enabling it means accepting 179 findings' worth of cleanup, and that is a decision about this
codebase rather than a porting step. The audit's **No** above is a judgment about that cleanup cost
and it is untouched; what has changed is only that the rule now exists to be turned on.
