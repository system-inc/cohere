package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// writeRepository writes files into a new git repository and commits them, so git mv has history to carry.
func writeRepository(test *testing.T, files map[string]string) string {
	root := test.TempDir()
	for name, contents := range files {
		filename := filepath.Join(root, name)
		if makeError := os.MkdirAll(filepath.Dir(filename), 0o755); makeError != nil {
			test.Fatal(makeError)
		}
		if writeError := os.WriteFile(filename, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	for _, arguments := range [][]string{
		{"init", "-q"},
		{"add", "."},
		{"-c", "user.name=fixture", "-c", "user.email=fixture@example.com", "commit", "-q", "-m", "fixture"},
	} {
		command := exec.Command("git", append([]string{"-C", root}, arguments...)...)
		if combined, gitError := command.CombinedOutput(); gitError != nil {
			test.Fatalf("git %v: %v: %s", arguments, gitError, combined)
		}
	}
	return root
}

func readFile(test *testing.T, root string, name string) string {
	contents, readError := os.ReadFile(filepath.Join(root, name))
	if readError != nil {
		test.Fatal(readError)
	}
	return string(contents)
}

func exists(root string, name string) bool {
	_, statError := os.Stat(filepath.Join(root, name))
	return statError == nil
}

// fixtureMoves moves goengine to go_engine, and markertype, nested in it, to marker_type inside the new folder.
var fixtureMoves = []folderMove{
	{OldFolder: "goengine", NewFolder: "go_engine", NewPackage: "go_engine"},
	{OldFolder: "goengine/markertype", NewFolder: "go_engine/marker_type", NewPackage: "marker_type"},
}

// fixtureEngine is the moved package and what lives in it.
var fixtureEngine = map[string]string{
	"go.work": "go 1.27\n\nuse .\n",
	"go.mod":  "module fixture\n\ngo 1.27\n",
	"goengine/engine.go": `// Package goengine runs the engine.
package goengine

import "fixture/goengine/markertype"

// Run runs.
func Run() int { return markertype.Marker + 1 }
`,
	"goengine/engine_test.go": `package goengine_test

import (
	"testing"

	"fixture/goengine"
)

func TestRun(test *testing.T) {
	if goengine.Run() != 2 {
		test.Fatal("Run")
	}
}
`,
	"goengine/markertype/marker.go": `// Package markertype holds the marker.
package markertype

// Marker is one.
const Marker = 1
`,
	"goengine/load/load.go": `// Package load keeps its name and moves with its parent.
package load

import "fixture/goengine/markertype"

// Loaded is the marker.
var Loaded = markertype.Marker
`,
	"goengine/data/notes.txt": "a file that is not Go moves too\n",
}

func withFiles(base map[string]string, extra map[string]string) map[string]string {
	result := map[string]string{}
	for name, contents := range base {
		result[name] = contents
	}
	for name, contents := range extra {
		result[name] = contents
	}
	return result
}

// Every decision on one tree: the clauses and docs of the moved folders, an external test package's clause, a
// nested folder moving with its parent and keeping its name, every import path, an implicit import's qualifiers,
// an alias spelling the old name dropped, another alias kept, a local in the way renamed in camel case, a fixture
// under testdata read without types, a file only Windows builds read with Windows' types, and a file importing
// nothing moved left byte for byte.
func TestFolderNamesMoveRewriteAndProve(test *testing.T) {
	test.Parallel()
	untouched := `package other

import "strings"

// Upper reads goengine as a word, not a package.
func Upper(goengine string) string { return strings.ToUpper(goengine) }
`
	root := writeRepository(test, withFiles(fixtureEngine, map[string]string{
		"user/user.go": `package user

import (
	"fixture/goengine"
	"fixture/goengine/load"
)

// Use reads the engine.
func Use() int { return goengine.Run() + load.Loaded }
`,
		"user/aliased.go": `package user

import goengine "fixture/goengine"

// Again reads the engine through an alias that spells its old name.
func Again() int { return goengine.Run() }
`,
		"user/renamed.go": `package user

import engine "fixture/goengine"

// Named reads the engine through an alias of its own.
func Named() int { return engine.Run() }
`,
		"user/shadow.go": `package user

import "fixture/goengine"

// Shadow has a local the new name would capture.
func Shadow() int {
	go_engine := 3
	return go_engine + goengine.Run()
}
`,
		"user/platform_windows.go": `package user

import "fixture/goengine"

// Windows reads the engine on Windows only.
func Windows() int { return goengine.Run() }
`,
		"user/testdata/fixture.go": `package fixture

import "fixture/goengine"

var value = goengine.Run()
`,
		"other/other.go": untouched,
	}))

	var output bytes.Buffer
	if exitCode := run(root, fixtureMoves, false, &output); exitCode != 0 {
		test.Fatalf("exit %d:\n%s", exitCode, output.String())
	}
	for _, gone := range []string{"goengine", "go_engine/markertype"} {
		if exists(root, gone) {
			test.Errorf("%s still exists", gone)
		}
	}
	if readFile(test, root, "go_engine/data/notes.txt") != "a file that is not Go moves too\n" {
		test.Error("the file that is not Go did not move with its folder")
	}
	want := map[string]string{
		"go_engine/engine.go": `// Package go_engine runs the engine.
package go_engine

import "fixture/go_engine/marker_type"

// Run runs.
func Run() int { return marker_type.Marker + 1 }
`,
		"go_engine/engine_test.go": `package go_engine_test

import (
	"testing"

	"fixture/go_engine"
)

func TestRun(test *testing.T) {
	if go_engine.Run() != 2 {
		test.Fatal("Run")
	}
}
`,
		"go_engine/marker_type/marker.go": `// Package marker_type holds the marker.
package marker_type

// Marker is one.
const Marker = 1
`,
		"go_engine/load/load.go": `// Package load keeps its name and moves with its parent.
package load

import "fixture/go_engine/marker_type"

// Loaded is the marker.
var Loaded = marker_type.Marker
`,
		"user/user.go": `package user

import (
	"fixture/go_engine"
	"fixture/go_engine/load"
)

// Use reads the engine.
func Use() int { return go_engine.Run() + load.Loaded }
`,
		"user/aliased.go": `package user

import "fixture/go_engine"

// Again reads the engine through an alias that spells its old name.
func Again() int { return go_engine.Run() }
`,
		"user/renamed.go": `package user

import engine "fixture/go_engine"

// Named reads the engine through an alias of its own.
func Named() int { return engine.Run() }
`,
		"user/shadow.go": `package user

import "fixture/go_engine"

// Shadow has a local the new name would capture.
func Shadow() int {
	goEngine := 3
	return goEngine + go_engine.Run()
}
`,
		"user/platform_windows.go": `package user

import "fixture/go_engine"

// Windows reads the engine on Windows only.
func Windows() int { return go_engine.Run() }
`,
		"user/testdata/fixture.go": `package fixture

import "fixture/go_engine"

var value = go_engine.Run()
`,
		"other/other.go": untouched,
	}
	for name, contents := range want {
		if got := readFile(test, root, name); got != contents {
			test.Errorf("%s:\n%s\nwant:\n%s", name, got, contents)
		}
	}
	for _, line := range []string{
		"edits: 26 in 10 files: 3 package clauses, 2 package docs, 10 import paths, 1 aliases dropped, 1 aliases kept, 8 qualifiers, 1 locals renamed, 1 files read without types",
		"proof in memory: ",
		"proof in memory, platform windows/amd64: 1 files,",
		"proof on disk: ",
	} {
		if !strings.Contains(output.String(), line) {
			test.Errorf("output lacks %q:\n%s", line, output.String())
		}
	}
	if strings.Contains(output.String(), "mismatch:") {
		test.Errorf("mismatches:\n%s", output.String())
	}
	status, statusError := exec.Command("git", "-C", root, "status", "--porcelain").Output()
	if statusError != nil {
		test.Fatal(statusError)
	}
	if !strings.Contains(string(status), "R  goengine/data/notes.txt -> go_engine/data/notes.txt") {
		test.Errorf("git mv did not carry the history:\n%s", status)
	}
}

// A declaration at package level spelling the new name cannot be renamed out of the way by the local engine, so
// the whole pass is refused and the tree is left as it was.
func TestFolderNamesRefuseAPackageLevelNameInTheWay(test *testing.T) {
	test.Parallel()
	user := `package user

import "fixture/goengine"

var go_engine = 2

// Use reads the engine.
func Use() int { return goengine.Run() + go_engine }
`
	root := writeRepository(test, withFiles(fixtureEngine, map[string]string{"user/user.go": user}))
	var output bytes.Buffer
	if exitCode := run(root, fixtureMoves, false, &output); exitCode != 1 {
		test.Fatalf("exit %d, want 1:\n%s", exitCode, output.String())
	}
	if !strings.Contains(
		output.String(),
		"go_engine is declared at package level, in the way of the import's new name",
	) {
		test.Errorf("no refusal for the package-level name:\n%s", output.String())
	}
	if !exists(root, "goengine/engine.go") ||
		exists(root, "go_engine") ||
		readFile(test, root, "user/user.go") != user {
		test.Error("a refused pass changed the tree")
	}
}

// A dry run proves the moves in memory and changes nothing.
func TestFolderNamesDryRunWritesNothing(test *testing.T) {
	test.Parallel()
	root := writeRepository(test, fixtureEngine)
	var output bytes.Buffer
	if exitCode := run(root, fixtureMoves, true, &output); exitCode != 0 {
		test.Fatalf("exit %d:\n%s", exitCode, output.String())
	}
	if !strings.Contains(output.String(), "dry run: proven in memory") || exists(root, "go_engine") ||
		readFile(test, root, "goengine/engine.go") != fixtureEngine["goengine/engine.go"] {
		test.Errorf("the dry run changed the tree or proved nothing:\n%s", output.String())
	}
}

// A clause that is not the folder's name, or its external test's, is refused rather than guessed.
func TestFolderNamesRefuseAClauseTheMappingDidNotExpect(test *testing.T) {
	test.Parallel()
	moves := []folderMove{
		{OldFolder: "goengine/markertype", NewFolder: "goengine/marker_type", NewPackage: "marker_type"},
	}
	mapping, mappingError := newMapping("fixture", moves)
	if mappingError != nil {
		test.Fatal(mappingError)
	}
	if _, nameError := mapping.NewPackageName("goengine/markertype", "marker"); nameError == nil {
		test.Error("package marker in markertype was not refused")
	}
	if name, nameError := mapping.NewPackageName(
		"goengine/markertype",
		"markertype_test",
	); nameError != nil || name != "marker_type_test" {
		test.Errorf("markertype_test reads %s (%v), want marker_type_test", name, nameError)
	}
	if name, nameError := mapping.NewPackageName("goengine", "goengine"); nameError != nil || name != "goengine" {
		test.Errorf("a folder no line moves renamed its package to %s (%v)", name, nameError)
	}
}

// A nested line must land inside its parent's new folder, and the paths translate by the longest prefix.
func TestFolderNamesMappingComposesNestedMoves(test *testing.T) {
	test.Parallel()
	if _, mappingError := newMapping("fixture", []folderMove{
		{OldFolder: "goengine", NewFolder: "go_engine", NewPackage: "go_engine"},
		{OldFolder: "goengine/markertype", NewFolder: "goengine/marker_type", NewPackage: "marker_type"},
	}); mappingError == nil {
		test.Error("a nested move outside its parent's new folder was accepted")
	}
	mapping, mappingError := newMapping("fixture", fixtureMoves)
	if mappingError != nil {
		test.Fatal(mappingError)
	}
	for old, want := range map[string]string{
		"fixture/goengine":                         "fixture/go_engine",
		"fixture/goengine/markertype":              "fixture/go_engine/marker_type",
		"fixture/goengine/markertype/inner":        "fixture/go_engine/marker_type/inner",
		"fixture/goengine/load":                    "fixture/go_engine/load",
		"fixture/goengineering":                    "fixture/goengineering",
		"other.example/goengine":                   "other.example/goengine",
		"fixture/goengine_test":                    "fixture/go_engine_test",
		"fixture/goengine [fixture/goengine.test]": "fixture/go_engine [fixture/go_engine.test]",
	} {
		if got := mapping.NewPackageId(old); got != want {
			test.Errorf("%s maps to %s, want %s", old, got, want)
		}
	}
}

// A package with only test files has its clause in a _test.go file, and it moves and takes the new name the same
// way: crosscompile's only file is cross_compile_test.go.
func TestFolderNamesRenameAPackageWithOnlyTestFiles(test *testing.T) {
	test.Parallel()
	root := writeRepository(test, map[string]string{
		"go.work": "go 1.27\n\nuse .\n",
		"go.mod":  "module fixture\n\ngo 1.27\n",
		"crosscompile/cross_compile_test.go": `package crosscompile

import "testing"

func TestCompiles(test *testing.T) {}
`,
	})
	moves := []folderMove{{OldFolder: "crosscompile", NewFolder: "cross_compile", NewPackage: "cross_compile"}}
	var output bytes.Buffer
	if exitCode := run(root, moves, false, &output); exitCode != 0 {
		test.Fatalf("exit %d:\n%s", exitCode, output.String())
	}
	want := `package cross_compile

import "testing"

func TestCompiles(test *testing.T) {}
`
	if got := readFile(test, root, "cross_compile/cross_compile_test.go"); got != want {
		test.Errorf("cross_compile_test.go:\n%s\nwant:\n%s", got, want)
	}
	if exists(root, "crosscompile") || !strings.Contains(output.String(), "1 package clauses") {
		test.Errorf("the folder did not move or the clause was not counted:\n%s", output.String())
	}
}
