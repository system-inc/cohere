# `no-octal-escape`

|                        |                |
| ---------------------- | -------------- |
| **Recommendation**     | **Strong Yes** |
| Violations in ahra     | none           |
| Plugin                 | `eslint core`  |
| Auto-fixable           | no             |
| Needs type information | no             |

## What it checks

Disallow octal escape sequences in string literals

## Why this recommendation

Catches a defect rather than a preference, and the tree is already clean, so it is a guardrail bought for nothing.
