# staticcheck v0.8.1, with upstream's fa9ae6d1a5 in unused/ and a directive fast path in analysis/lint/

This is `honnef.co/go/tools` v0.8.1 as released, copied whole and byte for byte from the module cache, but for
two changes: the `unused/` hunk of upstream's commit
[fa9ae6d1a5](https://github.com/dominikh/go-tools/commit/fa9ae6d1a5e940e7857ff6d44812debc69aa9154), "lintcmd/runner,
unused: reuse objectpath.Encoder", and `lint.ParseDirectives` skipping the comment map of a file with no directive.
cohere's go.mod replaces the module with this directory (#j3bfnyt).

## unused/: one objectpath.Encoder per package

### Why

U1000 spent most of its CPU in `objectpath.For`, which walks a package afresh for every object it records: 4.3 to 4.5
core-seconds at cohere's root, and with the shared encoder 0.5 to 0.7, the findings the same on cohere and loom.
v0.8.2 does not carry the commit.

### The change

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

The commit's other hunk, in `lintcmd/runner`, is not carried: cohere runs staticcheck's analyzers in its own engine
and never links staticcheck's runner. `TestU1000SharesOneObjectPathEncoderAcrossAPackage`, in
internal/go_engine/rules/unused_code/encoder_test.go, holds the change by counting the bytes U1000 allocates as a
package doubles.

## analysis/lint/: no comment map for a file with no directive

### Why

`lint.ParseDirectives`, which the `directives` analyzer runs for every package, built an `ast.NewCommentMap` for every
file and then kept only the comments starting `//lint:`. Almost no file has one: 14 directives in cohere's own 5,332 Go
files. The map was most of the analyzer's CPU, about 0.35 CPU-s on a cold run at cohere's root (#tcbjtmj). Upstream's
own `OPT(dh)` note above the call asks whether skipping it for a file with no directive is worth doing. Over those
5,332 files, one package per call, it took `ParseDirectives` from 338 to 409 ms and 174,820 allocations (226.7 MB) to
4.3 to 8.5 ms and 801 allocations (1.1 MB), the same 14 directives found.

### The change

In `analysis/lint/lint.go`, a file none of whose comments starts `//lint:` is skipped before its map is built. That
is the test the release puts to each comment the map holds, so such a file yielded no directive anyway, and a file
with one builds its map as released, each directive keeping its `Node`. Both hunks are marked `cohere:` in the file.

```diff
 		// OPT(dh): in our old code, we skip all the comment map work if we
 		// couldn't find any directives, benchmark if that's actually
 		// worth doing
+		// cohere: a file with no comment starting //lint: yields no directive, ...
+		if !hasDirectiveComment(f) {
+			continue
+		}
 		cm := ast.NewCommentMap(fset, f, f.Comments)
```

```diff
+// cohere: hasDirectiveComment reports whether any comment of f starts //lint:, ...
+func hasDirectiveComment(f *ast.File) bool {
+	for _, cg := range f.Comments {
+		for _, c := range cg.List {
+			if strings.HasPrefix(c.Text, "//lint:") {
+				return true
+			}
+		}
+	}
+	return false
+}
```

`TestParseDirectivesSkipsOnlyFilesWithoutOne`, in internal/go_engine/rules/unused_code/directives_test.go, holds
the change against the release's reading through `ast.NewCommentMap`: the same directives with the same comment and
`Node`, none for `//lint:` inside a block comment or a string, and no allocation for a file with no directive.

## Nothing else differs

Nothing else in the copy differs from the release but these two files, and this shows it:

```sh
diff -ru "$(go env GOMODCACHE)/honnef.co/go/tools@v0.8.1" third_party/honnef.co/go/tools
```

## When it goes

At the first staticcheck release that carries fa9ae6d1a5, `unused/unused.go` goes back to the release's file. The
`analysis/lint/` change is cohere's own, carried until a release makes it too. Once no change is left, delete this
directory and the `replace` in go.mod, and require that release. The two tests above stay and hold the release to
the same.
