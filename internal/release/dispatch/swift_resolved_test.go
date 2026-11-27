//go:build unix

package dispatch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/release/packaging"
	"github.com/system-inc/cohere/internal/testing/executable_testing"
)

// cohere-swift's development build carries the two flags of a laid set, `--disable-sandbox` and `--skip-update`, on
// both its SwiftPM calls, the build and its `--show-bin-path`, when release.SwiftResolvedVariable names one, so the two
// name the same build, and the set's scratch is laid into the build's own; when none is named neither call carries
// them and nothing is laid. The swift on the path here records each call's arguments, a line each, and names a
// directory holding a product.
//
// Not parallel: it puts a fake swift first on PATH and names the set, with test.Setenv.
func TestTheSwiftBuildCarriesTheLaidSetsFlags(test *testing.T) {
	bin := test.TempDir()
	product := test.TempDir()
	calls := filepath.Join(test.TempDir(), "calls")
	writeFile(test, filepath.Join(product, "cohere-swift"), "#!/bin/sh\n")
	fake := "#!/bin/sh\necho \"$*\" >> '" + calls + "'\ncase \"$*\" in\n*--show-bin-path*) echo " + product + " ;;\n" +
		"esac\n"
	if writeError := executable_testing.WriteFile(filepath.Join(bin, "swift"), []byte(fake), 0o755); writeError != nil {
		test.Fatal(writeError)
	}
	test.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	set := test.TempDir()
	writeFile(test, filepath.Join(set, "scratch", "checkouts", "swift-syntax", "Package.swift"), "// laid\n")

	for _, named := range []bool{false, true} {
		test.Setenv(release.SwiftResolvedVariable, map[bool]string{false: "", true: set}[named])
		if removeError := os.Remove(calls); removeError != nil && !os.IsNotExist(removeError) {
			test.Fatal(removeError)
		}
		scratch := filepath.Join(test.TempDir(), "swift-build")
		if buildError := buildSwiftProductWithSwiftPm(
			test.TempDir(),
			scratch,
			filepath.Join(test.TempDir(), "bin", "cohere-swift"),
			nil,
		); buildError != nil {
			test.Fatal(buildError)
		}
		lines := strings.Split(strings.TrimSpace(readFile(test, calls)), "\n")
		if len(lines) != 2 ||
			!strings.HasPrefix(lines[0], "build ") ||
			!strings.HasSuffix(lines[1], " --show-bin-path") ||
			strings.TrimSuffix(lines[1], " --show-bin-path") != lines[0] {
			test.Fatalf("SwiftPM was called %q, want the build and its --show-bin-path with the same arguments", lines)
		}
		flagged := strings.Contains(lines[0], " --disable-sandbox") && strings.Contains(lines[0], " --skip-update")
		unflagged := !strings.Contains(lines[0], "--disable-sandbox") && !strings.Contains(lines[0], "--skip-update")
		if named && !flagged || !named && !unflagged {
			test.Errorf("with a set named %v, SwiftPM was called %q", named, lines)
		}
		_, statError := os.Stat(filepath.Join(scratch, "checkouts", "swift-syntax", "Package.swift"))
		if laid := statError == nil; laid != named {
			test.Errorf("with a set named %v, its scratch was laid into the build's: %v", named, laid)
		}
	}
}
