# `nexus/correctness-require-drained-child-output`

|                        |                                                                                                                |
| ---------------------- | -------------------------------------------------------------------------------------------------------------- |
| **Recommendation**     | **Registered `off` in every set until a census turns it on.** 3 findings across five repositories, 0 false     |
| Findings               | **ahra 1**, www-connected-app 2, api-phi-health, www-phi-health and www-ahra-ai 0 (measured 2026-11-10)        |
| Measured precision     | 3 of 3 true: each is a stream left as a pipe that nothing reads before the child's close is awaited            |
| Plugin                 | cohere-native, `nexus`                                                                                         |
| Auto-fixable           | no                                                                                                             |
| Needs type information | yes, to resolve `spawn` and `events.once` to node's declarations; without a checker the rule registers nothing |

## What it checks

A wait for a spawned child's `'close'` or `'exit'` when the child's stdout or stderr is a pipe that
nothing reads before the wait. One finding per undrained stream, at the wait, naming the stream.

A pipe holds about 64 KB. A child that writes more to a stream nobody reads blocks in `write`, never
exits, and `'close'` never fires, so the parent waits forever beside it.

- **The child**: a `const` declared from `spawn` in `node:child_process` (or `child_process`), however
  it is imported, matched by the declaration the call resolves to.
- **The pipes**: with no options or no `stdio`, both streams. `stdio: 'pipe'` makes both pipes; an array
  makes slot 1 stdout and slot 2 stderr, and an absent slot, a hole, `undefined`, `null` or `'pipe'` is a
  pipe. `'ignore'`, `'inherit'`, a descriptor, a stream, or anything the rule cannot read is not.
- **A drain**: on `child.stdout` or `child.stderr`, a `'data'` or `'readable'` listener, `.pipe`,
  `.resume`, `.read`, `for await`, handing the stream to any function
  (`readline.createInterface({ input: child.stdout })`), and anything else not on the neutral list.
  `.setEncoding`, `.pause`, a listener for `'end'` or `'error'`, and a truth test are neutral.
- **A wait**: `child.on`/`.once`/`.addListener`/`.prependListener`/`.prependOnceListener` with
  `'close'` or `'exit'`, or `once(child, 'close')` from `node:events`.

Paths are decided on the control-flow graph of the function the child is declared in. An awaited wait is
reported when some path reaches it with the stream undrained, since nothing after it runs until the child
closes. A listener wait is reported when some path reaches it and goes on to an exit of the function
still undrained, since a drain attached later in the same run is in time. So a drain attached on one
branch of an `if` and not the other reports.

## What it declines, and why

- **`exec`, `execFile` and the `Sync` functions.** node's `execFile` (which `exec` calls) reads both
  streams into its own buffers on every child; the `Sync` functions buffer too. `fork` inherits stdout and
  stderr unless `silent`, and is left out.
- **A child that escapes.** Handed to a function, returned, stored, copied, exported, or read as
  `.stdio`: the receiver may drain it. A helper that does not is a missed finding.
- **A child not held in a local `const`.** `spawn(...).on('close', done)` and a `let` are not followed.
- **Nested functions.** A `new Promise` executor runs at once, so its references count where it is
  written, drains before waits, its branches not followed. Any other nested function may run at any time:
  a drain inside one counts everywhere, a wait inside one is not a wait.
- **Files that never name `child_process`** are skipped before any type question, which is what keeps
  the rule cheap. A `child_process` re-exported through a project file is a missed finding.
- **A kill on a timer** does not count as a drain. The child is still blocked until the timer fires,
  and its output is lost.

## Where it came from

Kirk's Mac ran out of process slots on 2026-11-10. One cause was a Swift app that waited for a child to
exit before reading its piped output: `ps` printed more than the pipe buffer, the child blocked writing
and the parent blocked waiting, and 80 such children piled up. Built as `#pte6ec9`.

## Census

Each repository linted with only this rule on (`--lint --no-cache --json --timing`), 2026-11-10, the
`-cohere-zero` checkouts for the four outside ahra.

| Repository        | Findings | Site                                                               | Reading                                                                                                                                              |
| ----------------- | -------- | ------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| ahra              | 1        | `modules/presence/PresenceApi.ts:235`, stderr                      | **Real**: `ahra pensieve --recent` collects stdout and leaves stderr a pipe; past 64 KB of stderr the child blocks until the 30-second kill          |
| www-connected-app | 2        | `libraries/library/ai/anthropic/ClaudeCommandLineInterface.ts:162` | **Real**, low risk: `claude --version` with `stdio: ['ignore', 'pipe', 'pipe']` reads neither stream; one line of output fits, `'ignore'` is the fix |
| api-phi-health    | 0        |                                                                    |                                                                                                                                                      |
| www-phi-health    | 0        |                                                                    |                                                                                                                                                      |
| www-ahra-ai       | 0        |                                                                    |                                                                                                                                                      |

**Cost on ahra**: 316 ms of rule CPU, 1.0% of all rules. Before the `child_process` text gate it was
3559 ms, 14.9%, spent resolving the signature of every call in 4954 files; 147 files name the module.

**Recall spot check on ahra**: of its 52 `spawn` calls, the ones read were silent for the right reason:
`stdio: 'inherit'` (`PhiAnalyticsApi.ts:300`), `'ignore'` (`PlanetScaleApi.ts:96`, `AiSessionCache.ts:566`,
`ClaudeCloudApi.ts:277`), log descriptors (`AhraOsHeadlessReactions.ts:101`), both piped streams read
(`LoomHouse.ts:156`), stdio from a variable (`Run.ts:128`), and stdout read with stderr inherited
(`OllamaApi.ts:69`).

## Verification

- Fixtures from the outage's shape (`spawn('ps', ['-eo', 'pid'])` then `await once(child, 'close')`, two
  findings, stdout and stderr), a drain inside an `if`, stderr alone undrained, a drain after the awaited
  close, `setEncoding` alone, an awaited promise around a close listener, ahra's promise wrapper that
  collects stdout only, an `'exit'` listener, renamed, bare-module and destructured imports, a drain on one
  branch of a ternary: 11 firing cases, each on its wait's exact span and message. 18 silent cases cover
  every `stdio` that is not a pipe, unreadable options, `execFile` and `spawnSync`, each kind of drain,
  both branches drained, a drain after a close listener in the same run, a callback drain, a child never
  waited on, escapes, `fork` and a local function, plus the no-checker decline.
- Mutant: counting a drain anywhere in the function as draining every path turns three firing cases red
  (the `if`, the ternary, the drain after the await).
