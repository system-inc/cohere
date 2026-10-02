# `@typescript-eslint/no-misused-spread`

| | |
|---|---|
| **Recommendation** | **Strong Yes** |
| Violations in ahra | **2** (measured 2026-10-01, after dropping the string branch; the gate adds the four string sites below) |
| Plugin | `@typescript-eslint` |
| Auto-fixable | no |
| Needs type information | yes |

## What it checks

Disallow using the spread operator when it might cause unexpected behavior

## Deliberate divergence: no string branch

Upstream also reports a string spread into an array or a call (`noStringSpread`) and asks for
`Intl.Segmenter`, because spread yields code points rather than the graphemes a reader sees. Cohere
drops that branch, by Kirk's ruling of 2026-10-01 (review option C). Whether a string should be
walked by code point or by grapheme is intent the rule cannot see; spread is the correct code-point
iteration, and upstream's only repair, `Array.from(text)`, is the same iteration under another name,
so every finding either launders or is wrong. The object cascade (Promise, function, Map, array,
iterable, class instance, class declaration), which catches silent data loss, is unchanged.

So cohere is silent where the gate reports, on the four string sites ahra had:

| site | why the code wants code points |
|---|---|
| `libraries/structure/libraries/nexus/source/geography/Countries.ts:13` | `ISO` letters mapped to regional indicators |
| `modules/openai/PngTextMetadata.ts:64` | a Latin-1 filter |
| `modules/pensieve/PensieveDailies.ts:288` | quote-mark scanning |
| `modules/pensieve/PensieveDailies.ts:326` | ignored characters filtered out |

Each is a gate-side entry in `internal/differential/acknowledged.go`. The fixture is
`TestNoMisusedSpreadLeavesStringSpreadAlone`: upstream invalid 0 through 13 verbatim, the four ahra
sites, the constructor shape, and a control proving the object cascade still reports in the same
harness. The `allow` option is kept for parity of the option surface; nothing in ahra sets it.

## Why this recommendation

Catches a defect rather than a preference, and the cleanup is small enough to do in one pass.

## Violations

5 in the tree. Showing the first few.

**`libraries/structure/libraries/nexus/source/security/secrets/Secret.test.ts:70`**

```
const spread = { ...secret };
```

> Using the spread operator on class instances will lose their class prototype

**`libraries/structure/source/modules/account/hooks/useAccount.ts:106`**

```
...previousAccountState.data,
```

> Using the spread operator on class instances will lose their class prototype

**`libraries/structure/source/services/network/NetworkService.ts:359`**

```
...options?.headers,
```

> Using the spread operator on an array in an object will result in a list of indices

### What fixing looks like

Not auto-fixable. Each site needs a human decision, which is what makes the count above the real cost.

