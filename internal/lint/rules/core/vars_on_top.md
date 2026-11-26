# `vars-on-top`

|                        |               |
| ---------------------- | ------------- |
| **Recommendation**     | **Yes**       |
| Violations in ahra     | none          |
| Plugin                 | `eslint core` |
| Auto-fixable           | no            |
| Needs type information | no            |

## What it checks

Require `var` declarations be placed at the top of their containing scope

## Why this recommendation

Stylistic, but the tree already satisfies it, so enabling costs nothing and prevents drift.
