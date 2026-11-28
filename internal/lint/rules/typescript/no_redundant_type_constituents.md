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

## What fixing looks like

Not auto-fixable. Each site needs a human decision, which is what makes the count in the table above the real cost.
