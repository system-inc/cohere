# `@typescript-eslint/no-unnecessary-type-arguments`

|                        |                      |
| ---------------------- | -------------------- |
| **Recommendation**     | **No**               |
| Violations in ahra     | **79**               |
| Plugin                 | `@typescript-eslint` |
| Auto-fixable           | yes                  |
| Needs type information | yes                  |

## What it checks

Disallow type arguments that are equal to the default

## Why this recommendation

Stylistic with a cleanup cost that is not obviously worth the convention it buys.

## What fixing looks like

Auto-fixable. `eslint --fix` rewrites these, so the cleanup is a command plus a review of the diff.
