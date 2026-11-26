# `one-var`

|                        |               |
| ---------------------- | ------------- |
| **Recommendation**     | **Strong No** |
| Violations in ahra     | **24698**     |
| Plugin                 | `eslint core` |
| Auto-fixable           | yes           |
| Needs type information | no            |

## What it checks

Enforce variables to be declared either together or separately in functions

## Why this recommendation

Stylistic and enormous: the volume says the codebase has a different convention, not that it is wrong.

## What fixing looks like

Auto-fixable. `eslint --fix` rewrites these, so the cleanup is a command plus a review of the diff.
