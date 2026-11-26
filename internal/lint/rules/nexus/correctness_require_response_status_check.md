# `nexus/correctness-require-response-status-check`

|                        |                                                                                                                                                                                           |
| ---------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Recommendation**     | **Yes, but registered `off` until the sites are fixed.** 162 findings across the four trees, 0 false. Turning it on at `error` today would put 144 findings on ahra                       |
| Findings               | **ahra 144** (125 body read, 19 discarded), **www-phi-health 2, www-connected-app 3, api-phi-health 13** (measured 2026-10-02)                                                            |
| Measured precision     | 162 of 162 true: in every finding the Response's status is never read on the reported path. 133 are real bugs, 29 are true but harmless (an error envelope in the body is tested instead) |
| Plugin                 | cohere-native, `nexus`                                                                                                                                                                    |
| Auto-fixable           | no                                                                                                                                                                                        |
| Needs type information | yes, to resolve `fetch` and `networkService.request` to their declarations; without a checker the rule registers nothing                                                                  |

## What it checks

A fetch Response whose body is read (`.json()`, `.text()`, `.arrayBuffer()`, `.blob()`, `.formData()`,
`.bytes()`, `.body`) on a path that never reads `.ok`, `.status` or `.statusText` and goes on to a normal
exit, and a fetch Response awaited and dropped. Tracked producers are the platform's global `fetch` and
Structure's `networkService.request` / `baseApiRequest`, both read and confirmed not to throw on an HTTP
error. The doc comment carries the precise version: what counts as a check, an escape and a body read,
and why checking after the read is accepted.

## Where it came from

Two sites in one tree, both verified before being used as fixtures: a read hook that parsed the body
without reading the status, so a 401 or 500 parsed into an object with no list on it and was cached as
the list, and a write whose Response was awaited and dropped, so a 400 or 500 reported success. Found by
the cross-language pass of the new-rules sweep (`#tevhg3f`), built as `#3twjf7k`.

## Reconciling with the research count

Research counted **139** on ahra (120 body read from `fetch` or `networkService.request`, 19 discarded).
cohere counts **144** (125 + 19). The 19 discards match exactly. The body reads differ by 5, all
explained, none a false positive:

- **+2, `baseApiRequest`**: the research producer set named `networkService.request` only. Two upload
  hooks go through `baseApiRequest`, which delegates to the same executor.
- **+3, path-sensitive**: the research probe silenced a binding when `.status` appeared anywhere in the
  function. The rule asks per path, and these three read it only on an error branch: the status read
  only inside a throw for another failure, or the Response escaping to a helper only when a flag is set.
- Research's other 13 body reads came from wrappers (a signing helper, a client's own `get` and `post`
  methods, a `Response` taken as a parameter), out of scope by design: the rule tracks producers, and
  the signing helper already throws on `!ok`.
- The research list's line numbers have drifted since; the per-file counts match apart from the five
  above.

## The findings, read one by one

All 162 were read in context (`context.txt` in the build scratch directory, the declaration through ten
lines past the read).

**Real (133)**

| Shape                                                                 | Count | Reading                                                                                       |
| --------------------------------------------------------------------- | ----- | --------------------------------------------------------------------------------------------- |
| Read hook returning the parsed body as its data                       | 72    | **Bug**: an error body is cached as the read's data, and the surface fails far from the cause |
| Write request or handler parsing the result into state                | 17    | **Bug**: a rejected write shows `undefined` counts or a wrong status line                     |
| Upload hooks through `baseApiRequest`, two in each of three trees     | 6     | **Bug**: a 401 crashes on `.map` of an error object, or returns it as the upload result       |
| Device and service clients                                            | 12    | **Bug**: an error page parsed as a value, or written into the file being downloaded           |
| Admin writes that log the body and return success whatever the status | 7     | **Bug**                                                                                       |
| Discarded `await networkService.request(...)` / `await fetch(...)`    | 19    | **Bug**: a failed write reports success                                                       |

**True but harmless (29)**: the status is never read, but the body's own error envelope is tested and the
API sets it on every failure: an `error` field set by the tree's own routes (though a middleware 401
without the field still slips through), GraphQl `errors`, an OAuth `error`, a vendor's `success`,
`code` or `result` field, a missing token that throws, a parser that rejects any other body so
verification fails closed, and one debug command printing whatever came back. 24 are in ahra, 1 in
www-connected-app and 4 in api-phi-health.

**Recall spot check**: every ahra file that calls a tracked producer, never spells `.ok` / `.status`, and
has no finding was read. There is one, and it returns the Response to its caller: correctly an escape.

## Verification

- Fixtures both ways from the two originating sites: the read hook before (one finding on
  `response.json()`) and after (`if(!response.ok) throw`, silent); the dropped write's two actions before
  (two discards) and after (kept and checked, silent). 13 firing shapes, 21 silent shapes, the
  `@types/node` `declare global` declaration, and an ambient namespaced `fetch` that must not count.
- Mutation check, each mutant a copy through `go test -overlay`, and each killed but one:
  global fetch accepting any symbol (local `fetch`); the global-scope container test (namespaced fetch);
  NetworkService accepting any method, and accepting any file (another `NetworkService` class);
  status members read as neutral (checked before the parse, and nine more); a non-access use read as
  neutral (stored in an object); an unknown member read as neutral (a clone handed on); an uncalled
  `.json` read as a body read (the parse method handed on uncalled); closures not dropping the binding;
  no zero-reference discard; no discard statement (the dropped write); no inline read; no `.then`
  binding; the first walk passing checks (checked before the parse); no second walk, and the second walk
  passing checks (parsed, then checked); thrown exits counted as exits (read to be thrown); the next
  loop turn not ending the Response (a loop that never leaves). **Survived**: removing the unvisited
  reference guard. A probe reporting whenever the guard fires counted 0 on all four trees, and no shape
  was found that reaches it; the doc comment says why it stays.
- `gofmt -l` and `go vet` clean through the overlay. The whole suite through the overlay is green apart
  from `TestEveryRegisteredRuleIsReachableFromTheLiveConfig` (this rule is not in the live config yet),
  `TestRulesFlagListsEveryRegisteredRule` (it builds the binary without the overlay, 460 against 461),
  and `command/cohere` tests failing to build on another author's in-progress edits to `main.go` and
  `format_test.go`, untouched here.
