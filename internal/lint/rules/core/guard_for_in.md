# `guard-for-in`

|                        |                |
| ---------------------- | -------------- |
| **Recommendation**     | **Strong Yes** |
| Violations in ahra     | **4**          |
| Plugin                 | `eslint core`  |
| Auto-fixable           | no             |
| Needs type information | no             |

## What it checks

Require `for-in` loops to include an `if` statement

## Why this recommendation

Catches a defect rather than a preference, and the cleanup is small enough to do in one pass.
