# `@typescript-eslint/prefer-promise-reject-errors`

|                        |                      |
| ---------------------- | -------------------- |
| **Recommendation**     | **Strong Yes**       |
| Violations in ahra     | **7**                |
| Plugin                 | `@typescript-eslint` |
| Auto-fixable           | no                   |
| Needs type information | yes                  |

## What it checks

Require using Error objects as Promise rejection reasons

## Why this recommendation

Catches a defect rather than a preference, and the cleanup is small enough to do in one pass.
