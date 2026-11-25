package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/testing/executable_testing"
)

// standInGo is a go for the offline check to run: it runs go1.27.0, has every toolchain but go1.27.9, writing each one
// it is asked for to the file `COHERE_PAIRS_TEST_TOOLCHAINS` names, and its module cache holds example.com/present and
// not example.com/absent, reported as go mod download -json reports a module it cannot fetch under `GOPROXY=off`.
const standInGo = `#!/bin/sh
case "$1 $2" in
"env GOVERSION") echo go1.27.0 ;;
"version ")
	echo "$GOTOOLCHAIN" >> "$COHERE_PAIRS_TEST_TOOLCHAINS"
	if [ "$GOTOOLCHAIN" = go1.27.9 ]; then
		echo "go: downloading go1.27.9: module lookup disabled by GOPROXY=off" >&2
		exit 1
	fi
	echo "go version $GOTOOLCHAIN" ;;
"mod download")
	echo '{"Path":"example.com/present","Version":"v1.0.0"}'
	echo '{"Path":"example.com/absent","Version":"v0.3.0","Error":"module lookup disabled by GOPROXY=off"}'
	exit 1 ;;
*) echo "the stand-in go was asked for $*" >&2; exit 2 ;;
esac
`

// sideSource makes a side's source as far as the offline check reads it: its go.mod, and, when tableFile is not empty,
// the release file that lists its standard library table's versions with tableFile's contents, as a side from before
// #znqfjw2 holds it. A side from #znqfjw2 on holds no such file.
func sideSource(test *testing.T, tableFile string) string {
	test.Helper()
	source := test.TempDir()
	files := map[string]string{filepath.Join(source, "go.mod"): "module github.com/system-inc/cohere\n\ngo 1.27\n"}
	if tableFile != "" {
		release := filepath.Join(source, filepath.FromSlash(standardLibraryTableSource))
		if createDirectoryError := os.MkdirAll(filepath.Dir(release), 0o755); createDirectoryError != nil {
			test.Fatal(createDirectoryError)
		}
		files[release] = tableFile
	}
	for path, contents := range files {
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	return source
}

// listing is a release file that lists versions, comma-separated, as a side from before #znqfjw2 lists them.
func listing(versions string) string {
	return "package release\n\nconst (\n\t" + standardLibraryTableVersionsName + " = \"" + versions +
		"\"\n\n\tstandardLibraryTableCgoValues = \"1,0\"\n)\n"
}

// offlineChecked is checkOffline over sides and one corpus with the stand-in go: its refusal, what it said, and every
// toolchain it asked the go for, sorted.
func offlineChecked(test *testing.T, sides ...goDirectory) (string, string, []string) {
	test.Helper()
	goBinary := filepath.Join(test.TempDir(), "go")
	if writeError := executable_testing.WriteFile(goBinary, []byte(standInGo), 0o755); writeError != nil {
		test.Fatal(writeError)
	}
	asked := filepath.Join(test.TempDir(), "toolchains")
	environment := append(offlineGoEnvironment(os.Environ()), "COHERE_PAIRS_TEST_TOOLCHAINS="+asked)
	said := &strings.Builder{}
	offlineError := checkOffline(goBinary, environment, test.TempDir(), sides,
		[]goDirectory{{label: "corpus loom", path: test.TempDir()}}, progress{writer: said})
	if offlineError == nil {
		test.Fatal("the offline check passed a machine missing a module")
	}
	contents, readError := os.ReadFile(asked)
	if readError != nil && !os.IsNotExist(readError) {
		test.Fatal(readError)
	}
	toolchains := strings.Fields(string(contents))
	slices.Sort(toolchains)
	return offlineError.Error(), said.String(), toolchains
}

// A side whose release still generates the tables, one from before #znqfjw2, needs each toolchain it lists, and a side
// after needs none: with both, the check asks for the old side's toolchains alone, each once, refuses naming the one
// the machine lacks and the module a graph needs, and passes the toolchain and the go the machine has.
func TestCheckOfflineNeedsOnlyAnOldSidesToolchains(test *testing.T) {
	test.Parallel()
	refusal, said, asked := offlineChecked(test,
		goDirectory{label: sideBase, path: sideSource(test, listing("go1.27.4,go1.27.9"))},
		goDirectory{label: sideBranch, path: sideSource(test, "")})
	if !slices.Equal(asked, []string{"go1.27.4", "go1.27.9"}) {
		test.Errorf("the check asked for toolchains %q, want the old side's go1.27.4 and go1.27.9 alone", asked)
	}
	for _, wanted := range []string{
		"toolchain go1.27.9, which the standard library tables are generated under, is not in the module cache",
		"base needs module example.com/absent@v0.3.0, not in the module cache",
		"corpus loom needs module example.com/absent@v0.3.0",
		"--prepare",
	} {
		if !strings.Contains(refusal, wanted) {
			test.Errorf("the refusal does not say %q:\n%s", wanted, refusal)
		}
	}
	if strings.Contains(refusal, "example.com/present") || strings.Contains(refusal, "toolchain go1.27.4") ||
		strings.Contains(refusal, "branch:") {
		test.Errorf("the refusal names what the machine holds, or the side with no table:\n%s", refusal)
	}
	for _, wanted := range []string{"offline: base asks for go1.27 and builds with go1.27.0\n",
		"offline: toolchain go1.27.4\n"} {
		if !strings.Contains(said, wanted) {
			test.Errorf("the check did not say %q:\n%s", wanted, said)
		}
	}
}

// Two sides from #znqfjw2 on need no toolchain of a table: the check asks the go for none, and refuses only the module.
func TestCheckOfflineNeedsNoToolchainOfSidesWithoutTables(test *testing.T) {
	test.Parallel()
	refusal, _, asked := offlineChecked(test, goDirectory{label: sideBase, path: sideSource(test, "")},
		goDirectory{label: sideBranch, path: sideSource(test, "")})
	if len(asked) != 0 {
		test.Errorf("the check asked for toolchains %q, want none", asked)
	}
	if strings.Contains(refusal, "toolchain") || strings.Contains(refusal, "base:") ||
		strings.Contains(refusal, "branch:") {
		test.Errorf("the refusal names a table's toolchain, or a side without one:\n%s", refusal)
	}
}

// The versions are read from a side's own release file: listed when it holds them, none and no error when the side
// holds no file, as this repository's tree from #znqfjw2 on holds none, and an error for a file that does not parse or
// holds no such constant.
func TestStandardLibraryTableVersionsReadsASidesOwnFile(test *testing.T) {
	test.Parallel()
	versions, versionsError := standardLibraryTableVersions(sideSource(test, listing("go1.27.0,go1.27.3")))
	if versionsError != nil || !slices.Equal(versions, []string{"go1.27.0", "go1.27.3"}) {
		test.Errorf("a side's file listing go1.27.0 and go1.27.3 read as %q, %v", versions, versionsError)
	}
	for name, source := range map[string]string{"a side with no file": sideSource(test, ""),
		"this repository": filepath.Join("..", "..")} {
		if versions, versionsError := standardLibraryTableVersions(source); versions != nil || versionsError != nil {
			test.Errorf("%s read as listing %q, %v; want none and no error", name, versions, versionsError)
		}
	}
	for name, contents := range map[string]string{
		"no such constant":   "package release\n\nconst other = \"go1.27.0\"\n",
		"a file that breaks": "package release\n\nconst (\n",
	} {
		if _, versionsError := standardLibraryTableVersions(sideSource(test, contents)); versionsError == nil {
			test.Errorf("%s: want an error", name)
		}
	}
}

// A module asks for its toolchain line when it has one, and for its go line otherwise.
func TestRequiredGoVersionPrefersTheToolchainLine(test *testing.T) {
	test.Parallel()
	for _, module := range []struct {
		goMod string
		want  string
	}{
		{"module a\n\ngo 1.27\n", "go1.27"},
		{"module a\n\ngo 1.27\n\ntoolchain go1.27.3\n", "go1.27.3"},
	} {
		directory := test.TempDir()
		writeError := os.WriteFile(filepath.Join(directory, "go.mod"), []byte(module.goMod), 0o644)
		if writeError != nil {
			test.Fatal(writeError)
		}
		if required, requiredError := requiredGoVersion(directory); requiredError != nil || required != module.want {
			test.Errorf("%q asks for %q, %v; want %q", module.goMod, required, requiredError, module.want)
		}
	}
}
