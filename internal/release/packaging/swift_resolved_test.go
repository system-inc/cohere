//go:build unix

package release

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/testing/executable_testing"
)

// layResolvedSet writes a laid resolved set as Machines lays one: its scratch, a checkout and SwiftPM's workspace
// state, beside the Package.resolved it was resolved from. It returns the set.
func layResolvedSet(test *testing.T) string {
	test.Helper()
	set := test.TempDir()
	writeFile(test, filepath.Join(set, "scratch", "checkouts", "swift-syntax", "Package.swift"), "// laid\n")
	writeFile(test, filepath.Join(set, "scratch", "workspace-state.json"), "{}\n")
	writeFile(test, filepath.Join(set, "Package.resolved"), "{}\n")
	return set
}

// A laid set's scratch is copied whole into a scratch path that holds no checkouts yet, one that doesn't exist or one
// that is empty, and a scratch path holding checkouts already, an earlier build's, is left as it was; either way the
// flags every SwiftPM call adds are returned. With no set named nothing is copied, nothing made, and no flag returned.
//
// Not parallel: it names the set with test.Setenv.
func TestALaidSwiftSetIsCopiedOnlyIntoAScratchWithNoCheckouts(test *testing.T) {
	set := layResolvedSet(test)

	test.Setenv(SwiftResolvedVariable, "")
	unnamed := filepath.Join(test.TempDir(), "scratch")
	if flags, unnamedError := SwiftResolvedArguments(unnamed); unnamedError != nil || flags != nil {
		test.Errorf("with no set named it returned %q (%v), want no flag", flags, unnamedError)
	}
	if _, statError := os.Stat(unnamed); statError == nil {
		test.Errorf("with no set named it made the scratch %s", unnamed)
	}

	test.Setenv(SwiftResolvedVariable, set)
	for _, scratch := range []string{filepath.Join(test.TempDir(), "a fresh scratch"), test.TempDir()} {
		flags, copyError := SwiftResolvedArguments(scratch)
		if copyError != nil || !slices.Equal(flags, []string{"--disable-sandbox", "--skip-update"}) {
			test.Fatalf("a scratch with no checkouts got %q (%v), want the two flags", flags, copyError)
		}
		for _, name := range []string{filepath.Join("checkouts", "swift-syntax", "Package.swift"),
			"workspace-state.json"} {
			if _, statError := os.Stat(filepath.Join(scratch, name)); statError != nil {
				test.Errorf("the set's %s wasn't copied into %s: %v", name, scratch, statError)
			}
		}
		if _, statError := os.Stat(filepath.Join(scratch, "scratch")); statError == nil {
			test.Errorf("the set's scratch was copied into %s as a directory of its own, not its contents", scratch)
		}
	}

	kept := test.TempDir()
	writeFile(test, filepath.Join(kept, "checkouts", "earlier", "Package.swift"), "// an earlier build's\n")
	flags, keptError := SwiftResolvedArguments(kept)
	if keptError != nil || !slices.Equal(flags, []string{"--disable-sandbox", "--skip-update"}) {
		test.Fatalf("a scratch with checkouts got %q (%v), want the two flags", flags, keptError)
	}
	entries, readError := os.ReadDir(kept)
	if readError != nil {
		test.Fatal(readError)
	}
	if len(entries) != 1 || entries[0].Name() != "checkouts" {
		test.Errorf("a scratch with checkouts was copied into: it holds %v", entries)
	}
	if _, statError := os.Stat(filepath.Join(kept, "checkouts", "swift-syntax")); statError == nil {
		test.Error("the set's checkouts were laid over an earlier build's")
	}
}

