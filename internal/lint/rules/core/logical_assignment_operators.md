# `logical-assignment-operators`

| | |
|---|---|
| **Recommendation** | **Maybe** |
| Violations in ahra | **8** |
| Plugin | `eslint core` |
| Auto-fixable | yes |
| Needs type information | no |

## What it checks

Require or disallow logical assignment operator shorthand

## Why this recommendation

Stylistic. Small enough to adopt if we want the convention, not urgent if we do not.

## Violations

8 in the tree. Showing the first few.

**`libraries/structure/source/components/forms/fields/FieldMessage.tsx:104`**

```
shouldShowSuccesses = shouldShowSuccesses && !hasErrors && validationResults.successes.length > 0;
```

> Assignment (=) can be replaced with operator assignment (&&=)

**`libraries/structure/source/components/markdown/utilities/MarkdownUtilities.ts:58`**

```
node.data = node.data || {};
```

> Assignment (=) can be replaced with operator assignment (||=)

**`libraries/structure/source/components/markdown/utilities/MarkdownUtilities.ts:59`**

```
node.data.hProperties = node.data.hProperties || {};
```

> Assignment (=) can be replaced with operator assignment (||=)

**`libraries/structure/source/components/markdown/utilities/MarkdownUtilities.ts:189`**

```
current.data = current.data || {};
```

> Assignment (=) can be replaced with operator assignment (||=)

**`libraries/structure/source/components/markdown/utilities/MarkdownUtilities.ts:209`**

```
node.data = node.data || {};
```

> Assignment (=) can be replaced with operator assignment (||=)

### What fixing looks like

Auto-fixable. `eslint --fix` rewrites these, so the cleanup is a command plus a review of the diff.

