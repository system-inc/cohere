# `no-div-regex`

|                        |                |
| ---------------------- | -------------- |
| **Recommendation**     | **Strong Yes** |
| Violations in ahra     | **1**          |
| Plugin                 | `eslint core`  |
| Auto-fixable           | yes            |
| Needs type information | no             |

## What it checks

Disallow equal signs explicitly at the beginning of regular expressions

## Why this recommendation

Catches a defect rather than a preference, and the cleanup is small enough to do in one pass.

## What fixing looks like

Auto-fixable. `eslint --fix` rewrites these, so the cleanup is a command plus a review of the diff.
