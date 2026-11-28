# `@typescript-eslint/explicit-member-accessibility`

|                        |                      |
| ---------------------- | -------------------- |
| **Recommendation**     | **No**               |
| Violations in ahra     | **1942**             |
| Plugin                 | `@typescript-eslint` |
| Auto-fixable           | yes                  |
| Needs type information | no                   |

## What it checks

Require explicit accessibility modifiers on class properties and methods

## Why this recommendation

Stylistic with a cleanup cost that is not obviously worth the convention it buys.

## What fixing looks like

Auto-fixable. `eslint --fix` rewrites these, so the cleanup is a command plus a review of the diff.
