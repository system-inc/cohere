# `react/iframe-missing-sandbox`

| | |
|---|---|
| **Recommendation** | **Yes** |
| Violations in ahra | none |
| Plugin | `eslint-plugin-react` |
| Auto-fixable | no |
| Needs type information | yes |

## What it checks

Enforce sandbox attribute on iframe elements

## Where cohere differs from upstream

Upstream reports `allow-scripts allow-same-origin` on every iframe. The escape it warns about needs a framed document in the page's own origin, so cohere reports the pair only when the frame is provably same-origin: a relative or absent `src`, an `about:`, `blob:` or `javascript:` URL, or any `srcDoc`. A cross-origin embed such as a YouTube player stays silent, as do values it cannot read. A template is read from its head, and any other value from its type's literal constituents.

## Why this recommendation

Stylistic, but the tree already satisfies it, so enabling costs nothing and prevents drift.

## Violations

None. The tree already satisfies this rule, so enabling it is a guardrail against future drift rather than a cleanup.

