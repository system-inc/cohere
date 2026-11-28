# `@typescript-eslint/no-unsafe-enum-comparison`

|                        |                      |
| ---------------------- | -------------------- |
| **Recommendation**     | **Strong No**        |
| Violations in ahra     | **508**              |
| Plugin                 | `@typescript-eslint` |
| Auto-fixable           | no                   |
| Needs type information | yes                  |

## What it checks

Disallow comparing an enum value with a non-enum value

## Why this recommendation

The codebase declares zero `enum`s, and every sampled violation compares TSESTree AST_NODE_TYPES string unions in the house lint rules, so all 508 hits are false positives.

> Reviewed against real violation sites and the house conventions in `CLAUDE.md`,
> which moved this from **Maybe** to **Strong No**. The first pass classified rules by
> correctness-versus-style crossed with violation count, and that is blind to
> whether a rule fights a convention we chose on purpose.

## What fixing looks like

Not auto-fixable. Each site needs a human decision, which is what makes the count in the table above the real cost.
