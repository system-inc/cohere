package go_root

import (
	"go/build"
	"os"
	"path/filepath"
	"testing"

	"github.com/system-inc/cohere/internal/testing/executable_testing"
)

// `CGO_ENABLED` at 0 or 1 decides, and otherwise cgo is on where go/build.Default has it on and a C compiler is
// named in `CC` or the default one is on the path, as cmd/go decides go env's `CGO_ENABLED`.
//
// Each path holds a link to the real toolchain's go, so Find still reads the root, and the default compiler with it,
// while the compiler is on the path or not as each case says.
//
// Not parallel: it sets the process's own `CGO_ENABLED`, `CC` and `PATH`.
func TestCgoEnabledIsDecidedAsGoDecidesIt(test *testing.T) {
	empty := test.TempDir()
	withCompiler := test.TempDir()
	test.Setenv("CC", "")
	goExecutable := filepath.Join(GoRoot(test), "bin", "go")
	for _, directory := range []string{empty, withCompiler} {
		if linkError := os.Symlink(goExecutable, filepath.Join(directory, "go")); linkError != nil {
			test.Fatal(linkError)
		}
	}
	if writeError := executable_testing.WriteFile(filepath.Join(withCompiler, CCompiler(test)),
		[]byte("#!/bin/sh\nexit 1\n"), 0o755); writeError != nil {
		test.Fatal(writeError)
	}
	for _, expected := range []struct {
		name       string
		cgoEnabled string
		compiler   string
		path       string
		enabled    bool
	}{
		{name: "CGO_ENABLED 0 beside a compiler", cgoEnabled: "0", path: withCompiler},
		{name: "CGO_ENABLED 1 with no compiler", cgoEnabled: "1", path: empty, enabled: true},
		{name: "the default compiler on the path", path: withCompiler, enabled: build.Default.CgoEnabled},
		{name: "no compiler on the path", path: empty},
		{name: "a compiler named in CC", compiler: "named-cc", path: empty, enabled: build.Default.CgoEnabled},
	} {
		test.Setenv("CGO_ENABLED", expected.cgoEnabled)
		test.Setenv("CC", expected.compiler)
		test.Setenv("PATH", expected.path)
		if enabled := CgoEnabled(test); enabled != expected.enabled {
			test.Errorf("%s: cgo is on %t, want %t", expected.name, enabled, expected.enabled)
		}
	}
}

// `CC` names the compiler when set, and otherwise it is the default the toolchain was built with (defaultCompiler).
//
// Not parallel: it sets the process's own `CC`.
func TestCCompilerIsCcWhenSet(test *testing.T) {
	test.Setenv("CC", "named-cc -flag")
	if compiler := CCompiler(test); compiler != "named-cc -flag" {
		test.Errorf("under CC the compiler is %q, want CC's", compiler)
	}
}

// homebrewDefaultCompiler is DefaultCC as cmd/dist writes it for a toolchain built with `CC=cc`, as Homebrew builds
// its own.
const homebrewDefaultCompiler = "package cfg\n\nfunc DefaultCC(goos, goarch string) string {\n" +
	"\tswitch goos+`/`+goarch {\n\t}\n\treturn \"cc\"\n}\n"

// officialDefaultCompiler is DefaultCC as cmd/dist writes it for a toolchain built without `CC`, here with one target
// named, linux/arm64: that target's compiler, then clang for cmd/dist's clangos, and gcc.
const officialDefaultCompiler = "package cfg\n\nfunc DefaultCC(goos, goarch string) string {\n" +
	"\tswitch goos+`/`+goarch {\n\tcase \"linux/arm64\":\n\t\treturn \"aarch64-linux-gnu-gcc\"\n\t}\n" +
	"\tswitch goos {\n\tcase \"darwin\", \"ios\", \"freebsd\", \"openbsd\":\n\t\treturn \"clang\"\n\t}\n" +
	"\treturn \"gcc\"\n}\n"

// The default compiler is read from the toolchain's own zdefaultcc.go, as cmd/go reads it: Homebrew's cc on every
// platform, the official toolchain's per-target compiler, clang for the clang platforms and gcc elsewhere, and
// cmd/dist's own rule only for a root without the file. A DefaultCC of any other shape is refused, never guessed past.
func TestTheDefaultCompilerIsReadFromTheToolchainsRoot(test *testing.T) {
	test.Parallel()
	rootWith := func(source string) string {
		root := test.TempDir()
		if source == "" {
			return root
		}
		path := filepath.Join(root, defaultCompilerPath)
		if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
			test.Fatal(makeError)
		}
		if writeError := os.WriteFile(path, []byte(source), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
		return root
	}
	homebrew, official, without := rootWith(homebrewDefaultCompiler), rootWith(officialDefaultCompiler), rootWith("")
	for _, expected := range []struct {
		name     string
		root     string
		goos     string
		goarch   string
		compiler string
	}{
		{name: "Homebrew's on darwin", root: homebrew, goos: "darwin", goarch: "arm64", compiler: "cc"},
		{name: "Homebrew's on linux", root: homebrew, goos: "linux", goarch: "amd64", compiler: "cc"},
		{name: "the official target named", root: official, goos: "linux", goarch: "arm64",
			compiler: "aarch64-linux-gnu-gcc"},
		{name: "the official on darwin", root: official, goos: "darwin", goarch: "arm64", compiler: "clang"},
		{name: "the official on linux", root: official, goos: "linux", goarch: "amd64", compiler: "gcc"},
		{name: "no file on freebsd", root: without, goos: "freebsd", goarch: "amd64", compiler: "clang"},
		{name: "no file on linux", root: without, goos: "linux", goarch: "amd64", compiler: "gcc"},
	} {
		compiler, compilerError := defaultCompiler(expected.root, expected.goos, expected.goarch)
		if compilerError != nil || compiler != expected.compiler {
			test.Errorf("%s: the default compiler is %q (%v), want %q", expected.name, compiler, compilerError,
				expected.compiler)
		}
	}
	unknown := rootWith("package cfg\n\nfunc DefaultCC(goos, goarch string) string {\n\tif goos == \"plan9\" {\n" +
		"\t\treturn \"pcc\"\n\t}\n\treturn \"gcc\"\n}\n")
	if compiler, compilerError := defaultCompiler(unknown, "plan9", "amd64"); compilerError == nil {
		test.Errorf("a DefaultCC of a shape cmd/dist never writes read as %q, want it refused", compiler)
	}
}
