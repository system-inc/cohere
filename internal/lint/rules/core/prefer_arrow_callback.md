# `prefer-arrow-callback`

|                        |               |
| ---------------------- | ------------- |
| **Recommendation**     | **Strong No** |
| Violations in ahra     | **5619**      |
| Plugin                 | `eslint core` |
| Auto-fixable           | yes           |
| Needs type information | no            |

## What it checks

Require using arrow functions for callbacks

## Why this recommendation

Stylistic and enormous: the volume says the codebase has a different convention, not that it is wrong.

## What fixing looks like

Auto-fixable. `eslint --fix` rewrites these, so the cleanup is a command plus a review of the diff.
