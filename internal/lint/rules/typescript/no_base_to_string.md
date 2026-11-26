# `@typescript-eslint/no-base-to-string`

|                        |                      |
| ---------------------- | -------------------- |
| **Recommendation**     | **Yes**              |
| Violations in ahra     | **53**               |
| Plugin                 | `@typescript-eslint` |
| Auto-fixable           | no                   |
| Needs type information | yes                  |

## What it checks

Require `.toString()` and `.toLocaleString()` to only be called on objects which provide useful information when stringified

## Why this recommendation

Real signal, but it is explicitly disabled in VerifySettings.json (`typescript/no-base-to-string: off`) and several sites are deliberate `String(value)` fallbacks in generic serializers like Csv.ts/Tsv.ts that already branch on typeof object.

> Reviewed against real violation sites and the house conventions in `CLAUDE.md`,
> which moved this from **Strong Yes** to **Yes**. The first pass classified rules by
> correctness-versus-style crossed with violation count, and that is blind to
> whether a rule fights a convention we chose on purpose.

## What fixing looks like

Not auto-fixable. Each site needs a human decision, which is what makes the count in the table above the real cost.
