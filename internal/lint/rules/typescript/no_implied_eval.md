# `@typescript-eslint/no-implied-eval`

|                        |                      |
| ---------------------- | -------------------- |
| **Recommendation**     | **Strong Yes**       |
| Violations in ahra     | none                 |
| Plugin                 | `@typescript-eslint` |
| Auto-fixable           | no                   |
| Needs type information | yes                  |

## What it checks

Disallow the use of `eval()`-like functions

## Why this recommendation

Catches a defect rather than a preference, and the tree is already clean, so it is a guardrail bought for nothing.
