# `@typescript-eslint/no-unnecessary-boolean-literal-compare`

|                        |                      |
| ---------------------- | -------------------- |
| **Recommendation**     | **Maybe**            |
| Violations in ahra     | **33**               |
| Plugin                 | `@typescript-eslint` |
| Auto-fixable           | yes                  |
| Needs type information | yes                  |

## What it checks

Disallow unnecessary equality comparisons against boolean literals

## Why this recommendation

Stylistic. Small enough to adopt if we want the convention, not urgent if we do not.

## What fixing looks like

Auto-fixable. `eslint --fix` rewrites these, so the cleanup is a command plus a review of the diff.
