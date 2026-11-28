# `react/require-optimization`

|                        |                       |
| ---------------------- | --------------------- |
| **Recommendation**     | **Yes**               |
| Violations in ahra     | none                  |
| Plugin                 | `eslint-plugin-react` |
| Auto-fixable           | no                    |
| Needs type information | no                    |

## What it checks

Enforce React components to have a shouldComponentUpdate method

## Why this recommendation

Stylistic, but the tree already satisfies it, so enabling costs nothing and prevents drift.
