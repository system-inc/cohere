package go_listing_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// fixtureModule is the module every listing here is of. Its root package embeds a file, imports a
// dependency and a cgo package, and has a file each build constraint leaves out (a Go file, a test
// file, and an assembly file that is plan9's alone), an in-package test and an external test that
// embeds a file of its own. The cgo package includes a header from a sibling directory through
// `${SRCDIR}` and one from a directory of its own named relatively. broken has a syntax error inside its
// imports, the kind go list sees, and user imports it.
var fixtureModule = map[string]string{
	"go.mod": "module example.com/fixture\n\ngo 1.21\n",
	"lib.go": `package fixture

import (
	_ "embed"

	"example.com/fixture/cg"
	"example.com/fixture/dep"
)

//go:embed message.txt
var Message string

func Value() int { return dep.Value() + cg.Value() }
`,
	"message.txt":   "embedded\n",
	"other.go":      "//go:build never\n\npackage fixture\n",
	"never_test.go": "//go:build never\n\npackage fixture\n",
	"asm_plan9.s":   "// assembled for plan9 alone\n",
	"lib_test.go": `package fixture

import "testing"

func TestValue(test *testing.T) { _ = Value() }
`,
	"x_test.go": `package fixture_test

import (
	_ "embed"
	"testing"

	"example.com/fixture"
)

//go:embed expected.txt
var expected string

func TestMessage(test *testing.T) { _ = fixture.Message == expected }
`,
	"expected.txt": "embedded\n",
	"dep/dep.go":   "package dep\n\nfunc Value() int { return 1 }\n",
	"dep/dep_test.go": `package dep

import "testing"

func TestValue(test *testing.T) { _ = Value() }
`,
	"cg/cg.go": `package cg

// #cgo CFLAGS: -I${SRCDIR}/../include -I include2
// #include "shared.h"
// #include "local.h"
// #include "cg.h"
import "C"

func Value() int { return int(C.value()) }
`,
	"cg/cg.c":             "#include \"cg.h\"\n\nint value(void) { return 2; }\n",
	"cg/cg.h":             "int value(void);\n",
	"cg/include2/local.h": "/* local */\n",
	"include/shared.h":    "/* shared */\n",
	"broken/broken.go":    "package broken\n\nimport \"fmt\n",
	"user/user.go":        "package user\n\nimport _ \"example.com/fixture/broken\"\n",
}

// writeFixture writes fixtureModule into a new temporary directory and returns its root, its symbolic
// links resolved (a Mac's temporary directory is under /var, which is /private/var), so the paths go
// list prints are the root's own.
func writeFixture(test *testing.T) string {
	test.Helper()
	root, resolveError := filepath.EvalSymlinks(test.TempDir())
	if resolveError != nil {
		test.Fatal(resolveError)
	}
	for name, content := range fixtureModule {
		path := filepath.Join(root, filepath.FromSlash(name))
		if mkdirError := os.MkdirAll(filepath.Dir(path), 0o755); mkdirError != nil {
			test.Fatal(mkdirError)
		}
		if writeError := os.WriteFile(path, []byte(content), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	return root
}

// goCommand is go run in root with arguments, under this process's environment less what would make
// the fixture's listing another one: no workspace, no `GOFLAGS`, no module proxy, the local toolchain,
// and cgo on, so the cgo package is listed as cgo on a machine with no C compiler too (go list
// compiles nothing).
func goCommand(root string, arguments []string) *exec.Cmd {
	command := exec.Command("go", arguments...)
	command.Dir = root
	command.Env = append(
		os.Environ(),
		"GOWORK=off",
		"GOFLAGS=",
		"GOPROXY=off",
		"GOTOOLCHAIN=local",
		"CGO_ENABLED=1",
		"PWD="+root,
	)
	return command
}
