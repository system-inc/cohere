# `yoda`

|                        |               |
| ---------------------- | ------------- |
| **Recommendation**     | **Maybe**     |
| Violations in ahra     | **7**         |
| Plugin                 | `eslint core` |
| Auto-fixable           | yes           |
| Needs type information | no            |

## What it checks

Require or disallow "Yoda" conditions

## Why this recommendation

Stylistic. Small enough to adopt if we want the convention, not urgent if we do not.

## What fixing looks like

Auto-fixable. `eslint --fix` rewrites these, so the cleanup is a command plus a review of the diff.
