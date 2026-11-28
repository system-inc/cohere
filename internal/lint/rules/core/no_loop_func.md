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

## What fixing looks like

Not auto-fixable. Each site needs a human decision, which is what makes the count in the table above the real cost.
