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
