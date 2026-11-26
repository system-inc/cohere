# `no-duplicate-imports`

|                        |               |
| ---------------------- | ------------- |
| **Recommendation**     | **No**        |
| Violations in ahra     | **610**       |
| Plugin                 | `eslint core` |
| Auto-fixable           | no            |
| Needs type information | no            |

## What it checks

Disallow duplicate module imports

## Why this recommendation

Nearly every hit is the deliberate `import type {...}` / `import {...}` split that the in-house consistent-type-imports rule produces, as in TasksCenter.tsx lines 42-43, so it would fight an existing rule rather than find defects.

> Reviewed against real violation sites and the house conventions in `CLAUDE.md`,
> which moved this from **Maybe** to **No**. The first pass classified rules by
> correctness-versus-style crossed with violation count, and that is blind to
> whether a rule fights a convention we chose on purpose.
