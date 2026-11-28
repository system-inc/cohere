# `react/no-arrow-function-lifecycle`

|                        |                       |
| ---------------------- | --------------------- |
| **Recommendation**     | **Yes**               |
| Violations in ahra     | none                  |
| Plugin                 | `eslint-plugin-react` |
| Auto-fixable           | yes                   |
| Needs type information | no                    |

## What it checks

Lifecycle methods should be methods on the prototype, not class fields

## Why this recommendation

Stylistic, but the tree already satisfies it, so enabling costs nothing and prevents drift.
