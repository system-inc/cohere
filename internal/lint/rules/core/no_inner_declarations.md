# `no-inner-declarations`

|                        |               |
| ---------------------- | ------------- |
| **Recommendation**     | **Yes**       |
| Violations in ahra     | none          |
| Plugin                 | `eslint core` |
| Auto-fixable           | no            |
| Needs type information | no            |

## What it checks

Disallow variable or `function` declarations in nested blocks

## Why this recommendation

Stylistic, but the tree already satisfies it, so enabling costs nothing and prevents drift.
