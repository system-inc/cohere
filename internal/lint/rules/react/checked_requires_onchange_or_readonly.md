# `react/checked-requires-onchange-or-readonly`

|                        |                       |
| ---------------------- | --------------------- |
| **Recommendation**     | **Yes**               |
| Violations in ahra     | none                  |
| Plugin                 | `eslint-plugin-react` |
| Auto-fixable           | no                    |
| Needs type information | no                    |

## What it checks

Enforce using `onChange` or `readonly` attribute when `checked` is used

## Why this recommendation

Stylistic, but the tree already satisfies it, so enabling costs nothing and prevents drift.
