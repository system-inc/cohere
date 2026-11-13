package build

import (
	"crypto/sha256"
	"encoding/hex"
	"go/build"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/copied"
)

// copiedSources are the sha256 of each upstream file the copies are of, at CopiedToolchain, by its path below the
// toolchain's src directory.
var copiedSources = map[string]string{
	"go/build/build.go":           "a8b066521d73f2b1c1f01d65588483e416deeab9b2139991645da0dc89dd7d49",
	"go/build/read.go":            "6e319d4ccc52e5a968a85cd5066948234a061c3ae958c77c07d0f943121b5c29",
	"internal/syslist/syslist.go": "079a54068737d10e87aae714c07b6074e78b8c020edd696953ae01d3086a3010",
}

// The toolchain's go/build is the copy's, byte for byte, and its release is among those the binary reads through the
// copy under (sharedReleases): a toolchain whose sources moved past the copy's fails, though its line diff against the
// copy may pass, since a change in the code the copy drops is in no line it compares; and one whose sources did not
// move fails too until its release is added, so the list says what the toolchains ship.
func TestTheToolchainsGoBuildIsTheCopys(test *testing.T) {
	test.Parallel()
	output, goError := exec.Command("go", "env", "GOROOT", "GOVERSION").Output()
	if goError != nil {
		test.Fatalf("asking go env for GOROOT and GOVERSION: %v", goError)
	}
	// One line each, split on the line alone, since a root's path may hold a space.
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) != 2 {
		test.Fatalf("go env printed %q, want the root and the release", output)
	}
	moved := false
	for path, copiedSum := range copiedSources {
		contents, readError := os.ReadFile(filepath.Join(lines[0], "src", filepath.FromSlash(path)))
		if readError != nil {
			test.Fatal(readError)
		}
		sum := sha256.Sum256(contents)
		if hex.EncodeToString(sum[:]) != copiedSum {
			moved = true
			test.Errorf("%s's %s is not %s's, which the copy is of: diff the copy against it and bump the copy",
				lines[1], path, CopiedToolchain)
		}
	}
	if !moved && !sharedReleases[lines[1]] {
		test.Errorf("%s's go/build is %s's, which the copy is of: add it to sharedReleases", lines[1],
			CopiedToolchain)
	}
}

// Every copied file's header names the release CopiedToolchain names, whose sources copiedSources holds, so a copy
// bumped to another release cannot leave the binary or the test checking the old one.
func TestEveryCopyIsOfTheCopiedToolchain(test *testing.T) {
	test.Parallel()
	for _, file := range []string{"build.go", "read.go", "syslist/syslist.go"} {
		parsed, parseError := parser.ParseFile(token.NewFileSet(), file, nil,
			parser.PackageClauseOnly|parser.ParseComments)
		if parseError != nil {
			test.Fatal(parseError)
		}
		header, declared, readError := copied.Read(parsed)
		if !declared || readError != nil {
			test.Fatalf("%s declares no copy: %v", file, readError)
		}
		if header.Module != copied.StandardLibrary || header.Version != CopiedToolchain ||
			copiedSources[header.Path] == "" {
			test.Errorf("%s copies %s of %s@%s, want one of copiedSources of %s@%s", file, header.Path,
				header.Module, header.Version, copied.StandardLibrary, CopiedToolchain)
		}
	}
}

// Only a binary of a release whose go/build is the copy's reads through it, an experiment on or not, and another
// names its release and the copy's.
func TestOnlyACopiedReleaseShares(test *testing.T) {
	test.Parallel()
	for _, linked := range []string{CopiedToolchain, CopiedToolchain + " X:nodwarf5", "go1.27.3"} {
		if sharing, unshared := Sharing(linked); !sharing || unshared != "" {
			test.Errorf("%s does not share: %s", linked, unshared)
		}
	}
	for _, linked := range []string{"go1.28.0", "go1.26.5", "devel go1.28-abcdef"} {
		sharing, unshared := Sharing(linked)
		if sharing || !strings.Contains(unshared, linked) || !strings.Contains(unshared, CopiedToolchain) {
			test.Errorf("%s shares, or says %q, naming not both releases", linked, unshared)
		}
	}
}

// Each Go file is opened once for the run, by the first build whose names admit it, however many builds read its
// directory after, and each build's Package is go/build's own. A file only one build admits is opened by that build.
func TestAGoFileIsOpenedOnceARun(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	for name, contents := range map[string]string{
		"plain.go":       "package plain\n\nimport \"os\"\n\nvar _ = os.Args\n",
		"plain_linux.go": "package plain\n\nimport _ \"container/list\"\n",
		"tagged.go":      "//go:build darwin\n\npackage plain\n\nimport _ \"container/ring\"\n",
		"plain_test.go":  "package plain\n\nimport \"testing\"\n\nfunc TestPlain(test *testing.T) {}\n",
		"plain.s":        "// +build ignore\n",
	} {
		if writeError := os.WriteFile(filepath.Join(directory, name), []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	var mutex sync.Mutex
	opened := map[string]int{}
	headers := NewHeaders()
	for _, target := range []string{"darwin", "linux", "windows", "darwin", "linux"} {
		context := build.Default
		context.GOOS, context.GOARCH, context.CgoEnabled = target, "arm64", false
		context.OpenFile = func(path string) (io.ReadCloser, error) {
			mutex.Lock()
			opened[filepath.Base(path)]++
			mutex.Unlock()
			return os.Open(path)
		}
		built, importError := ImportDirectory(context, directory, nil, headers)
		context.OpenFile = nil
		expected, expectedError := context.ImportDir(directory, 0)
		if !reflect.DeepEqual(built, expected) || (importError == nil) != (expectedError == nil) {
			test.Errorf("%s: read %+v with %v, go/build %+v with %v", target, built, importError, expected,
				expectedError)
		}
	}
	for _, name := range []string{"plain.go", "plain_linux.go", "tagged.go", "plain_test.go"} {
		if opened[name] != 1 {
			test.Errorf("%s was opened %d times, want once", name, opened[name])
		}
	}
	// A file other than Go is opened by every build that reads its directory, as go/build opens it.
	if opened["plain.s"] != 5 {
		test.Errorf("plain.s was opened %d times, want once a build", opened["plain.s"])
	}
}
