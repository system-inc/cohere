# `react/button-has-type`

| | |
|---|---|
| **Recommendation** | **Maybe** |
| Violations in ahra | **13** |
| Plugin | `eslint-plugin-react` |
| Auto-fixable | no |
| Needs type information | no |

## What it checks

Disallow usage of `button` elements without an explicit `type` attribute

## Why this recommendation

Stylistic. Small enough to adopt if we want the convention, not urgent if we do not.

## Violations

13 in the tree. Showing the first few.

**`libraries/structure/source/components/buttons/Button.tsx:218`**

```
<button ref={ref as React.Ref<HTMLButtonElement>} type={type} disabled={isDisabled} {...commonProperties}>
```

> The button type attribute must be specified by a static string or a trivial ternary expression

**`libraries/structure/source/components/containers/AccordionItem.tsx:54`**

```
<button
```

> Missing an explicit type attribute for button

**`libraries/structure/source/components/forms/fields/markup/lexical/LexicalFloatingLinkEditor.tsx:93`**

```
<button onClick={handleUpdateLink}>{translations.LexicalFloatingLinkEditor.update}</button>
```

> Missing an explicit type attribute for button

**`libraries/structure/source/components/forms/fields/markup/lexical/LexicalFloatingLinkEditor.tsx:94`**

```
<button onClick={handleRemoveLink}>{translations.LexicalFloatingLinkEditor.remove}</button>
```

> Missing an explicit type attribute for button

**`libraries/structure/source/components/markdown/ContentNavigator.tsx:127`**

```
<button
```

> Missing an explicit type attribute for button

### What fixing looks like

Not auto-fixable. Each site needs a human decision, which is what makes the count above the real cost.

