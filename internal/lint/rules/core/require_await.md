# `require-await`

|                        |                                                                 |
| ---------------------- | --------------------------------------------------------------- |
| **Recommendation**     | **Yes, enabled**                                                |
| Violations in ahra     | **3** (cohere, 2026-10-01); ESLint reports 89                   |
| Plugin                 | `eslint core`                                                   |
| Auto-fixable           | no (a suggestion, never a fix)                                  |
| Needs type information | no upstream; cohere asks the checker for the contract exemption |

## What it checks

Disallow async functions which have no `await` expression

## Why this recommendation

Catches a defect rather than a preference. The classic catch is `items.map(async (item) => transform(item))`,
which hands back an array of promises where values were wanted.

The 637 this document used to cite were the `generateMetadata` world: Next.js route files writing
`export async function generateMetadata(): Promise<Metadata>` without awaiting. That world is gone. On
2026-10-01, 43 route files write `generateMetadata` without `async`, and none of the 22 that keep it
reports here.