// A set that can't be laid is refused in words, never built around: one named by no whole path, and one whose scratch
// cp can't copy, which says cp's own words.
//
// Not parallel: it names the set with test.Setenv.
func TestALaidSwiftSetThatCantBeCopiedIsRefused(test *testing.T) {
	test.Setenv(SwiftResolvedVariable, "relative/set")
	if _, relativeError := SwiftResolvedArguments(test.TempDir()); relativeError == nil ||
		!strings.Contains(relativeError.Error(), "is no directory's whole path") {
		test.Errorf("a set named by a relative path was taken: %v", relativeError)
	}
	empty := test.TempDir()
	test.Setenv(SwiftResolvedVariable, empty)
	if _, copyError := SwiftResolvedArguments(test.TempDir()); copyError == nil ||
		!strings.Contains(copyError.Error(), "copying the laid resolved set "+filepath.Join(empty, "scratch")) ||
		!strings.Contains(copyError.Error(), "(cp -cR)") {
		test.Errorf("a set with no scratch wasn't refused naming its copy: %v", copyError)
	}
}

// A release's build of cohere-swift carries the two flags on each of its SwiftPM calls, the resolve, the build and its
// `--show-bin-path`, when a laid set is named, and none of them when none is. The swift on the path here records each
// call's arguments, a line each, and names a directory holding a product, which is no executable, so the build is
// refused after its SwiftPM calls, at the product's checks.
//
// Not parallel: it puts a fake swift first on PATH and names the set, with test.Setenv.
func TestAReleasesSwiftPmCallsCarryTheLaidSetsFlags(test *testing.T) {
	bin := test.TempDir()
	product := test.TempDir()
	calls := filepath.Join(test.TempDir(), "calls")
	writeFile(test, filepath.Join(product, SwiftBinaryFileName), "#!/bin/sh\n")
	fake := "#!/bin/sh\necho \"$*\" >> '" + calls + "'\ncase \"$*\" in\n*--show-bin-path*) echo " + product + " ;;\n" +
		"esac\n"
	if writeError := executable_testing.WriteFile(filepath.Join(bin, "swift"), []byte(fake), 0o755); writeError != nil {
		test.Fatal(writeError)
	}
	test.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	set := layResolvedSet(test)

	for _, named := range []bool{false, true} {
		test.Setenv(SwiftResolvedVariable, map[bool]string{false: "", true: set}[named])
		if removeError := os.Remove(calls); removeError != nil && !os.IsNotExist(removeError) {
			test.Fatal(removeError)
		}
		module := test.TempDir()
		if makeError := os.MkdirAll(filepath.Join(module, "swift_language", "Sources", "CohereSwift", "Command"),
			0o755); makeError != nil {
			test.Fatal(makeError)
		}
		buildError := buildSwiftBinary(module, test.TempDir(), Target{GoOperatingSystem: "darwin",
			GoArchitecture: runtime.GOARCH}, filepath.Join(test.TempDir(), SwiftBinaryFileName), "0123456789abcdef")
		if buildError == nil || strings.Contains(buildError.Error(), "cohere-swift's dependencies") ||
			strings.Contains(buildError.Error(), "building cohere-swift") ||
			strings.Contains(buildError.Error(), "asking SwiftPM") {
			test.Fatalf("the build didn't reach the product's checks past its SwiftPM calls: %v", buildError)
		}
		recorded, readError := os.ReadFile(calls)
		if readError != nil {
			test.Fatal(readError)
		}
		lines := strings.Split(strings.TrimSpace(string(recorded)), "\n")
		if len(lines) != 3 || !strings.HasPrefix(lines[0], "package resolve ") ||
			!strings.HasPrefix(lines[1], "build ") || !strings.HasSuffix(lines[2], " --show-bin-path") {
			test.Fatalf("SwiftPM was called %q, want the resolve, the build and its --show-bin-path", lines)
		}
		for _, line := range lines {
			flagged := strings.Contains(line, " --disable-sandbox") && strings.Contains(line, " --skip-update")
			unflagged := !strings.Contains(line, "--disable-sandbox") && !strings.Contains(line, "--skip-update")
			if named && !flagged || !named && !unflagged {
				test.Errorf("with a set named %v, SwiftPM was called %q", named, line)
			}
		}
	}
}
