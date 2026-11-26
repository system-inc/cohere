# `@typescript-eslint/no-unnecessary-type-assertion`

|                        |                      |
| ---------------------- | -------------------- |
| **Recommendation**     | **No**               |
| Violations in ahra     | **475**              |
| Plugin                 | `@typescript-eslint` |
| Auto-fixable           | yes                  |
| Needs type information | yes                  |

## What it checks

Disallow type assertions that do not change the type of an expression

## Why this recommendation

Stylistic with a cleanup cost that is not obviously worth the convention it buys.
