# Mixed capture kinds

The TasksCenter reproduction captures `props.items ?? []` in a local lookup
function, calls that function after a state declaration, then memoizes a callback
using only the setter. React Compiler 1.0.0 compiles it successfully. Cohere
previously widened the collection through the lookup result's later consumer,
creating one scope spanning both the collection and the state declaration.
Non-reactive dependency pruning then promoted the otherwise stable setter as an
output of that scope. The setter was the victim, not the origin of reactivity.

The full-source trace starts with the setter non-reactive everywhere and with
mutable range `[254,255)`. The collection's scope originally has raw range
`[166,854)`, aligned/merged to `[162,854)`. With this repair it has raw range
`[168,169)`, aligned/merged to `[162,171)`. The state declaration no longer lies
inside that scope, and the exact-source preservation finding disappears.

## Reference distinctions

React's `mergeValueKinds` joins Frozen and Mutable as **MaybeFrozen**, not
Mutable. A conditional mutation of MaybeFrozen is omitted. This is the compiler's
rule for a value that could be frozen, not proof that both runtime alternatives
are immutable. Assign and CreateFrom still retain mutable-style identity and
edges; Alias, Capture and MaybeAlias still retain a MaybeFrozen source where a
Frozen source would be discarded. Definite mutation remains distinct.

Global must also be represented separately. Global plus Mutable is Mutable,
whereas Frozen plus Mutable is MaybeFrozen. MaybeAlias retains Global sources
but drops Frozen ones. Treating every LoadGlobal as Frozen would make the new
phi join unsound. Freezing an already Primitive, Global or Frozen value must
leave its kind unchanged.

React's CreateFunction additionally refines the closure's kind using capture
kinds, tracked side effects and reference captures. This implementation takes a
bounded subset: a closure with a nonempty context can become Frozen only when
all captures are non-mutable, no capture derives from a ref, and every inspected
body has read-only effects. Nested and outlined bodies are checked. Unknown
calls, mutation effects, suspension, context/global writes, object methods and
freeze effects retain the conservative Mutable seed. This is not a complete
interprocedural side-effect analysis and does not exempt setters or method calls
from memoization validation.

## Fixtures and counterfactual

The mixed-fallback and conditional-expression memo fixtures both report before
the repair and are clean afterwards. Restoring the previous effects/ranges
implementation makes exactly those two cases fail; the six controls stay green.
The controls cover an already-frozen capture, a mutable fallback, a global/mutable
choice, a primitive/mutable choice, a missing reactive dependency and a direct
method call instead of the local lookup function. Every source was compiled
directly with compiler 1.0.0 and preservation validation enabled. Missing-dependency
controls prove the reference harness can report preservation CompileErrors.

The earlier mutable-fallback range test incorrectly required the mixed value to
widen in a component. It now requires widening only in the ordinary-function
counterpart, while still proving both named values were visited. The kind-join
table, alias-edge matrix, immutable-freeze tests, read-only-body exclusions and
ref-derived-value tests pin the mechanism independently of final diagnostics.

## Corpus attribution

Separate private interventions attribute every change in the live hoistable
corpus; counts preserve duplicate dependencies:

| Intervention | Deep | Flat | Removed | Added |
| --- | ---: | ---: | ---: | ---: |
| Baseline | 777 | 3380 | | |
| Global distinction | 783 | 3276 | 108 | 10 |
| Mixed phi kinds | 793 | 3300 | 5 | 39 |
| Read-only closures | 795 | 3302 | 0 | 4 |

The Global change removes 104 roots and four deep paths. Ten added paths are
optional theme members in Badge, AnimatedButton, CopyButton, CalendarRoot,
TimeSeriesChart, Card, ToggleButton, ToggleGroup, ToggleGroupButton and DialogRoot.
The four removed deep paths are handleWebSocketConnectionMessage in the worker
server and three anonymous Confetti paths (width and two x accesses).

The phi change is confined to CalendarGrid (3 removed, 14 added), CalendarHeader
(2 removed, 9 added), and CalendarDayCell (16 added). Their fallback values stop
pulling downstream date, density and JSX reads into a single mutable scope.
This adds ten deep and 24 flat dependencies, rather than discovering a new
non-null property assumption. CalendarHeader's overlap unions fall from 1 to 0.

Closure refinement adds only GraphQlMutationForm's fieldPropertiesAsRecord root,
TimeSeriesMetricsChart's properties.dataKey, one useGraphQlInfiniteScroll root,
and TimeSeriesTip's properties.sortByValue. InputTimeRange's overlap unions fall
from 2 to 1. Overall overlap counts move 130/126 to 128/124; the gap remains four.
The scope oracle stays at 159 assigned and 142 surviving scopes, with no
under-produced fixtures. The preservation and dependency goldens retain their
existing expectations. No validator exemption is added.

## Measurement boundary

Before this repair, the exact `12a6f28` archive binary reports ten occurrences on
the frozen ahra snapshot: three covered by reference CompileSuccess and seven
covered by bailouts. Its SHA-256 is
`7a2183f7faf087413ec56cd210462941bf648e039ad1d67308cfd8fe5bce3b03`.
The exact `42239d3` archive binary reports nine occurrences: one removed, none
added, nine unchanged. TasksCenter is cleared; both TimeSeriesChart findings
remain covered by reference CompileSuccess. The other seven retain their bailout
coverage (six Todo, one Suppression), which is not evidence of either correctness
or incorrectness. The binary's SHA-256 is
`fa37c35a64c80237e15f0bba0981d00e9de8cde262f6faf3b7ac4e882d7035f3`.
