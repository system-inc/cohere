# `no-unmodified-loop-condition`

| | |
|---|---|
| **Recommendation** | **Strong Yes** |
| Violations in ahra | **3** |
| Plugin | `eslint core` |
| Auto-fixable | no |
| Needs type information | no |

## What it checks

Disallow unmodified loop conditions

## Why this recommendation

Catches a defect rather than a preference, and the cleanup is small enough to do in one pass.

## Violations

3 in the tree. Showing the first few.

**`modules/os/boot-screens/RainbowMatrix.ts:685`**

```
while(!stopped) {
```

> 'stopped' is not modified in this loop

**`modules/os/sensation/AhraOsMonitors.ts:547`**

```
while(!abortRequested && currentRow !== null && currentRow.enabled === 1) {
```

> 'abortRequested' is not modified in this loop

**`modules/tasks/TasksWatchCommandLineInterface.ts:305`**

```
while(!stopping) {
```

> 'stopping' is not modified in this loop

### What fixing looks like

Not auto-fixable. Each site needs a human decision, which is what makes the count above the real cost.

