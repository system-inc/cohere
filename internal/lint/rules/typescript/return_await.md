# `@typescript-eslint/return-await`

|                        |                      |
| ---------------------- | -------------------- |
| **Recommendation**     | **Maybe**            |
| Violations in ahra     | **32**               |
| Plugin                 | `@typescript-eslint` |
| Auto-fixable           | yes                  |
| Needs type information | yes                  |

## What it checks

Enforce consistent awaiting of returned promises

## Why this recommendation

Stylistic. Small enough to adopt if we want the convention, not urgent if we do not.

## What fixing looks like

Auto-fixable. `eslint --fix` rewrites these, so the cleanup is a command plus a review of the diff.
