# `nexus/correctness-no-caller-data-mutation`

| | |
|---|---|
| **Recommendation** | `error` |
| Plugin | cohere-native, `nexus` |
| Auto-fixable | no |
| Needs type information | yes, to tie the written chain to a parameter by symbol and to say where each written property and mutating method is declared; without a checker the rule registers nothing |

## What it checks

A write through a parameter into data project code declares: an assignment (any operator), `++`, `--`,
`delete`, or a call of a mutating default-library collection method (`push`, `unshift`, `pop`, `shift`,
`splice`, `sort`, `reverse`, `fill`, `copyWithin`; a Map's or WeakMap's `set` and `delete`, a Set's or
WeakSet's `add` and `delete`, and `clear`), on a chain of property accesses, element accesses and
non-null assertions whose root is a parameter, or a name destructured from one. Unreassigned
local aliases and `for...of` bindings preserve that origin too.

### Array copies preserve element identity

A new array need not contain new objects. The rule follows existing elements through the default
library's `filter`, `slice`, `splice`, `concat`, and `map` when its inline callback returns its first
parameter unchanged (an expression body or a single `return`). Array literals preserve the identity
of their ordinary elements and spreads. `sort` and `reverse` return the same array; indexing, `find`, `at`,
`shift`, `pop`, and `for...of` select its elements. `splice` returns a new array containing the removed
original elements. These contracts also apply to `ReadonlyArray` where available.

The container and the elements are separate: sorting a `slice()` or replacing its slots is silent;
writing a shared element's fields or mutating a shared element collection reports. Known literal
positions (including concat prefixes and negative `at` positions) that select fresh objects stay
silent. A `map` callback that constructs an object, such as `entry => ({ ...entry })`, does not
establish element identity.
Unknown calls, project methods with array-method names and arbitrary callbacks are not followed.
A reassigned local element binding is followed when its initializer (if present) and every assignment
are `shift()` or `pop()` calls returning elements from the same caller origin. This includes
`for (let window = queue.shift(); window !== undefined; window = queue.shift())` and a `while` loop
that assigns `queue.pop()` to its element binding. A fresh-object assignment or a removal call on a
fresh array invalidates that binding. Host-owned properties and the existing process-state and
out-parameter exemptions retain their boundaries through aliases.

Examples below assume `entries` is caller data and `queue` is a shallow copy of it:

| Kind | Example reported write |
|---|---|
| Local alias of caller data | `const alias = entries; alias.push(entry)` |
| Filtered elements | `entries.filter(keep)[0]!.status = 'done'` |
| Sliced elements | `entries.slice()[0]!.status = 'done'` |
| Identity-mapped elements | `entries.map(entry => entry)[0]!.status = 'done'` |
| Spread or ordinary array elements | `[...entries][0]!.status = 'done'`; `[entries[0]!][0]!.status = 'done'` |
| Concatenated receiver or arguments | `entries.concat([])[0]!.status = 'done'`; `for (const entry of local.concat(entries)) entry.status = 'done'` |
| Copies sorted or reversed before selection | `entries.slice().sort(compare)[0]!.status = 'done'`; `entries.slice().reverse()[0]!.status = 'done'` |
| Removed elements and arrays | `queue.shift()!.status = 'done'`; `queue.pop()!.status = 'done'`; `queue.splice(i, 1)[0]!.status = 'done'` |
| Found or positional elements | `entries.find(keep)!.status = 'done'`; `entries.at(0)!.position++` |
| Aliased selection or iteration | `const entry = entries.slice()[0]!; entry.status = 'done'`; `for (const entry of entries.slice()) entry.position++` |
| Record or collection elements | `records.slice()[0]![key] = 1`; `arrays.slice().at(0)!.push(1)` |

For example, `runJunkDeleteWindows` builds a queue with `state.windows.filter(...).sort(...)`,
then repeatedly calls `queue.shift()` in a `for` header and update. Writes to `window.rowsDeleted`
and `window.finished` still change the caller's progress record. Intentional writes can use disable
comments explaining that the progress record is saved after each statement.

The line between data and a host object is where the written property is declared. A property a source
file declares is the caller's data. A property a declaration file declares (`lib.*.d.ts`, the DOM,
`@types/*`, a package's own types) is a host object's API, and setting it is how that API works:
`context.fillStyle`, `port.onmessage`, `element.style.color`, `pattern.lastIndex`. A write with no
declared property (an array element, an index-signature key) counts when the receiver is itself the
caller's data and is an array, a tuple, a `Record`, `Partial`, `Required`, `Pick` or `Omit`, or a type a
source file declares.

## Process state: `@processState <why>`

Some objects are not a caller's data but state the whole process shares: a long-lived hub every helper
is handed, an observer, a registry. Writing through one is what it is for. Rather than a suppression at
every write, the type says so once, at its own declaration, with a doc tag and the reason:

```ts
/** The kingdom's live hub, one per process. @processState every emit and watcher keeps it current in place */
export interface KingdomLiveHubInterface { ... }
```

A write through a parameter whose declared type is that type is then exempt. The tag goes on an
interface, a class or a type alias.

- **The match is exact.** The parameter's declared type, after alias resolution, must be the tagged type
  itself. A union containing it, `Partial<Hub>`, `Readonly<Hub>`, `Hub[]`, an interface extending it, and
  an optional parameter (a union with `undefined`) are each a different type and still report, so a tag
  never spreads past the one type that says it.
- **The reason is required.** A bare `@processState` is reported where it is written
  (`processStateWithoutReason`), and it exempts nothing.

## An out-parameter by contract: `@mutates <parameter> <why>`

Some methods are handed a record to fill in, and that is their contract. Base's
`OrmPersistedScheduledExecutable.run(entity, context)` is the case: the scheduler persists `entity` after
`run` returns, so every job's override writes its outcome onto it. The method that defines the
contract can state it once for all overrides:

```ts
/** @mutates entity the scheduler persists the record after run returns */
protected abstract run(entity: JobRecordInterface, context: ContextInterface): Promise<void>;
```

A write through that parameter is then exempt in the tagged method and in every method that overrides or
implements it, at any depth. The tag goes on a method declaration or an interface's method signature.

- **Overrides are found through the checker**, from the class's and interface's base types and its
  implemented interfaces, never by name. A method that shares the tagged method's name, or its
  parameter's name, without overriding it still reports.
- **The parameter is matched by position** in the tagged declaration, since an override may rename it.
  Every other parameter of the method still reports, and so does a plain function.
- **The tag has to be usable.** One without a reason (`mutatesWithoutReason`), or whose first word names
  none of its method's parameters (`mutatesUnknownParameter`), is reported where it is written and
  exempts nothing.
- Each write a contract excuses is counted in `--coverage`, under the tagged method, its parameter and
  the file declaring them.

## What it declines

Each is a missed finding, never a false one.

- **A callback's parameter**: a function expression or arrow handed straight to a call or `new`, or
  invoked on the spot. A reduce accumulator is the reduce's own value, and an element `forEach` hands
  over belongs to the code iterating it, on the same page. The cost: `Object.entries(byGroup).map(function([group, items]) { return items.sort(...); })`
  reorders the caller's arrays and is not reported.
- **A setter** on a project class: assigning it calls code written to be called that way.
- **A parameter the function reassigns** anywhere, its defaults included: a write may land in the copy.
- **A chain through `this`, an unknown call's result, or a cast**, and a target typed `any`.
  The array contracts above are the supported call results.
- **`list.length = 0`**: `length` is declared by the default library, so the truncation of a caller's
  array is silent.
- **A default-library typed array** (`buffer[offset] = 0` on a `Uint8Array`): declared by the default
  library, and a buffer handed in to be filled is that API's shape.
