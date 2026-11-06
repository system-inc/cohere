package load

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// requireCgo skips test, with the reason, where the toolchain has no cgo: go reports `CGO_ENABLED` 0 when it finds
// no C compiler, and only the load through go/packages, which translates cgo with one, needs it.
func requireCgo(test *testing.T) {
	test.Helper()
	output, goEnvironmentError := exec.Command("go", "env", "CGO_ENABLED").Output()
	if goEnvironmentError != nil {
		test.Fatalf("reading CGO_ENABLED with go env: %v", goEnvironmentError)
	}
	if enabled := strings.TrimSpace(string(output)); enabled != "1" {
		test.Skipf("the toolchain here has no cgo (go env CGO_ENABLED is %q), and the cgo tool needs it", enabled)
	}
}

// cgoPackageFiles is one cgo package, the same bytes in every directory it is written to, so go can serve a
// second load the cgo translation it cached for the first.
var cgoPackageFiles = map[string]string{
	"go.mod": "module example.com/cgoprobe\n\ngo 1.21\n",
	"native.go": "package native\n\n// static int twice(int value) { return value * 2; }\nimport \"C\"\n\n" +
		"func Twice(value int) int { return int(C.twice(C.int(value))) }\n",
}

// A cgo package's compiled files are go's translation of its raw files, and their `//line` directives are
// what every position in the package, and cgocall's reading of the raw file, resolves to, when the project's own
// cgo sends the load through go/packages. Loaded twice from two directories under `GOFLAGS=-trimpath`, as the
// pool runs every go command, each load's positions must name its own directory's native.go, one that exists,
// and never the other's.
//
// It needs cgo, so it skips where the toolchain has none; Loom's runner, which refuses the go list -compiled that
// go/packages runs, cannot reach it at all.
func TestACgoPackageLoadedUnderTrimpathNamesItsOwnFiles(test *testing.T) {
	test.Parallel()
	requireCgo(test)

	for _, directory := range []string{test.TempDir(), test.TempDir()} {
		for name, contents := range cgoPackageFiles {
			if writeError := os.WriteFile(filepath.Join(directory, name), []byte(contents), 0o644); writeError != nil {
				test.Fatal(writeError)
			}
		}
		loaded, loadError := Packages(directory, []string{"./..."},
			[]string{"GOWORK=off", "CGO_ENABLED=1", "GOFLAGS=-trimpath"}, nil)
		if loadError != nil {
			test.Fatal(loadError)
		}
		if len(loaded.Diagnostics) > 0 {
			test.Fatalf("the cgo package loaded from %s with diagnostics: %+v", directory, loaded.Diagnostics)
		}
		named := 0
		for _, analyzedPackage := range loaded.Packages {
			for _, file := range analyzedPackage.Syntax {
				for _, declaration := range file.Decls {
					position := analyzedPackage.Fset.Position(declaration.Pos())
					if filepath.Base(position.Filename) != "native.go" {
						continue
					}
					named++
					if !strings.HasPrefix(position.Filename, directory+string(filepath.Separator)) {
						test.Errorf("a load from %s names %s", directory, position)
					} else if _, statError := os.Stat(position.Filename); statError != nil {
						test.Errorf("a load from %s names %s, which does not exist: %v", directory, position, statError)
					}
				}
			}
		}
		if named == 0 {
			test.Fatalf("no declaration of the load from %s named native.go, so nothing was checked", directory)
		}
	}
}

// Every spelling of -trimpath leaves the translated load's `GOFLAGS`, every other flag stays in order, and
// -trimpath=false comes last, so the value is never empty: an empty one would hand go the go env file's flags,
// which on this machine hold -trimpath.
//
// It is the translated load's, so it runs only where that load can, with cgo; Loom's runner cannot reach it, and
// refuses the go env it runs under the spellings `--trimpath` and `-trimpathy` besides.
func TestTranslatedEnvironmentTakesTrimpathOutOfGoFlags(test *testing.T) {
	test.Parallel()
	requireCgo(test)

	directory := test.TempDir()
	for given, want := range map[string]string{
		"GOFLAGS=-trimpath":                      "GOFLAGS=-trimpath=false",
		"GOFLAGS=--trimpath":                     "GOFLAGS=-trimpath=false",
		"GOFLAGS=-trimpath=true":                 "GOFLAGS=-trimpath=false",
		"GOFLAGS=-buildvcs=false -trimpath -p=4": "GOFLAGS=-buildvcs=false -p=4 -trimpath=false",
		"GOFLAGS=-tags=trimpath -trimpathy":      "GOFLAGS=-tags=trimpath -trimpathy -trimpath=false",
	} {
		test.Run(given, func(test *testing.T) {
			test.Parallel()

			environment, environmentError := translatedEnvironment(directory, []string{given})
			if environmentError != nil {
				test.Fatal(environmentError)
			}
			var goFlags []string
			for _, variable := range environment {
				if strings.HasPrefix(variable, "GOFLAGS=") {
					goFlags = append(goFlags, variable)
				}
			}
			if !slices.Equal(goFlags, []string{want}) {
				test.Fatalf("want only %q, got %q", want, goFlags)
			}
		})
	}
}
