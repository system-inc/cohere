# `react/no-object-type-as-default-prop`

|                        |                       |
| ---------------------- | --------------------- |
| **Recommendation**     | **Yes**               |
| Violations in ahra     | none                  |
| Plugin                 | `eslint-plugin-react` |
| Auto-fixable           | no                    |
| Needs type information | no                    |

## What it checks

Disallow usage of referential-type variables as default param in functional component

## Why this recommendation

Stylistic, but the tree already satisfies it, so enabling costs nothing and prevents drift.
