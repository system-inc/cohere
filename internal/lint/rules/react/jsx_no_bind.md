# `react/jsx-no-bind`

|                        |                       |
| ---------------------- | --------------------- |
| **Recommendation**     | **No**                |
| Violations in ahra     | **1063**              |
| Plugin                 | `eslint-plugin-react` |
| Auto-fixable           | no                    |
| Needs type information | no                    |

## What it checks

Disallow `.bind()` or arrow functions in JSX props

## Why this recommendation

Overlaps a rule we already enforce in-house, so it would report the same defect under a second name.
