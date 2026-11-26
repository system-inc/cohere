# `react/prefer-read-only-props`

|                        |                       |
| ---------------------- | --------------------- |
| **Recommendation**     | **No**                |
| Violations in ahra     | **364**               |
| Plugin                 | `eslint-plugin-react` |
| Auto-fixable           | yes                   |
| Needs type information | no                    |

## What it checks

Enforce that props are read-only

## Why this recommendation

Stylistic with a cleanup cost that is not obviously worth the convention it buys.

## What fixing looks like

Auto-fixable. `eslint --fix` rewrites these, so the cleanup is a command plus a review of the diff.
