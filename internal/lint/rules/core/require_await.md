# `require-await`

| | |
|---|---|
| **Recommendation** | **Maybe** |
| Violations in ahra | **637** |
| Plugin | `eslint core` |
| Auto-fixable | no |
| Needs type information | no |

## What it checks

Disallow async functions which have no `await` expression

## Why this recommendation

Catches a defect rather than a preference, but the volume means it needs a plan rather than a switch.

## Violations

637 in the tree. Showing the first few.

**`app/(os-layout)/ahra/page.tsx:12`**

```
export async function generateMetadata(): Promise<Metadata> {
```

> Async function 'generateMetadata' has no 'await' expression

**`app/(os-layout)/art/page.tsx:8`**

```
export async function generateMetadata(): Promise<Metadata> {
```

> Async function 'generateMetadata' has no 'await' expression

**`app/(os-layout)/contacts/[id]/page.tsx:9`**

```
export async function generateMetadata(): Promise<Metadata> {
```

> Async function 'generateMetadata' has no 'await' expression

**`app/(os-layout)/contacts/page.tsx:11`**

```
export async function generateMetadata(): Promise<Metadata> {
```

> Async function 'generateMetadata' has no 'await' expression

**`app/(os-layout)/data/layout.tsx:14`**

```
export async function generateMetadata(): Promise<Metadata> {
```

> Async function 'generateMetadata' has no 'await' expression

### What fixing looks like

Not auto-fixable. Each site needs a human decision, which is what makes the count above the real cost.

