# `no-extra-bind`

|                        |                |
| ---------------------- | -------------- |
| **Recommendation**     | **Strong Yes** |
| Violations in ahra     | none           |
| Plugin                 | `eslint core`  |
| Auto-fixable           | yes            |
| Needs type information | no             |

## What it checks

Disallow unnecessary calls to `.bind()`

## Why this recommendation

Catches a defect rather than a preference, and the tree is already clean, so it is a guardrail bought for nothing.
