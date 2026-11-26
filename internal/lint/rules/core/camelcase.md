# `camelcase`

|                        |               |
| ---------------------- | ------------- |
| **Recommendation**     | **Strong No** |
| Violations in ahra     | **1208**      |
| Plugin                 | `eslint core` |
| Auto-fixable           | no            |
| Needs type information | no            |

## What it checks

Enforce camelcase naming convention

## Why this recommendation

Beyond overlapping the in-house naming rules, the violations are snake_case GraphQL and REST wire keys such as `before_direct` in useMessagesFeedRequest.ts, which must match the upstream API spelling per the Module-Scope Constants convention on values that cross a boundary.

> Reviewed against real violation sites and the house conventions in `CLAUDE.md`,
> which moved this from **No** to **Strong No**. The first pass classified rules by
> correctness-versus-style crossed with violation count, and that is blind to
> whether a rule fights a convention we chose on purpose.
