// Command verify type-checks, lints, fixes, and formats a TypeScript codebase in one process,
// over one AST, against one type graph.
//
// See the domain body at `ahra tasks show system_verify` for why this exists and what it replaces.
package main

import (
	"fmt"
	"os"

	"github.com/microsoft/typescript-go/shim/core"
)

// version is stamped at build time by the release pipeline. The zero value means a local build.
var version = "dev"

func main() {
	// The shim reach is load-bearing rather than decorative: touching a real typescript-go symbol
	// here is what proves the module graph resolves through `internal/*` before anything else is
	// built on that assumption.
	_ = core.ScriptKindTS

	fmt.Fprintf(os.Stdout, "verify %s\n", version)
}
