# `@typescript-eslint/no-unsafe-return`

|                        |                      |
| ---------------------- | -------------------- |
| **Recommendation**     | **Yes**              |
| Violations in ahra     | **52**               |
| Plugin                 | `@typescript-eslint` |
| Auto-fixable           | no                   |
| Needs type information | yes                  |

## What it checks

Disallow returning a value with type `any` from a function

## Why this recommendation

4 of 6 sampled sites are the same @cloudflare/workers-types `Buffer: any` artifact; the genuine signal is the JSON.parse returns in Structure.ts/StructureDoctor.ts, which is a smaller real cleanup than 52 suggests.

> Reviewed against real violation sites and the house conventions in `CLAUDE.md`,
> which moved this from **Strong Yes** to **Yes**. The first pass classified rules by
> correctness-versus-style crossed with violation count, and that is blind to
> whether a rule fights a convention we chose on purpose.
