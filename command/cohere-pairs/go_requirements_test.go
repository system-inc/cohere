package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/executable_testing"
)

// standInGo is a go for the offline check to run: it runs go1.27.0, has every toolchain but go1.27.9, and its module
// cache holds example.com/present and not example.com/absent, reported as go mod download -json reports a module
// it cannot fetch under `GOPROXY=off`.
const standInGo = `#!/bin/sh
case "$1 $2" in
"env GOVERSION") echo go1.27.0 ;;
"version ")
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

// sideSource makes a side's source as far as the offline check reads it: its go.mod, and the release file that lists
// its standard library table's versions.
func sideSource(test *testing.T, versions string) string {
	test.Helper()
	source := test.TempDir()
	release := filepath.Join(source, filepath.FromSlash(standardLibraryTableSource))
	if createDirectoryError := os.MkdirAll(filepath.Dir(release), 0o755); createDirectoryError != nil {
		test.Fatal(createDirectoryError)
	}
	for path, contents := range map[string]string{
		filepath.Join(source, "go.mod"): "module github.com/system-inc/cohere\n\ngo 1.27\n",
		release: "package release\n\nconst (\n\t" + standardLibraryTableVersionsName + " = \"" + versions +
			"\"\n\n\tstandardLibraryTableCgoValues = \"1,0\"\n)\n",
	} {
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	return source
}

// The offline check refuses naming each missing piece, a toolchain the release's tables are generated under and a
// module a graph needs, and passes the toolchains and the go the machine has.
func TestCheckOfflineNamesAMissingToolchainAndModule(test *testing.T) {
	test.Parallel()
	goBinary := filepath.Join(test.TempDir(), "go")
	if writeError := executable_testing.WriteFile(goBinary, []byte(standInGo), 0o755); writeError != nil {
		test.Fatal(writeError)
	}
	base := sideSource(test, "go1.27.0,go1.27.9")
	branch := sideSource(test, "go1.27.0")
	said := &strings.Builder{}
	offlineError := checkOffline(goBinary, offlineGoEnvironment(os.Environ()), test.TempDir(),
		[]goDirectory{{label: sideBase, path: base}, {label: sideBranch, path: branch}},
		[]goDirectory{{label: "corpus loom", path: test.TempDir()}}, progress{writer: said})
	if offlineError == nil {
		test.Fatal("the offline check passed a machine missing a toolchain and a module")
	}
	refusal := offlineError.Error()
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
	if strings.Contains(refusal, "example.com/present") || strings.Contains(refusal, "toolchain go1.27.0") {
		test.Errorf("the refusal names what the machine holds:\n%s", refusal)
	}
	for _, wanted := range []string{"offline: base asks for go1.27 and builds with go1.27.0\n",
		"offline: toolchain go1.27.0\n"} {
		if !strings.Contains(said.String(), wanted) {
			test.Errorf("the check did not say %q:\n%s", wanted, said)
		}
	}
}

// The versions are read from the release's own source, so this repository's are what a side at this commit checks:
// every one a toolchain name, and the file still holding the constant by its name.
func TestStandardLibraryTableVersionsReadsTheRelease(test *testing.T) {
	test.Parallel()
	versions, versionsError := standardLibraryTableVersions(filepath.Join("..", ".."))
	if versionsError != nil {
		test.Fatal(versionsError)
	}
	if len(versions) == 0 || slices.ContainsFunc(versions, func(listed string) bool {
		return !strings.HasPrefix(listed, "go1.")
	}) {
		test.Errorf("this repository's release lists %q, not Go toolchains", versions)
	}
	if _, missingError := standardLibraryTableVersions(test.TempDir()); missingError == nil {
		test.Error("a source with no release file read as listing versions")
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
