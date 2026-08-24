# tsgolint utils, vendored

Upstream: https://github.com/typescript-eslint/tsgolint
License: MIT, `Copyright (c) 2025 typescript-eslint and other contributors`. Full text in `LICENSE`.

What remains here is `utils/` and nothing else: the type-checker helper shelf that verify's own
typescript rules call. `GetConstrainedTypeAtLocation`, `UnionTypeParts`, `IntersectionTypeParts`,
`IsTypeFlagSet`, `TypeRecurser`, `IsBuiltinSymbolLike`, `GetCallSignatures`, `TrimNodeTextRange` and
their neighbours are genuinely useful and several rules reach for them by name.

## What used to be here, and why it is gone

This directory also held a parallel rule runtime (`rule/`, with its own `Rule`, `RuleContext`,
`RuleListeners`, `RuleMessage`, `RuleFix`) and six vendored rule bodies, plus an adapter at
`internal/rules/upstream` that converted one into a rule verify could run. The whole apparatus
existed so vendored rule bodies compiled unmodified, which kept re-syncing with upstream a diff
rather than a merge.

tsgolint is a dead project and we will never re-sync, so that arrangement was permanent overhead
plus a second, parallel way to write a rule. All six rules were absorbed onto verify's own rule
interface in `b52e79c` and `dc99b29`, and the layer came out with them. Each absorbed rule carries a
doc comment naming its tsgolint source file and the vendored commit, which is the provenance this
README used to hold.

The absorbed six, all now in `internal/rules/typescript/`: `await-thenable`, `no-array-delete`,
`no-unsafe-unary-minus`, `no-for-in-array`, `no-implied-eval`, `switch-exhaustiveness-check`.

## What was changed in what remains

**Import paths, in every file:**

    github.com/typescript-eslint/tsgolint/internal/utils
      -> github.com/system-inc/verify/internal/upstream/tsgolint/utils
    github.com/microsoft/typescript-go/shim/ast
      -> github.com/microsoft/TypeScript/tsc/shim/ast

**API migration, 36 lines across four files.** These exist because upstream vendored against
`microsoft/typescript-go` and we are on `microsoft/TypeScript` at `tsc/`, which renamed and
re-signed part of the surface:

    utils/overlay_vfs.go      22 lines   WriteFile lost its byte-order-mark parameter; Chtimes is new
    utils/create_program.go    6 lines   NewCompilerHost gained three optional arguments;
                                         GetParsedCommandLineOfConfigFile gained one
    utils/ts_eslint.go         6 lines   ast.IsParameter -> IsParameterDeclaration, and
                                         AsTypeParameter -> AsTypeParameterDeclaration
    utils/ts_api_utils.go      2 lines   the same IsParameter rename

No helper's logic is edited. A reader who expects byte-identical files needs this note.

The vendored commit was `4178710` (2025-07-13) for the files recorded properly, and established by
diff rather than by record for the rest, which landed in `ea1d334` without capturing it. That
uncertainty no longer costs anything, because nothing here is re-synced: these helpers are ours to
maintain now, and the next change to one of them is an ordinary edit rather than a merge.

## Why it compiles at all

The vendored code imports `github.com/microsoft/TypeScript/tsc/shim/...`, and every one of those
paths is `replace`-directed in our `go.mod` to `./shim/...`. There is exactly one shim tree in this
repo and it is ours, so `ast.Node` and `checker.Checker` are not merely the same shape across two
trees, they are the same package.

An earlier version of this file described diffing two shim trees for byte-identity. After the
migration to `microsoft/TypeScript` at `tsc/` there is no second shim tree to diff. Do not go
looking for it; it does not exist. A mismatch fails loudly at compile rather than quietly at
runtime, which is the property we actually need.

## Promoting these into internal/utils

Reasonable, and deliberately not done in the absorption passes. It is a separate decision with its
own blast radius, and folding it into a behavior-preserving move would have made the finding-set
diff harder to trust. The only thing keeping these under an `upstream/` path today is history.
