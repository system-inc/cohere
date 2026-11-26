# `@typescript-eslint/require-await`

|                        |                                                        |
| ---------------------- | ------------------------------------------------------ |
| **Recommendation**     | **No**                                                 |
| Violations in ahra     | **436** when audited; not re-measured since, see below |
| Plugin                 | `@typescript-eslint`                                   |
| Auto-fixable           | no                                                     |
| Needs type information | yes                                                    |

## What it checks

Disallow async functions which do not return promises and have no `await` expression

## Why this recommendation

All 6 samples are `export async function generateMetadata(): Promise<Metadata>`, a Next.js framework signature contract present in 65 route files that must stay async whether or not it awaits.

> Reviewed against real violation sites and the house conventions in `CLAUDE.md`,
> which moved this from **Yes** to **No**. The first pass classified rules by
> correctness-versus-style crossed with violation count, and that is blind to
> whether a rule fights a convention we chose on purpose.

## What fixing looks like

Not auto-fixable. Each site needs a human decision, which is what makes the count in the table above the real cost.
