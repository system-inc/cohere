# `react/jsx-no-literals`

|                        |                       |
| ---------------------- | --------------------- |
| **Recommendation**     | **No**                |
| Violations in ahra     | **1785**              |
| Plugin                 | `eslint-plugin-react` |
| Auto-fixable           | no                    |
| Needs type information | no                    |

## What it checks

Disallow usage of string literals in JSX

## Why this recommendation

Overlaps a rule we already enforce in-house, so it would report the same defect under a second name.
