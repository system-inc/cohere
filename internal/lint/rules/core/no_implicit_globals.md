# `no-implicit-globals`

|                        |               |
| ---------------------- | ------------- |
| **Recommendation**     | **Yes**       |
| Violations in ahra     | none          |
| Plugin                 | `eslint core` |
| Auto-fixable           | no            |
| Needs type information | no            |

## What it checks

Disallow declarations in the global scope

## Why this recommendation

Stylistic, but the tree already satisfies it, so enabling costs nothing and prevents drift.
