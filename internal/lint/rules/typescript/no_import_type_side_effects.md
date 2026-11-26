# `@typescript-eslint/no-import-type-side-effects`

|                        |                      |
| ---------------------- | -------------------- |
| **Recommendation**     | **Strong Yes**       |
| Violations in ahra     | **54**               |
| Plugin                 | `@typescript-eslint` |
| Auto-fixable           | yes                  |
| Needs type information | no                   |

## What it checks

Enforce the use of top-level import type qualifier when an import only has specifiers with inline type qualifiers

## Why this recommendation

It directly reinforces the already-configured `consistent-type-imports` with `fixStyle: separate-type-imports`; 2304 imports already use `import type {` and only 59 inline stragglers remain, so it is an autofixable finisher for a convention already chosen.

> Reviewed against real violation sites and the house conventions in `CLAUDE.md`,
> which moved this from **Maybe** to **Strong Yes**. The first pass classified rules by
> correctness-versus-style crossed with violation count, and that is blind to
> whether a rule fights a convention we chose on purpose.
