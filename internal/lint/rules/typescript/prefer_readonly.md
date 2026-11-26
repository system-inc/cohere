# `@typescript-eslint/prefer-readonly`

|                        |                      |
| ---------------------- | -------------------- |
| **Recommendation**     | **No**               |
| Violations in ahra     | **253**              |
| Plugin                 | `@typescript-eslint` |
| Auto-fixable           | yes                  |
| Needs type information | yes                  |

## What it checks

Require private members to be marked as `readonly` if they're never modified outside of the constructor

## Why this recommendation

Stylistic with a cleanup cost that is not obviously worth the convention it buys.
