package type_checking

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
)

// CompilerImpliesStrict answers whether the compiler makes this file strict however it is written.
//
// Every TypeScript file is, whatever its tsconfig says, which is ESLint's `ecmaFeatures.impliedStrict`
// (#hks3djf). tsgo reads neither `strict` nor `alwaysStrict` to decide it. Its binder runs every strict
// mode check on every file with no guard (binder.go:632-644 and the checks at 1391-1462 at our pin,
// d92d9bfe), its emitter adds the directive to any output that is not already ESM with no option read
// (estransforms/usestrict.go, run unconditionally by emitter.go:169), and `alwaysStrict: false` is a
// removed option it refuses (compiler/program.go:1038). So no tsconfig turns this off, and asking the
// options here would let a `strict: false` project read as sloppy code the compiler binds as strict
// (Kirk's ruling, Nov 25, #8skx4es).
//
// A JavaScript file is left out: one the compiler only checks runs as written, so a sloppy one really
// does run sloppy, and its own shape (a module, or a directive) decides it.
//
// Lifted from `strict`, which asks it to decide whether a script needs a directive, when
// `no-implicit-globals` needed it to decide whether an assignment to an undeclared name can leak.
// Neither rule reads a compiler option through it, so neither keys its cached verdict on the tsconfig.
func CompilerImpliesStrict(source *ast.SourceFile) bool {
	return !ast.IsSourceFileJS(source)
}
