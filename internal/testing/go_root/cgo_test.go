package go_root

import (
	"go/build"
	"path/filepath"
	"testing"

	"github.com/system-inc/cohere/internal/testing/executable_testing"
)

// `CGO_ENABLED` at 0 or 1 decides, and otherwise cgo is on where go/build.Default has it on and a C compiler is
// named in `CC` or the default one is on the path, as cmd/go decides go env's `CGO_ENABLED`.
//
// Not parallel: it sets the process's own `CGO_ENABLED`, `CC` and `PATH`.
func TestCgoEnabledIsDecidedAsGoDecidesIt(test *testing.T) {
	empty := test.TempDir()
	withCompiler := test.TempDir()
	test.Setenv("CC", "")
	if writeError := executable_testing.WriteFile(filepath.Join(withCompiler, CCompiler()),
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
		if enabled := CgoEnabled(); enabled != expected.enabled {
			test.Errorf("%s: cgo is on %t, want %t", expected.name, enabled, expected.enabled)
		}
	}
}

// `CC` names the compiler when set, and otherwise it is cmd/dist's default for the platform: clang where macOS, iOS
// and the BSDs ship no gcc, gcc elsewhere.
//
// Not parallel: it sets the process's own `CC`.
func TestCCompilerIsCcOrThePlatformsDefault(test *testing.T) {
	test.Setenv("CC", "named-cc -flag")
	if compiler := CCompiler(); compiler != "named-cc -flag" {
		test.Errorf("under CC the compiler is %q, want CC's", compiler)
	}
	test.Setenv("CC", "")
	want := "gcc"
	if build.Default.GOOS == "darwin" || build.Default.GOOS == "ios" || build.Default.GOOS == "freebsd" ||
		build.Default.GOOS == "openbsd" {
		want = "clang"
	}
	if compiler := CCompiler(); compiler != want {
		test.Errorf("on %s the default compiler is %q, want %s", build.Default.GOOS, compiler, want)
	}
}
