# `no-template-curly-in-string`

|                        |                |
| ---------------------- | -------------- |
| **Recommendation**     | **Strong Yes** |
| Violations in ahra     | **2**          |
| Plugin                 | `eslint core`  |
| Auto-fixable           | no             |
| Needs type information | no             |

## What it checks

Disallow template literal placeholder syntax in regular strings

## Why this recommendation

Catches a defect rather than a preference, and the cleanup is small enough to do in one pass.
