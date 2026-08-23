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
binary shipped for the platform, and if there is none it exits non-zero naming the platform and
everywhere it looked. There is deliberately no path where it execs something that might do nothing:
the gate this tool replaces printed green over zero files for days because a resolver found no
binary, fell through to a bare command name, and an empty file list is indistinguishable from a
clean tree.

## Releasing

`verify` reaches a machine as an npm install. One thin dispatcher package resolves a per-platform
binary package, the same shape oxlint and tsgo use:

```
verify                  the package you install; a Node launcher, no binary
@verify/darwin-arm64    ~14 MB Go binary
@verify/darwin-x64
@verify/linux-arm64     for CI and containers
@verify/linux-x64
@verify/win32-arm64
@verify/win32-x64
```

The platform packages are `optionalDependencies` pinned to the exact version, and npm picks one by
matching the `os` and `cpu` fields against the machine. Those fields are generated from the same
target that cross-compiles the binary, because npm's spelling and Go's disagree in two places —
`win32` against `windows`, `x64` against `amd64` — and a package published under the Go spelling
installs correctly and is never found, which on the machine is indistinguishable from a platform we
never shipped.

```sh
go run ./cmd/verify-release --version 0.1.0 --output dist
```

One command builds all six from one machine, in about two minutes, and it stages rather than
publishes. It refuses a release it cannot complete: a target that fails to build fails the whole
run, because a version missing one platform resolves to nothing there and gets reported as a bug
against a release that looked fine everywhere else.

The launcher is Node rather than Go, which is the one surprising choice. A Go dispatcher would have
to be cross-compiled per platform, making it a seventh platform package and defeating the point of
installing one thing. Node is present by construction in an npm install, and `require.resolve` asks
the package manager where a package actually is instead of modeling pnpm, npm, and yarn layouts by
hand.

`verify --version` reports the version, the platform, the Go toolchain, and the vendored compiler as
`owner/name@commit`, so a bug report names what was running rather than "latest". The stamps go in
at link time; an unstamped local build says `dev` and says why.

The compiler's repository is stamped rather than written down, because it has already moved once:
the pin was against `microsoft/typescript-go` until that repository was archived, and is now against
`microsoft/TypeScript`. Both spellings are forty hex characters and resolve in different places, so a
hardcoded label would have survived the migration while quietly becoming false.

### The formatter's bundles

The formatter runs our Prettier fork inside a JavaScript engine, and those bundles are build output
of `~/Projects/system/prettier` rather than anything this repository pins. `--embed-formatter` pulls
them in and stamps the fork's `HEAD` beside the compiler pin, so `--version` grows a `formatter:`
line naming exactly which Prettier a binary formats with.

That stamp exists because the fork is reached by a path on a build machine, not by a version. Without
it, two binaries built from the same verify commit can format the same file differently and neither
can say why, and formatting differences are the worst kind to debug from a report: every diff after
the first one is noise.

The flag is off by default, because the formatter is not wired into verify yet and a release should
not demand a built fork for a feature nothing reaches. When it is on, the build refuses a fork that
is absent, unbuilt, missing any required bundle, holding a zero-byte one, or whose bundles are older
than its tracked source. It refuses before cross-compiling anything, so a stale fork costs a second
rather than six builds. The subset embedded is `standalone.js`, `plugins/estree.js`, and
`plugins/typescript.js` — 2.3 MB measured, against 18 MB for the fork's whole `dist/prettier`, which
would more than double a 14 MB binary to carry formatters for languages this tool does not check.

Staleness is measured by modification time against the fork's newest tracked source file, not by
recording a commit beside the bundles. A recorded commit only catches a rebuild someone remembered
to re-record; the case that actually happens is an edited working tree that was never rebuilt, where
the commit has not moved and the bundles are wrong anyway. `AHRA_VERIFY_PRETTIER_FORK` points at a
checkout somewhere other than the default path.

`AHRA_VERIFY_BINARY=/path/to/verify` points every `verify` on the machine at a local build. A broken
override is fatal rather than a fallback, even when a good install is sitting right there: someone
who sets it has stated which binary they want, and quietly running a different one would hand them
results they would read as their own build's.

### macOS signing

Measured, because the answer decides how much this matters. An unsigned binary with no quarantine
attribute runs normally, and that is what an `npm install` produces — package managers extract
tarballs without setting `com.apple.quarantine`, so consumers installing from the registry are not
blocked. The same binary *with* quarantine set is killed by the kernel: exit 137, SIGKILL, and zero
bytes on both stdout and stderr.

That silent kill is the reason to sign. It is reached whenever the binary travels as a file rather
than as a package — a release asset from a browser, a binary copied out of CI — and it fails in the
worst available way, with no output to explain it. The launcher's signal handling turns it into a
loud 137 rather than a green nothing, and signing removes it entirely.

Signing is wired and credential-gated. It needs a **Developer ID Application** certificate
specifically; an Apple Development certificate signs successfully and is then rejected by the notary
service at the end of a release. Staging without credentials is supported and says so in its own
summary, because an unsigned release is fine and an unsigned release that looks signed is not.

Note that a notarization ticket cannot be stapled to a bare executable — only to a bundle, a disk
image, or an installer package — so a notarized Mach-O is validated by an online check on first
launch. A machine that is entirely offline the first time it runs a quarantined `verify` is still
blocked. The npm path does not set quarantine, so this does not affect it.
