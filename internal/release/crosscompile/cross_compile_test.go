// Package crosscompile proves cohere compiles for every platform a release ships. It reads the whole
// program, so any edit reruns it, and it lives apart from internal/release/packaging so that the edit
// reruns it alone and not packaging's Swift and staging tests too (#nxgt2ca).
package crosscompile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	release "github.com/system-inc/cohere/internal/release/packaging"
)

// crossCompileVariable chooses how TestEveryReleaseTargetCompiles proves the targets. Unset, it
// type-checks cohere for each one from source, here and in release.yml. Set to "build", it builds each one
// as the release does, for whoever wants that proof from a test.
//
// Measured on private caches (#nkbfkvn), for all six targets: building costs 17.1 GB of Go cache cold,
// and 5.5 GB more after one real line in internal/lint/rule, because every package that imports it
// recompiles six times. Type-checking from source costs 12 MB cold and nothing after the same edit, in
// about five seconds, since nothing is compiled. `go vet` per target was no better than building, at
// 2.7 GB cold for one target: it compiles every dependency for its export data. Building on every
// local `go test ./...` was most of the 25 GB an hour that filled the disk on 2026-10-03.
//
// Type-checking catches what the Windows break was, a call to something one platform does not define,
// and any type error in a file only one platform compiles. What only a build catches, a link-time
// failure, is left to release.yml's staging, which builds all six right after its tests and fails on
// any that does not build. Asking the test to build them there as well cost a three-core runner 600
// seconds and a timeout (dry run 37238149770), for a proof staging gives anyway.
const crossCompileVariable = "COHERE_CROSS_COMPILE"

// TestEveryReleaseTargetCompiles builds cohere for every platform a release ships, so a target that
// stops compiling fails `go test` instead of the next release.
//
// `go test` otherwise builds only for the host. cohere stopped compiling for windows at `11a25134`
// and nothing in the ordinary gate noticed, because the break was in a Unix-only process-group call
// and every machine that ran the tests was a Mac. The first place it would have shown up was a
// release, the most expensive place there is to find it.
//
// It ranges over release.Targets itself rather than a list of its own, because a second list is how a
// dropped or renamed platform stops being checked without anything failing. It compiles through
// release.GoBuildCommand, the same command the release runs, for the same reason.
//
// It type-checks rather than builds, locally and in release.yml; crossCompileVariable says why, with the
// measurements. `-short` skips it for a quick local loop and says so.
func TestEveryReleaseTargetCompiles(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skipf("NOT MEASURED: -short skips checking cohere for the %d release targets, so a platform that no longer builds would pass here", len(release.Targets))
	}
	mode := os.Getenv(crossCompileVariable)
	if mode != "" && mode != "build" {
		t.Fatalf("%s is %q; it is unset to type-check each target, or \"build\" to build each one", crossCompileVariable, mode)
	}

	moduleDirectory := filepath.Join("..", "..", "..")

	for _, target := range release.Targets {
		// Six type-checks at once hold about 5.4 GB, 900 MB each. One after another they were most of
		// packaging's wall on every edit, about 6s against about 2s in parallel (#nxgt2ca).
		t.Run(target.String(), func(t *testing.T) {
			t.Parallel()
			if mode != "build" {
				if problems := typeCheckTarget(moduleDirectory, target); len(problems) > 0 {
					t.Fatalf("cohere does not type-check for %s, so a release would fail there:\n%s", target, strings.Join(problems, "\n"))
				}
				return
			}
			command := release.GoBuildCommand(moduleDirectory, target, strings.Join(release.StripFlags, " "), filepath.Join(t.TempDir(), target.BinaryFileName()))
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("cohere does not compile for %s, so a release would fail there: %v\n%s", target, err, output)
			}
		})
	}
}

