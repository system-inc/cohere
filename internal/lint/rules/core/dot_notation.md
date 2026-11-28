# `dot-notation`

|                        |               |
| ---------------------- | ------------- |
| **Recommendation**     | **No**        |
| Violations in ahra     | **121**       |
| Plugin                 | `eslint core` |
| Auto-fixable           | yes           |
| Needs type information | no            |

## What it checks

Enforce dot notation whenever possible

## Why this recommendation

Stylistic with a cleanup cost that is not obviously worth the convention it buys.

## What fixing looks like

Auto-fixable. `eslint --fix` rewrites these, so the cleanup is a command plus a review of the diff.
