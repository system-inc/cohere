# `react/jsx-boolean-value`

|                        |                       |
| ---------------------- | --------------------- |
| **Recommendation**     | **No**                |
| Violations in ahra     | **101**               |
| Plugin                 | `eslint-plugin-react` |
| Auto-fixable           | yes                   |
| Needs type information | no                    |

## What it checks

Enforce boolean attributes notation in JSX

## Why this recommendation

Stylistic with a cleanup cost that is not obviously worth the convention it buys.

## What fixing looks like

Auto-fixable. `eslint --fix` rewrites these, so the cleanup is a command plus a review of the diff.