// typeCheckTarget type-checks the package the release builds, and everything it imports, from source as
// target would compile it, and returns every error, or none. Nothing is compiled, so nothing is written
// to the build cache.
func typeCheckTarget(moduleDirectory string, target release.Target) []string {
	loaded, err := packages.Load(&packages.Config{
		// Syntax and type information for every dependency is what makes go/packages type-check them all
		// from source rather than reading export data, which it would have to compile first.
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports |
			packages.NeedDeps | packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo,
		Dir: moduleDirectory,
		Env: append(os.Environ(), "GOOS="+target.GoOperatingSystem, "GOARCH="+target.GoArchitecture, "CGO_ENABLED=0"),
	}, "./command/cohere")
	if err != nil {
		return []string{err.Error()}
	}
	var problems []string
	checked := 0
	packages.Visit(loaded, nil, func(pkg *packages.Package) {
		checked++
		for _, problem := range pkg.Errors {
			problems = append(problems, problem.Error())
		}
	})
	if checked == 0 {
		return []string{fmt.Sprintf("nothing was loaded for %s, so nothing was checked", target)}
	}
	return problems
}

// TestTypeCheckCatchesAPlatformOnlyBreak is the known-dirty control for the local path: the darwin-only
// call the build control below uses, and a type error only a windows file holds, both fail the
// windows type-check and neither fails darwin's.
func TestTypeCheckCatchesAPlatformOnlyBreak(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	writeFile(t, filepath.Join(directory, "go.mod"), "module crosscompile\n\ngo 1.21\n")
	writeFile(t, filepath.Join(directory, "command", "cohere", "main.go"), "package main\n\nfunc main() { onlyOnDarwin() }\n")
	writeFile(t, filepath.Join(directory, "command", "cohere", "only_darwin.go"), "package main\n\nfunc onlyOnDarwin() {}\n")
	writeFile(t, filepath.Join(directory, "command", "cohere", "broken_windows.go"), "package main\n\nvar broken int = \"text\"\n")

	if problems := typeCheckTarget(directory, release.Target{GoOperatingSystem: "darwin", GoArchitecture: "arm64"}); len(problems) > 0 {
		t.Fatalf("the control does not type-check even for darwin, so it cannot show anything: %v", problems)
	}
	problems := strings.Join(typeCheckTarget(directory, release.Target{GoOperatingSystem: "windows", GoArchitecture: "amd64"}), "\n")
	for _, want := range []string{"onlyOnDarwin", "broken_windows.go"} {
		if !strings.Contains(problems, want) {
			t.Errorf("the windows type-check did not report %s: %q", want, problems)
		}
	}
}

// TestCrossCompileCatchesAPlatformOnlyBreak is the known-dirty control for the test above. A check
// that has never failed has not been shown to check anything, and the shape here is the shape of the
// windows break that motivated it: code that calls something only one platform defines.
func TestCrossCompileCatchesAPlatformOnlyBreak(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	writeFile(t, filepath.Join(directory, "go.mod"), "module crosscompile\n\ngo 1.21\n")
	writeFile(t, filepath.Join(directory, "command", "cohere", "main.go"), "package main\n\nfunc main() { onlyOnDarwin() }\n")
	writeFile(t, filepath.Join(directory, "command", "cohere", "only_darwin.go"), "package main\n\nfunc onlyOnDarwin() {}\n")

	darwin := release.Target{GoOperatingSystem: "darwin", GoArchitecture: "arm64"}
	if output, err := release.GoBuildCommand(directory, darwin, "", filepath.Join(t.TempDir(), "cohere")).CombinedOutput(); err != nil {
		t.Fatalf("the control does not compile even for darwin, so it cannot show anything: %v\n%s", err, output)
	}

	windows := release.Target{GoOperatingSystem: "windows", GoArchitecture: "amd64"}
	output, err := release.GoBuildCommand(directory, windows, "", filepath.Join(t.TempDir(), "cohere.exe")).CombinedOutput()
	if err == nil {
		t.Fatal("a call to a darwin-only function compiled for windows, so the cross-compile check cannot catch a platform-only break")
	}
	if !strings.Contains(string(output), "onlyOnDarwin") {
		t.Fatalf("the windows build failed, but not on the darwin-only call: %s", output)
	}
}

func writeFile(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
