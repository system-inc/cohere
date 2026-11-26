# `no-alert`

|                        |                |
| ---------------------- | -------------- |
| **Recommendation**     | **Strong Yes** |
| Violations in ahra     | **6**          |
| Plugin                 | `eslint core`  |
| Auto-fixable           | no             |
| Needs type information | no             |

## What it checks

Disallow the use of `alert`, `confirm`, and `prompt`

## Why this recommendation

Catches a defect rather than a preference, and the cleanup is small enough to do in one pass.
