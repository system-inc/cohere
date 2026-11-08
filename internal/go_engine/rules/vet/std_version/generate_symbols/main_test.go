package main

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The committed table is what the generator writes now, from the API files of the toolchain go env names, read
// through the minor version go.mod pins. It goes red when go.mod moves to a new minor version, or when the table is
// edited by hand, and says how to write it again.
func TestTheCommittedTableIsTheToolchainsTable(test *testing.T) {
	test.Parallel()
	root, rootError := moduleRoot()
	if rootError != nil {
		test.Fatal(rootError)
	}
	generated, generateError := tableFor(root)
	if generateError != nil {
		test.Fatal(generateError)
	}
	committed, readError := os.ReadFile(filepath.Join(root, tablePath))
	if readError != nil {
		test.Fatal(readError)
	}
	if bytes.Equal(committed, generated) {
		return
	}
	committedLines := strings.Split(string(committed), "\n")
	generatedLines := strings.Split(string(generated), "\n")
	for index := range max(len(committedLines), len(generatedLines)) {
		committedLine, generatedLine := "(the end of the file)", "(the end of the file)"
		if index < len(committedLines) {
			committedLine = committedLines[index]
		}
		if index < len(generatedLines) {
			generatedLine = generatedLines[index]
		}
		if committedLine != generatedLine {
			test.Fatalf("%s is stale against the toolchain's API: line %d reads %q, and the generator writes %q. "+
				"Write it again from the checkout's root with go run "+
				"./internal/go_engine/rules/vet/std_version/generate_symbols", tablePath, index+1, committedLine,
				generatedLine)
		}
	}
}

// Each shape of API line is named as x/tools' generator names it in the manifest, a method by its receiver without
// type parameters, an interface's method in parentheses, a field by its type, an embedded field by its type's bare
// name, while an interface's unexported and deprecated methods are no symbol, and a symbol a later file names again
// keeps the first file's version.
func TestEachApiLineIsNamedAsTheManifestNamesIt(test *testing.T) {
	test.Parallel()
	first := strings.Join([]string{
		"pkg bytes, func Clone([]uint8) []uint8",
		"pkg bytes, method (*Buffer) Grow(int)",
		"pkg sync/atomic, method (*Pointer[$0]) Load() *$0",
		"pkg unique, method (Handle[$0]) Value() $0",
		"pkg reflect, type Type interface, CanSeq() bool",
		"pkg reflect, type Type interface, unexported methods",
		"pkg net, type Error interface, Temporary //deprecated",
		"pkg go/ast, type ParenExpr struct, Lparen token.Pos",
		"pkg go/types, type Checker struct, embedded *Info",
		"pkg net/http, type Example struct, embedded pkg.Generic[$0]",
		"pkg syscall (darwin-arm64), const AF_INET = 2",
		"pkg syscall (darwin-arm64), const AF_INET ideal-int",
		"pkg os, var ErrProcessDone error",
		"pkg go/types, type Alias struct",
		"# a comment",
		"",
	}, "\n")
	later := "pkg bytes, func Clone([]uint8) []uint8 //deprecated\npkg bytes, func CutLast([]uint8) bool\n"
	introduced := map[string]map[string]int{}
	if parseError := parseApiFile("first.txt", []byte(first), 3, introduced); parseError != nil {
		test.Fatal(parseError)
	}
	if parseError := parseApiFile("later.txt", []byte(later), 4, introduced); parseError != nil {
		test.Fatal(parseError)
	}
	wanted := map[string]map[string]int{
		"bytes":       {"Clone": 3, "(*Buffer).Grow": 3, "CutLast": 4},
		"sync/atomic": {"(*Pointer).Load": 3},
		"unique":      {"(Handle).Value": 3},
		"reflect":     {"(Type).CanSeq": 3},
		"go/ast":      {"ParenExpr.Lparen": 3},
		"go/types":    {"Checker.Info": 3, "Alias": 3},
		"net/http":    {"Example.Generic": 3},
		"syscall":     {"AF_INET": 3},
		"os":          {"ErrProcessDone": 3},
	}
	for _, path := range slices.Sorted(maps.Keys(wanted)) {
		if !maps.Equal(introduced[path], wanted[path]) {
			test.Errorf("%s is %v, want %v", path, introduced[path], wanted[path])
		}
	}
	for path := range introduced {
		if _, isWanted := wanted[path]; !isWanted {
			test.Errorf("%s is %v, and no line names it", path, introduced[path])
		}
	}
	if parseError := parseApiFile("broken.txt", []byte("not an api line\n"), 5, introduced); parseError == nil {
		test.Error("a line no API file holds was read without an error")
	}
}

// go.mod's toolchain line pins the minor version when it has one, and its go line otherwise.
func TestTheToolchainLinePinsTheMinorVersionBeforeTheGoLine(test *testing.T) {
	test.Parallel()
	cases := []struct {
		goModule string
		minor    int
	}{
		{"module example.com/pinned\n\ngo 1.27\n", 27},
		{"module example.com/pinned\n\ngo 1.21.3\n", 21},
		{"module example.com/pinned\n\ngo 1.26\n\ntoolchain go1.27.2\n", 27},
	}
	for _, pinned := range cases {
		minor, pinnedError := pinnedMinor("go.mod", []byte(pinned.goModule))
		if pinnedError != nil {
			test.Errorf("%q: %v", pinned.goModule, pinnedError)
			continue
		}
		if minor != pinned.minor {
			test.Errorf("%q pins go1.%d, want go1.%d", pinned.goModule, minor, pinned.minor)
		}
	}
	if _, pinnedError := pinnedMinor("go.mod", []byte("module example.com/unpinned\n")); pinnedError == nil {
		test.Error("a go.mod with no go line pinned a version")
	}
}
