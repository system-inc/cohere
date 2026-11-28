# `@typescript-eslint/prefer-includes`

|                        |                      |
| ---------------------- | -------------------- |
| **Recommendation**     | **Maybe**            |
| Violations in ahra     | **30**               |
| Plugin                 | `@typescript-eslint` |
| Auto-fixable           | yes                  |
| Needs type information | yes                  |

## What it checks

Enforce `includes` method over `indexOf` method

## Why this recommendation

Stylistic. Small enough to adopt if we want the convention, not urgent if we do not.

## What fixing looks like

Auto-fixable. `eslint --fix` rewrites these, so the cleanup is a command plus a review of the diff.
