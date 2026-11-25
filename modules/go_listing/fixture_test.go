package go_listing_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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
// compiles nothing). root is checked to lie outside this module, so the command reads nothing Go's
// test cache could miss.
func goCommand(test testing.TB, root string, arguments []string) *exec.Cmd {
	test.Helper()
	command := exec.Command("go", arguments...)
	command.Dir = outsideModule(test, root)
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

// outsideModule returns directory, a place a test runs the go command, after failing the test unless
// it lies outside the module the test runs in, symbolic links followed. Go keys a cached test result
// only on files inside that module, so a go command run on a module the test wrote somewhere of its
// own reads nothing the cache could miss. It is cohere's build_inputs.OutsideModule, which this
// module cannot import; cohere's guard accepts a go command whose directory is set from it, here in a
// nested module alone, and this makes that claim checked rather than trusted.
func outsideModule(test testing.TB, directory string) string {
	test.Helper()
	workingDirectory, workingDirectoryError := os.Getwd()
	if workingDirectoryError != nil {
		test.Fatal(workingDirectoryError)
	}
	module := workingDirectory
	for {
		if _, statError := os.Stat(filepath.Join(module, "go.mod")); statError == nil {
			break
		}
		parent := filepath.Dir(module)
		if parent == module {
			test.Fatalf("no go.mod above %s, so there is no module to be outside of", workingDirectory)
		}
		module = parent
	}
	resolvedModule, moduleError := filepath.EvalSymlinks(module)
	if moduleError != nil {
		test.Fatal(moduleError)
	}
	absolute, absoluteError := filepath.Abs(directory)
	if absoluteError != nil {
		test.Fatal(absoluteError)
	}
	resolved, resolveError := filepath.EvalSymlinks(absolute)
	if resolveError != nil {
		test.Fatal(resolveError)
	}
	relative, relativeError := filepath.Rel(resolvedModule, resolved)
	if relativeError != nil {
		test.Fatal(relativeError)
	}
	if relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		test.Fatalf(
			"%s is inside the module at %s, so the go command run there reads this module's code",
			directory,
			resolvedModule,
		)
	}
	return directory
}

// outsideModule fails a test for a directory inside this module, the module's own root and one below
// it, and passes a directory the test made of its own.
func TestOutsideModuleFailsADirectoryInsideTheModule(test *testing.T) {
	test.Parallel()
	for _, inside := range []string{".", "testdata"} {
		if failure, _ := outsideModuleFailure(test, inside); failure == "" {
			test.Errorf("%s passed as outside the module", inside)
		}
	}
	outside := test.TempDir()
	failure, returned := outsideModuleFailure(test, outside)
	if failure != "" {
		test.Errorf("a temporary directory failed as inside the module: %s", failure)
	}
	if returned != outside {
		test.Errorf("outsideModule returned %q for %q", returned, outside)
	}
}

// outsideModuleFailure runs outsideModule on directory against a recorder in place of the test, and
// returns what it failed with, empty when it passed, and what it returned.
func outsideModuleFailure(test *testing.T, directory string) (string, string) {
	test.Helper()
	recorder := &failureRecorder{TB: test}
	returned := ""
	done := make(chan struct{})
	// A failure ends the goroutine as a real test's Fatal does, so outsideModule goes no further.
	go func() {
		defer close(done)
		returned = outsideModule(recorder, directory)
	}()
	<-done
	return recorder.failure, returned
}

// failureRecorder is a test whose Fatal and Fatalf record the failure and end the calling goroutine,
// as a real test's do, without failing the test it wraps.
type failureRecorder struct {
	testing.TB
	failure string
}

func (recorder *failureRecorder) Helper() {
	// Marking a frame of the wrapped test from this goroutine would touch it from another goroutine, and the
	// recorder reports nothing, so there are no lines to attribute.
}

func (recorder *failureRecorder) Fatal(arguments ...any) {
	recorder.failure = fmt.Sprint(arguments...)
	runtime.Goexit()
}

func (recorder *failureRecorder) Fatalf(format string, arguments ...any) {
	recorder.failure = fmt.Sprintf(format, arguments...)
	runtime.Goexit()
}
