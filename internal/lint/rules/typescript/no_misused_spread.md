# `@typescript-eslint/no-misused-spread`

| | |
|---|---|
| **Recommendation** | **Strong Yes** |
| Violations in ahra | **5** |
| Plugin | `@typescript-eslint` |
| Auto-fixable | no |
| Needs type information | yes |

## What it checks

Disallow using the spread operator when it might cause unexpected behavior

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

