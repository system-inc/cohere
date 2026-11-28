# `react/jsx-sort-props`

|                        |                       |
| ---------------------- | --------------------- |
| **Recommendation**     | **No**                |
| Violations in ahra     | **8336**              |
| Plugin                 | `eslint-plugin-react` |
| Auto-fixable           | yes                   |
| Needs type information | no                    |

## What it checks

Enforce props alphabetical sorting

## Why this recommendation

Overlaps a rule we already enforce in-house, so it would report the same defect under a second name.

## What fixing looks like

Auto-fixable. `eslint --fix` rewrites these, so the cleanup is a command plus a review of the diff.
