# `nexus/correctness-no-caller-data-mutation`

| | |
|---|---|
| **Recommendation** | **Kirk's call on the 144 ahra sites first** (below), then `error`. Registered off in ahra until then |
| Findings | **ahra 144** (measured 2026-10-03, cohere's real config plus this rule) |
| Measured precision | 144 of 144 land in data a source file declares, reached through a parameter; none is a host object |
| Plugin | cohere-native, `nexus` |
| Auto-fixable | no |
| Needs type information | yes, to tie the written chain to a parameter by symbol and to say where each written property and mutating method is declared; without a checker the rule registers nothing |

## What it checks

A write through a parameter into data project code declares: an assignment (any operator), `++`, `--`,
`delete`, or a call of a mutating default-library collection method (`push`, `unshift`, `pop`, `shift`,
`splice`, `sort`, `reverse`, `fill`, `copyWithin`; a Map's or WeakMap's `set` and `delete`, a Set's or
WeakSet's `add` and `delete`, and `clear`), on a chain of property accesses, element accesses and
non-null assertions whose root is a parameter, or a name destructured from one.

The line between data and a host object is where the written property is declared. A property a source
file declares is the caller's data. A property a declaration file declares (`lib.*.d.ts`, the DOM,
`@types/*`, a package's own types) is a host object's API, and setting it is how that API works:
`context.fillStyle`, `port.onmessage`, `element.style.color`, `pattern.lastIndex`. A write with no
declared property (an array element, an index-signature key) counts when the receiver is itself the
caller's data and is an array, a tuple, a `Record`, `Partial`, `Required`, `Pick` or `Omit`, or a type a
source file declares.

## Where it came from

Kirk's ruling on `#e3yxyx7`, 2026-10-01: not upstream's `no-param-reassign` with `props: true`. On ahra
that adds 143 findings that are two different things, about 80 writes into a caller's data object and
about 55 writes to host objects whose API is setting properties, and upstream can only separate them with
an allowance list of parameter names, which the doctrine forbids. This rule separates them by type.

## What it declines

Each is a missed finding, never a false one.

- **A callback's parameter**: a function expression or arrow handed straight to a call or `new`, or
  invoked on the spot. A reduce accumulator is the reduce's own value, and an element `forEach` hands
  over belongs to the code iterating it, on the same page. The cost: `Object.entries(byGroup).map(function([group, items]) { return items.sort(...); })`
  reorders the caller's arrays and is not reported.
- **A setter** on a project class: assigning it calls code written to be called that way.
- **A parameter the function reassigns** anywhere, its defaults included: a write may land in the copy.
- **A chain through `this`, a call's result, or a cast**, and a target typed `any`.
- **`list.length = 0`**: `length` is declared by the default library, so the truncation of a caller's
  array is silent.
- **A default-library typed array** (`buffer[offset] = 0` on a `Uint8Array`): declared by the default
  library, and a buffer handed in to be filled is that API's shape.

## The ahra findings, classified by hand

Measured 2026-10-03. The first run counted 175; 31 were callback parameters (reduce accumulators,
`forEach` elements, syntax-tree visitors' nodes), which is what the callback exemption removes. The 144
left, every one read at its site:

| kind | count | examples |
|---|---|---|
| Accumulator or out-parameter: the function's contract is to fill what it is handed | 78 | `diffs.push` in `StructureDoctor.ts` (25), `into.inputTokens +=` in `AhraOsKingdom.addUsage`, `out.x =` in `MapProjection.ts` (10), `visited.add`, `commentCount.value++` |
| State holder: an object that exists to be mutated by the functions it is passed to | 39 | `hub.debounceTimer =`, `hub.watchersByPath.delete` across the four live hubs (28), `credentials.accessToken =`, `state.cursorByToken[...] =` |
| In-place edit that is the function's stated job | 25 | `request.headers[...] =` in `signAwsRequest` (5), `localArguments.splice` in each `takeFlag` (7), `packageJson[...] =` in the doctor's `apply` (4), `opsNavigationLink.active =` |
| Hidden in-place sort: the caller's list comes back reordered and nothing says so | 2 | `reportDriveUsage` sorts `rows`; `mergeOldestFirst` returns `messages.sort(...)` |

None is a false finding by the rule's definition: each lands in data a source file declares, reached
through a parameter. The first three kinds are deliberate designs; the last is a defect. Which of the
deliberate ones should be restructured (return the value, take a copy) and which should carry a
suppression stating the out-parameter contract is Kirk's call.

## Verification

- Fixtures from the real site: `addUsage` as it stands (three writes fire) and fixed to return the sum
  (silent). 15 firing shapes and 20 silent ones, including a host type in a declaration file, the
  default library's `RegExp` and `Error`, a host Record and a host array reached from project data.
- Mutation check, 14 mutants, each confirmed to compile, all killed: callbacks not exempt, `new` not
  counted as a callback, the reassignment check removed, defaults not searched for a reassignment,
  setters counted, declaration files counted, any element key resolved as a property, the receiver not
  required to be data, `Record` alone as a data shape, any method counted as a writer, a collection's
  receiver unchecked, destructured parameters not followed, arrays not counted, updates not listened to.
