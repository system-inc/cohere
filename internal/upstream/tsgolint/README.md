# tsgolint, vendored

Upstream: https://github.com/typescript-eslint/tsgolint
License: MIT, `Copyright (c) 2025 typescript-eslint and other contributors`. Full text in `LICENSE`.

**Vendored commit: unrecorded.** The copy landed in `ea1d334` without capturing which tsgolint
commit it came from, and it cannot be recovered after the fact. So re-syncing today is a diff
against nothing, and vendoring further rules means fetching from a tree whose relationship to this
one is unknown. **Anyone vendoring the next rule records the commit here, in the same act.** That is
the whole fix and it is one line; leaving the gap is how the second rule inherits the first one's.

## Why this is here

tsgolint implements 40 type-aware typescript-eslint rules in Go, against the same
checker verify uses. Four of them are rules our gate enforces today:
`await-thenable`, `no-floating-promises`, `no-misused-promises`, `switch-exhaustiveness-check`.
Those are the expensive ones to write correctly, because they ask real questions of the checker.

The two packages here are copied rather than depended on, because both are `internal/` upstream and
Go will not let another module import them.

## What was changed

Only the import path, mechanically:

    github.com/typescript-eslint/tsgolint/internal/utils
      -> github.com/system-inc/verify/internal/upstream/tsgolint/utils

No logic edits. Keeping the copy byte-identical apart from that rewrite is what makes re-syncing
with upstream a diff rather than a merge.

## Why it compiles at all

The vendored code imports `github.com/microsoft/TypeScript/tsc/shim/...`, and every one of those
paths is `replace`-directed in our `go.mod` to `./shim/...`. **There is exactly one shim tree in
this repo and it is ours**, so `ast.Node` and `checker.Checker` are not merely the same shape
across two trees, they are the same package.

That is a stronger guarantee than an earlier version of this file claimed, and it obsoletes the
check it described. This used to say the two sides' shims were byte-identical over typescript-go
commit `2b82831a05b6b99da279d12fecbcbc460574a85b`, verified with `diff -q`. After the migration to
`microsoft/TypeScript` at `tsc/` there is no second shim tree to diff, and that commit is not the
live one. **Do not go looking for the second tree; it does not exist.**

A mismatch still fails loudly at compile rather than quietly at runtime, which is the property we
actually need.

## The one real gap: exit visits

`rule.ListenerOnExit(kind)` returns `kind + 1000`. The allow-pattern variants add further
offsets, and the constants naming them are ceilings rather than the offsets themselves, which is
easy to misread: `ListenerOnAllowPattern` adds 2000 and `ListenerOnNotAllowPattern` adds 4000, so
the five live encodings land at +1000, +2000, +3000, +4000 and +5000. Those are pseudo-kinds: an upstream rule registers a listener at
an integer that is not a real `ast.Kind`, and their walk knows to call it when leaving a node.

Our walk has no exit-visit concept, so a listener registered at one of those offsets would be
looked up against real node kinds, never match, and never fire. A rule that runs and reports
nothing is exactly the silent-death shape this project exists to prevent, so the adapter refuses
such a rule at registration time rather than adapting it.

Five of the 40 use them: `no_misused_spread`, `no_unsafe_assignment`, `require_await`,
`return_await`, `unbound_method`. None of the four we currently need is among them.
