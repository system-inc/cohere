# staticcheck v0.8.1, with upstream's fa9ae6d1a5 in unused/

This is `honnef.co/go/tools` v0.8.1 as released, copied whole and byte for byte from the module cache, but for
one change: the `unused/` hunk of upstream's commit
[fa9ae6d1a5](https://github.com/dominikh/go-tools/commit/fa9ae6d1a5e940e7857ff6d44812debc69aa9154), "lintcmd/runner,
unused: reuse objectpath.Encoder". cohere's go.mod replaces the module with this directory (#j3bfnyt).

## Why

U1000 spent most of its CPU in `objectpath.For`, which walks a package afresh for every object it records: 4.3 to 4.5
core-seconds at cohere's root, and with the shared encoder 0.5 to 0.7, the findings the same on cohere and loom.
v0.8.2 does not carry the commit.

## The change

In `unused/unused.go`, the graph keeps one encoder and finds every object path through it:

```diff
 	namedTypes     []*types.TypeName
 	interfaceTypes []*types.Interface
+
+	pathEnc objectpath.Encoder
 }
```

```diff
-		objPath, _ := objectpath.For(obj)
+		objPath, _ := g.pathEnc.For(obj)
```

Nothing else in the copy differs from the release but this file, and this shows it:

```sh
diff -ru "$(go env GOMODCACHE)/honnef.co/go/tools@v0.8.1" third_party/honnef.co/go/tools
```

The commit's other hunk, in `lintcmd/runner`, is not carried: cohere runs staticcheck's analyzers in its own engine
and never links staticcheck's runner. `TestU1000SharesOneObjectPathEncoderAcrossAPackage`, in
internal/go_engine/rules/unused_test.go, holds the change by counting the bytes U1000 allocates as a package doubles.

## When it goes

At the first staticcheck release that carries fa9ae6d1a5, delete this directory and the `replace` in go.mod, and
require that release. The test above stays and holds the release to the same count.
