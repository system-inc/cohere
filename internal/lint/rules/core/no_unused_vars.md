# `no-unused-vars`

| | |
|---|---|
| **Recommendation** | **No** |
| Violations in ahra | **12** |
| Plugin | `eslint core` |
| Auto-fixable | no |
| Needs type information | no |

## What it checks

Disallow unused variables

## Why this recommendation

The stated reason is wrong (it is a correctness rule, not formatting), but the verdict stands for a different reason: verify already ships no-unused-vars and all 12 sites are already suppressed with `eslint-disable-next-line @typescript-eslint/no-unused-vars`, so the base rule would double-report and its disable comments would not match.

> Reviewed against real violation sites and the house conventions in `CLAUDE.md`,
> which moved this from **Strong No** to **No**. The first pass classified rules by
> correctness-versus-style crossed with violation count, and that is blind to
> whether a rule fights a convention we chose on purpose.

## Violations

12 in the tree. Showing the first few.

**`libraries/structure/source/services/network/NetworkService.ts:196`**

```
infer TVariables // Infer but don't use
```

> 'TVariables' is defined but never used

**`modules/apple/contacts/ContactsApi.ts:170`**

```
return allContacts.map(({ _pk: _pk, _dbPath: _dbPath, ...contact }) => contact);
```

> '_pk' is defined but never used

**`modules/apple/contacts/ContactsApi.ts:170`**

```
return allContacts.map(({ _pk: _pk, _dbPath: _dbPath, ...contact }) => contact);
```

> '_dbPath' is defined but never used

**`modules/asana/AsanaApi.ts:941`**

```
static async myTasks(_workspaceGid: string = defaultWorkspace): Promise<void> {
```

> '_workspaceGid' is assigned a value but never used

**`modules/facets/FacetsMonthlies.ts:127`**

```
_options: Record<string, string | number | boolean> = {},
```

> '_options' is assigned a value but never used

### What fixing looks like

Not auto-fixable. Each site needs a human decision, which is what makes the count above the real cost.

