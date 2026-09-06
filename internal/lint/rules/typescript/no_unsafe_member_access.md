# `@typescript-eslint/no-unsafe-member-access`

| | |
|---|---|
| **Recommendation** | **Maybe** |
| Violations in ahra | **395** |
| Plugin | `@typescript-eslint` |
| Auto-fixable | no |
| Needs type information | yes |

## What it checks

Disallow member access on a value with type `any`

## Why this recommendation

5 of 6 sampled sites are `.from`/`.toString` on the `Buffer: any` global injected by @cloudflare/workers-types, so the count measures a broken ambient type, not unsafe code.

> Reviewed against real violation sites and the house conventions in `CLAUDE.md`,
> which moved this from **Yes** to **Maybe**. The first pass classified rules by
> correctness-versus-style crossed with violation count, and that is blind to
> whether a rule fights a convention we chose on purpose.

## Violations

395 in the tree. Showing the first few.

**`app/api/finance/transaction/[transactionId]/attachments/route.ts:42`**

```
const originalBytes = Buffer.from(await file.arrayBuffer());
```

> Unsafe member access .from on an `any` value

**`app/api/spotify/callback/route.ts:36`**

```
const basic = Buffer.from(`${creds.clientId}:${creds.clientSecret}`).toString('base64');
```

> Unsafe member access .from on an `any` value

**`app/api/spotify/callback/route.ts:36`**

```
const basic = Buffer.from(`${creds.clientId}:${creds.clientSecret}`).toString('base64');
```

> Unsafe member access .clientId on an `any` value

**`app/api/spotify/callback/route.ts:36`**

```
const basic = Buffer.from(`${creds.clientId}:${creds.clientSecret}`).toString('base64');
```

> Unsafe member access .clientSecret on an `any` value

**`app/api/spotify/callback/route.ts:36`**

```
const basic = Buffer.from(`${creds.clientId}:${creds.clientSecret}`).toString('base64');
```

> Unsafe member access .toString on an `any` value

### What fixing looks like

Not auto-fixable. Each site needs a human decision, which is what makes the count above the real cost.

