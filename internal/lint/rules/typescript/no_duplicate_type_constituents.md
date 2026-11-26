# `@typescript-eslint/no-duplicate-type-constituents`

|                        |                      |
| ---------------------- | -------------------- |
| **Recommendation**     | **Yes**              |
| Violations in ahra     | **3**                |
| Plugin                 | `@typescript-eslint` |
| Auto-fixable           | yes                  |
| Needs type information | yes                  |

## What it checks

Disallow duplicate constituents of union or intersection types

## Why this recommendation

Only 3 sites and one is in generated code (GraphQlOperations.ts), so it is autofixable housekeeping rather than a defect worth top priority.

> Reviewed against real violation sites and the house conventions in `CLAUDE.md`,
> which moved this from **Strong Yes** to **Yes**. The first pass classified rules by
> correctness-versus-style crossed with violation count, and that is blind to
> whether a rule fights a convention we chose on purpose.
