# `react/jsx-no-constructed-context-values`

| | |
|---|---|
| **Recommendation** | **Yes** |
| Violations in ahra | **18** |
| Plugin | `eslint-plugin-react` |
| Auto-fixable | no |
| Needs type information | no |

## What it checks

Disallows JSX context provider values from taking values that will cause needless rerenders

## Why this recommendation

The 18 hits are genuine perf defects in shared primitives such as DialogRoot.tsx and CalendarContext.tsx, where a fresh context object each render re-renders every consumer in the tree.

> Reviewed against real violation sites and the house conventions in `CLAUDE.md`,
> which moved this from **Maybe** to **Yes**. The first pass classified rules by
> correctness-versus-style crossed with violation count, and that is blind to
> whether a rule fights a convention we chose on purpose.

## Violations

18 in the tree. Showing the first few.

**`libraries/structure/source/api/web-sockets/providers/WebSocketViaSharedWorkerProvider.tsx:479`**

```
const contextValue: WebSocketViaSharedWorkerContextInterface = {
```

> The 'contextValue' object (at line 479) passed as the value prop to the Context provider (at line 506) changes every render. To fix this consider wrapping it in a useMemo hook

**`libraries/structure/source/components/calendars/CalendarContext.tsx:136`**

```
const value: CalendarContextValueInterface = {
```

> The 'value' object (at line 136) passed as the value prop to the Context provider (at line 155) changes every render. To fix this consider wrapping it in a useMemo hook

**`libraries/structure/source/components/dialogs/DialogRoot.tsx:139`**

```
const contextValue = {
```

> The 'contextValue' object (at line 139) passed as the value prop to the Context provider (at line 219) changes every render. To fix this consider wrapping it in a useMemo hook

**`libraries/structure/source/components/drawers/DrawerRoot.tsx:195`**

```
const contextValue = {
```

> The 'contextValue' object (at line 195) passed as the value prop to the Context provider (at line 208) changes every render. To fix this consider wrapping it in a useMemo hook

**`libraries/structure/source/components/drawers/DrawerRoot.tsx:220`**

```
<DrawerNestedContext.Provider value={{ value: isNestedDrawer ? 2 : 1 }}>
```

> The object passed as the value prop to the Context provider (at line 220) changes every render. To fix this consider wrapping it in a useMemo hook

### What fixing looks like

Not auto-fixable. Each site needs a human decision, which is what makes the count above the real cost.

