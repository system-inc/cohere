# `@typescript-eslint/no-useless-empty-export`

|                        |                      |
| ---------------------- | -------------------- |
| **Recommendation**     | **Yes**              |
| Violations in ahra     | none                 |
| Plugin                 | `@typescript-eslint` |
| Auto-fixable           | yes                  |
| Needs type information | no                   |

## What it checks

Disallow empty exports that don't change anything in a module file

## Why this recommendation

Stylistic, but the tree already satisfies it, so enabling costs nothing and prevents drift.
