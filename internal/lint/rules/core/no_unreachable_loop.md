# `no-unreachable-loop`

|                        |                |
| ---------------------- | -------------- |
| **Recommendation**     | **Strong Yes** |
| Violations in ahra     | **1**          |
| Plugin                 | `eslint core`  |
| Auto-fixable           | no             |
| Needs type information | no             |

## What it checks

Disallow loops with a body that allows only one iteration

## Why this recommendation

Catches a defect rather than a preference, and the cleanup is small enough to do in one pass.
