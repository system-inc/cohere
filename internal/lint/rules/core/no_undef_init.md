# `no-undef-init`

|                        |                |
| ---------------------- | -------------- |
| **Recommendation**     | **Strong Yes** |
| Violations in ahra     | **17**         |
| Plugin                 | `eslint core`  |
| Auto-fixable           | yes            |
| Needs type information | no             |

## What it checks

Disallow initializing variables to `undefined`

## Why this recommendation

Catches a defect rather than a preference, and the cleanup is small enough to do in one pass.
