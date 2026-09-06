# `@typescript-eslint/no-unused-private-class-members`

| | |
|---|---|
| **Recommendation** | **Strong Yes** |
| Violations in ahra | **4** |
| Plugin | `@typescript-eslint` |
| Auto-fixable | no |
| Needs type information | no |

## What it checks

Disallow unused private class members

## Why this recommendation

Catches a defect rather than a preference, and the cleanup is small enough to do in one pass.

## Violations

4 in the tree. Showing the first few.

**`libraries/structure/libraries/nexus/source/numbers/Decimal.ts:385`**

```
private signedCoefficient(): bigint {
```

> Private class member 'signedCoefficient' is defined but never used

**`libraries/structure/libraries/nexus/source/numbers/Decimal.ts:757`**

```
private toFixedPointStringPadded(decimalPlaces: number): string {
```

> Private class member 'toFixedPointStringPadded' is defined but never used

**`libraries/structure/source/api/web-sockets/shared-worker/WebSocketConnection.ts:58`**

```
private boundHandleInternetAvailable: EventListener | null = null;
```

> Private class member 'boundHandleInternetAvailable' is defined but never used

**`libraries/structure/source/api/web-sockets/shared-worker/WebSocketConnection.ts:59`**

```
private boundHandleInternetUnavailable: EventListener | null = null;
```

> Private class member 'boundHandleInternetUnavailable' is defined but never used

### What fixing looks like

Not auto-fixable. Each site needs a human decision, which is what makes the count above the real cost.

