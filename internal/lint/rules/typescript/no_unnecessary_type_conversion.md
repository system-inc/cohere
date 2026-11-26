# `@typescript-eslint/no-unnecessary-type-conversion`

|                        |                      |
| ---------------------- | -------------------- |
| **Recommendation**     | **No**               |
| Violations in ahra     | **73**               |
| Plugin                 | `@typescript-eslint` |
| Auto-fixable           | no                   |
| Needs type information | yes                  |

## What it checks

Disallow conversion idioms when they do not change the type or value of the expression

## Why this recommendation

Stylistic with a cleanup cost that is not obviously worth the convention it buys.
