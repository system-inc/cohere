# `@typescript-eslint/consistent-type-definitions`

|                        |                      |
| ---------------------- | -------------------- |
| **Recommendation**     | **No**               |
| Violations in ahra     | **872**              |
| Plugin                 | `@typescript-eslint` |
| Auto-fixable           | yes                  |
| Needs type information | no                   |

## What it checks

Enforce type definitions to consistently use either `interface` or `type`

## Why this recommendation

Stylistic with a cleanup cost that is not obviously worth the convention it buys.

## What fixing looks like

Auto-fixable. `eslint --fix` rewrites these, so the cleanup is a command plus a review of the diff.

## Status: ported 2026-09-06, registered and NOT enabled

The audit's 872 and the port's 872 agree exactly, across 42 files including three generated ones that
carry 485, 232 and 103 findings apiece. The dense files matter: a count drawn only from sparse files
can agree by accident, and these leave a gap room to show.

Registered so it compiles in and appears in `cohere --rules`; deliberately not enabled, because
enabling it means accepting 872 findings' worth of cleanup, and that is a decision about this
codebase rather than a porting step. The audit's **No** above is a judgment about that cleanup cost
and it is untouched; what has changed is only that the rule now exists to be turned on.
