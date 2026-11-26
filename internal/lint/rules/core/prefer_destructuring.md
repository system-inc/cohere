# `prefer-destructuring`

|                        |               |
| ---------------------- | ------------- |
| **Recommendation**     | **No**        |
| Violations in ahra     | **789**       |
| Plugin                 | `eslint core` |
| Auto-fixable           | yes           |
| Needs type information | no            |

## What it checks

Require destructuring from arrays and/or objects

## Why this recommendation

Stylistic with a cleanup cost that is not obviously worth the convention it buys.

## What fixing looks like

Auto-fixable. `eslint --fix` rewrites these, so the cleanup is a command plus a review of the diff.
