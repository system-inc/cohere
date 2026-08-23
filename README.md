# verify

One binary that type-checks, lints, fixes, and formats a TypeScript codebase — in one process,
over one AST, against one type graph.

## Why

Writing code is becoming free; knowing it is correct is not. A gate that is slow gets skipped, and
a skipped gate enforces nothing. The goal is unlimited rules checked in about a quarter second, so
that encoding a judgment stops needing justification.

Measured on a 3,416-file, 24.7 MB TypeScript codebase, the existing toolchain costs 9.3 seconds —
517x slower than running a regex over the same bytes. Almost none of that is compute. It is
boundaries: rules crossing into a JavaScript isolate, a Tailwind plugin making a blocking
cross-thread round trip per class literal, and a type checker rebuilding a 9,530-file program from
scratch on every run, once per shard.

`verify` removes the boundaries by standing in the type checker's own language.

## Built on

- [microsoft/typescript-go](https://github.com/microsoft/typescript-go) — Apache 2.0. The TypeScript
  compiler in Go. Vendored as a submodule and reached through generated shim modules.
- [typescript-eslint/tsgolint](https://github.com/typescript-eslint/tsgolint) — MIT. The shim
  generator and the type-aware rule catalog. We do not fork it: its architecture is one-shot and
  stateless by design, which is precisely what we are replacing.

## Development

```sh
git submodule update --init --depth 1 typescript-go
go build ./...
go run ./cmd/verify
```

`verify` cannot verify itself — it is a Go program and its phases check TypeScript. This repo is
gated by Go's own toolchain: `gofmt -l .`, `go vet ./...`, `go test ./...`, `go build ./...`.

## The dispatcher

Rules are compiled in rather than loaded, which is what makes them free to run. The cost is that
adding a rule means rebuilding, so `cmd/verify-dispatch` pays that cost automatically: it hashes
everything the binary is built from, looks for `.cache/verify/bin/verify-<platform>-<hash>`, and
execs it when present or builds it first when absent. Editing a rule costs one rebuild; every run
after it is a stat and an exec.

```sh
verify                    # resolve, rebuild if the rules moved, exec
verify --dev              # build to a stable path instead of a hash-named one
verify --dispatch-verbose # say so when a rebuild fires
```

Anything the dispatcher does not own is forwarded to the real binary untouched.

`--dev` exists because Go caches package compilation but not linking, so a hash-named binary is a
new filename and therefore a full link on every change. Measured on a comparable binary: a leaf rule
edit costs 2.03s, while a genuinely unchanged tree at a stable path costs 0.29s. The stable path is
what makes that floor reachable during rule authoring. It records its hash beside the binary and
compares it, so a stable name never means a stale binary.

The build flags are fixed at `-trimpath -ldflags="-s -w"`. They make the link marginally faster and
the binary 29% smaller, and `-trimpath` is a build-input change rather than a link flag: turning it
on invalidates the entire compile cache, measured at 33s on a warm 2.0 GB cache. Flipping it per run
would pay that repeatedly, so it does not vary. `GOCACHE` is pinned inside `.cache/verify/` so that
other Go work neither shares it nor evicts it — Go's default cache trims entries unused for about
five days, which would quietly turn a warm rebuild into a cold one.

**A missing binary is a loud error, never a fallback.** With no Go toolchain the dispatcher runs the
prebuilt binary shipped for the platform, and if there is none it exits non-zero naming the platform
and the path it looked for. There is deliberately no path where it execs something that might do
nothing: the gate this tool replaces printed green over zero files for days because a resolver found
no binary, fell through to a bare command name, and an empty file list is indistinguishable from a
clean tree.

Packaging that binary into `node_modules/.bin/verify` belongs to the release domain. The contract it
needs: ship `prebuilt/verify-<goos>-<goarch>` next to the module root, executable.
