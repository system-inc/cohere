# `@typescript-eslint/no-redundant-type-constituents`

|                        |                      |
| ---------------------- | -------------------- |
| **Recommendation**     | **Maybe**            |
| Violations in ahra     | **21**               |
| Plugin                 | `@typescript-eslint` |
| Auto-fixable           | no                   |
| Needs type information | yes                  |

## What it checks

Disallow members of unions and intersections that do nothing or override type information

## Why this recommendation

Stylistic. Small enough to adopt if we want the convention, not urgent if we do not.
