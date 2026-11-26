# `no-promise-executor-return`

|                        |               |
| ---------------------- | ------------- |
| **Recommendation**     | **Yes**       |
| Violations in ahra     | **61**        |
| Plugin                 | `eslint core` |
| Auto-fixable           | no            |
| Needs type information | no            |

## What it checks

Disallow returning values from Promise executor functions

## Why this recommendation

Catches a defect rather than a preference. The cleanup is real but bounded.
