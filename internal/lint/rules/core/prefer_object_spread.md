# `prefer-object-spread`

|                        |               |
| ---------------------- | ------------- |
| **Recommendation**     | **Yes**       |
| Violations in ahra     | none          |
| Plugin                 | `eslint core` |
| Auto-fixable           | yes           |
| Needs type information | no            |

## What it checks

Disallow using `Object.assign` with an object literal as the first argument and prefer the use of object spread instead

## Why this recommendation

Stylistic, but the tree already satisfies it, so enabling costs nothing and prevents drift.
