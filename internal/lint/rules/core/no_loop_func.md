# `no-loop-func`

|                        |                |
| ---------------------- | -------------- |
| **Recommendation**     | **Strong Yes** |
| Violations in ahra     | **10**         |
| Plugin                 | `eslint core`  |
| Auto-fixable           | no             |
| Needs type information | no             |

## What it checks

Disallow function declarations that contain unsafe references inside loop statements

## Why this recommendation

Catches a defect rather than a preference, and the cleanup is small enough to do in one pass.
