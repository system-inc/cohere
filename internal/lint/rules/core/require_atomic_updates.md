# `require-atomic-updates`

|                        |                |
| ---------------------- | -------------- |
| **Recommendation**     | **Strong Yes** |
| Violations in ahra     | **31**         |
| Plugin                 | `eslint core`  |
| Auto-fixable           | no             |
| Needs type information | no             |

## What it checks

Disallow assignments that can lead to race conditions due to usage of `await` or `yield`

## Why this recommendation

Catches a defect rather than a preference, and the cleanup is small enough to do in one pass.
