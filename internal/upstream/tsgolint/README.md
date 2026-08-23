# tsgolint, vendored

Upstream: https://github.com/typescript-eslint/tsgolint
License: MIT, `Copyright (c) 2025 typescript-eslint and other contributors`. Full text in `LICENSE`.

## Why this is here

tsgolint implements 40 type-aware typescript-eslint rules in Go, against the same
`microsoft/typescript-go` checker verify uses. Four of them are rules our gate enforces today:
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

Their generated shims and ours are byte-identical over the same typescript-go commit
`2b82831a05b6b99da279d12fecbcbc460574a85b`, verified with `diff -q` across `shim/ast`,
`shim/checker`, `shim/core`, and `shim/scanner`. So `ast.Node` and `checker.Checker` are the same
nominal types in both trees, not merely the same shape. We carry two patches on that checkout
(`0001-Parallel-readDirectory-visitor`, `0002-Create-one-checker-per-CPU`); both touch
`internal/compiler/program.go` and `internal/vfs/utilities.go` only, neither touches a type a rule
sees.

If either side moves off that commit, this stops compiling loudly rather than misbehaving quietly,
which is the failure mode we want.

## The one real gap: exit visits

`rule.ListenerOnExit(kind)` returns `kind + 1000`, and there are further offsets at 2000 and 4000
for their allow-pattern variants. Those are pseudo-kinds: an upstream rule registers a listener at
an integer that is not a real `ast.Kind`, and their walk knows to call it when leaving a node.

Our walk has no exit-visit concept, so a listener registered at one of those offsets would be
looked up against real node kinds, never match, and never fire. A rule that runs and reports
nothing is exactly the silent-death shape this project exists to prevent, so the adapter refuses
such a rule at registration time rather than adapting it.

Five of the 40 use them: `no_misused_spread`, `no_unsafe_assignment`, `require_await`,
`return_await`, `unbound_method`. None of the four we currently need is among them.
