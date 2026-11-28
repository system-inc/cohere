# `react/jsx-no-leaked-render`

|                        |                       |
| ---------------------- | --------------------- |
| **Recommendation**     | **No**                |
| Violations in ahra     | **428**               |
| Plugin                 | `eslint-plugin-react` |
| Auto-fixable           | yes                   |
| Needs type information | no                    |

## What it checks

Disallow problematic leaked values from being rendered

## Why this recommendation

Stylistic with a cleanup cost that is not obviously worth the convention it buys.

## What fixing looks like

Auto-fixable. `eslint --fix` rewrites these, so the cleanup is a command plus a review of the diff.
