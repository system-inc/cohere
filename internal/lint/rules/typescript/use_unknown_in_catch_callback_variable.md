# `@typescript-eslint/use-unknown-in-catch-callback-variable`

|                        |                      |
| ---------------------- | -------------------- |
| **Recommendation**     | **Strong Yes**       |
| Violations in ahra     | **39**               |
| Plugin                 | `@typescript-eslint` |
| Auto-fixable           | no                   |
| Needs type information | yes                  |

## What it checks

Enforce typing arguments in Promise rejection callbacks as `unknown`

## Why this recommendation

Catches a defect rather than a preference, and the cleanup is small enough to do in one pass.
