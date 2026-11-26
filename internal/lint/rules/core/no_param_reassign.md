# `no-param-reassign`

|                        |                |
| ---------------------- | -------------- |
| **Recommendation**     | **Strong Yes** |
| Violations in ahra     | **42**         |
| Plugin                 | `eslint core`  |
| Auto-fixable           | no             |
| Needs type information | no             |

## What it checks

Disallow reassigning function parameters

## Why this recommendation

Catches a defect rather than a preference, and the cleanup is small enough to do in one pass.
