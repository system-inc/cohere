# `react/forbid-foreign-prop-types`

|                        |                       |
| ---------------------- | --------------------- |
| **Recommendation**     | **Yes**               |
| Violations in ahra     | none                  |
| Plugin                 | `eslint-plugin-react` |
| Auto-fixable           | no                    |
| Needs type information | no                    |

## What it checks

Disallow using another component's propTypes

## Why this recommendation

Stylistic, but the tree already satisfies it, so enabling costs nothing and prevents drift.
