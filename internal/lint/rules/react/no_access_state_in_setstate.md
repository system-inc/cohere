# `react/no-access-state-in-setstate`

|                        |                       |
| ---------------------- | --------------------- |
| **Recommendation**     | **Yes**               |
| Violations in ahra     | none                  |
| Plugin                 | `eslint-plugin-react` |
| Auto-fixable           | no                    |
| Needs type information | no                    |

## What it checks

Disallow when this.state is accessed within setState

## Why this recommendation

Stylistic, but the tree already satisfies it, so enabling costs nothing and prevents drift.
