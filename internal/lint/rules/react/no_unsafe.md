# `react/no-unsafe`

|                        |                       |
| ---------------------- | --------------------- |
| **Recommendation**     | **Strong Yes**        |
| Violations in ahra     | none                  |
| Plugin                 | `eslint-plugin-react` |
| Auto-fixable           | no                    |
| Needs type information | no                    |

## What it checks

Disallow usage of unsafe lifecycle methods

## Why this recommendation

Catches a defect rather than a preference, and the tree is already clean, so it is a guardrail bought for nothing.
