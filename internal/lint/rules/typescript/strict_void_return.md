# `@typescript-eslint/strict-void-return`

|                        |                      |
| ---------------------- | -------------------- |
| **Recommendation**     | **Yes**              |
| Violations in ahra     | **85**               |
| Plugin                 | `@typescript-eslint` |
| Auto-fixable           | no                   |
| Needs type information | yes                  |

## What it checks

Disallow passing a value-returning function in a position accepting a void function

## Why this recommendation

Catches a defect rather than a preference. The cleanup is real but bounded.
