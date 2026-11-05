// Package flagged holds sources that reach a walk outside the package with nothing declaring it, each of which the
// rule must flag where it is. A want's regexp matches the backtick around the marker with a dot, since a raw string
// cannot hold one.
package flagged

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func visit(path string, entry fs.DirEntry, walkError error) error { return walkError }

func straightToWalkDir() {
	_ = filepath.WalkDir("../..", visit) // want `^This path takes a directory walk outside its package, and nothing declares what the walk reads, so a change to those files won't rerun this package's tests: they stay green on a stale result and the red surfaces later on someone else's change\. Declare it with a .// Reads: <glob>. comment on this line or just above it, one glob per comment, and a matching reads line in cloud/fast-gate/executors\.txt\.$`
}

func throughAVariable() {
	root := "../.." // want `This path takes a directory walk outside its package`
	_ = filepath.Walk(root, func(path string, information fs.FileInfo, walkError error) error { return walkError })
}

// A joined constant handed to a helper that a TempDir test also calls.
func censusOfTheRepository(test *testing.T) []string {
	return censusIn(test, filepath.Join("..", "..")) // want `This path takes a directory walk outside its package`
}

func censusOfAFixture(test *testing.T) []string {
	return censusIn(test, test.TempDir())
}

func censusIn(test *testing.T, root string) []string {
	var files []string
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkError error) error {
		files = append(files, path)
		return walkError
	})
	return files
}

func absolute() {
	root, absoluteError := filepath.Abs("../..") // want `This path takes a directory walk outside its package`
	if absoluteError != nil {
		return
	}
	_, _ = os.ReadDir(filepath.Join(root, "internal"))
}

const repository = "../../.."

func namedConstant() {
	_, _ = os.ReadDir(repository) // want `This path takes a directory walk outside its package`
}

// The test hands "../.." to checkMirrors, which hands it on to goFiles, which walks it.
func TestMirrors(test *testing.T) {
	test.Parallel()
	if problems := checkMirrors("../.."); len(problems) > 0 { // want `This path takes a directory walk outside its package`
		test.Error(problems)
	}
}

// A walk up to the repository root, flagged where the root-finding function is called.
func repositoryRoot(test *testing.T) string {
	test.Helper()
	directory, workingError := os.Getwd()
	if workingError != nil {
		test.Fatal(workingError)
	}
	for {
		data, readError := os.ReadFile(filepath.Join(directory, "go.mod"))
		if readError == nil && strings.Contains(string(data), "module github.com/system-inc/cohere\n") {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			test.Fatal("no repository root above the working directory")
		}
		directory = parent
	}
}

func TestEveryTest(test *testing.T) {
	test.Parallel()
	root := repositoryRoot(test) // want `This path takes a directory walk outside its package`
	_ = eachTest(root)
}

func eachTest(root string) error {
	return filepath.WalkDir(root, visit)
}

// A walk-up written inline, with a for clause, is flagged where it starts from the working directory.
func inlineWalkUp() {
	working, _ := os.Getwd() // want `This path takes a directory walk outside its package`
	for directory := working; ; directory = filepath.Dir(directory) {
		if _, statError := os.Stat(filepath.Join(directory, "go.mod")); statError == nil {
			_, _ = filepath.Glob(filepath.Join(directory, "cmd", "*", "*_test.go"))
			return
		}
	}
}

func glob() {
	_, _ = filepath.Glob("../*/testdata/*.json") // want `This path takes a directory walk outside its package`
}

func globThroughAParameter() {
	_ = goFilesIn("../lower") // want `This path takes a directory walk outside its package`
}

func goFilesIn(directory string) []string {
	files, _ := filepath.Glob(filepath.Join(directory, "*.go"))
	return files
}

// A ReadDir loop that recurses by hand walks as far as WalkDir would.
func readDirectoryLoop() {
	_ = listAll("../../internal") // want `This path takes a directory walk outside its package`
}

func listAll(directory string) []string {
	entries, readError := os.ReadDir(directory)
	if readError != nil {
		return nil
	}
	var names []string
	for _, entry := range entries {
		path := filepath.Join(directory, entry.Name())
		names = append(names, path)
		if entry.IsDir() {
			names = append(names, listAll(path)...)
		}
	}
	return names
}

func fileSystem() {
	_ = fs.WalkDir(os.DirFS("../.."), ".", visit) // want `This path takes a directory walk outside its package`
}

func parseDirectory() {
	_, _ = parser.ParseDir(token.NewFileSet(), "../load", nil, 0) // want `This path takes a directory walk outside its package`
}

func concatenated() {
	_, _ = os.ReadDir("../" + "stage1") // want `This path takes a directory walk outside its package`
}

func walkedInAClosure() {
	root := filepath.Join("..", "..", "cmd") // want `This path takes a directory walk outside its package`
	walk := func() error { return filepath.WalkDir(root, visit) }
	_ = walk()
}

func emptyGlob() {
	// Reads:
	_ = filepath.WalkDir("../..", visit) // want `This path takes a directory walk outside its package`
}

func twoGlobsInOneComment() {
	// Reads: internal/*.go cmd/*.go
	_ = filepath.WalkDir("../..", visit) // want `This path takes a directory walk outside its package`
}

func noSpaceAfterTheSlashes() {
	//Reads: internal/**
	_ = filepath.WalkDir("../..", visit) // want `This path takes a directory walk outside its package`
}

func noSpaceAfterTheColon() {
	// Reads:internal/**
	_ = filepath.WalkDir("../..", visit) // want `This path takes a directory walk outside its package`
}

// The planner reads a declaration where the marker opens a line comment, so a sentence that mentions it, or a block
// comment, declares nothing.
func mentionedInASentence() {
	// This walk Reads: internal/**
	_ = filepath.WalkDir("../..", visit) // want `This path takes a directory walk outside its package`
}

func inABlockComment() {
	/* Reads: internal/** */
	_ = filepath.WalkDir("../..", visit) // want `This path takes a directory walk outside its package`
}

func declaredTooFarAbove() {
	// Reads: internal/**
	_ = os.Getenv("HOME")
	_ = filepath.WalkDir("../..", visit) // want `This path takes a directory walk outside its package`
}

// The comment belongs on the source, not on the walk in the helper the source reaches.
func declaredOnTheWalkInstead() {
	_ = declaredWalk("../..") // want `This path takes a directory walk outside its package`
}

func declaredWalk(root string) error {
	// Reads: internal/**
	return filepath.WalkDir(root, visit)
}

// A path out of the package joined with values the rule doesn't read is judged by its start.
func siblingSlices() {
	for _, slice := range []string{"flagged", "selector", "values"} {
		_, _ = os.ReadDir(filepath.Join("..", slice)) // want `This path takes a directory walk outside its package`
	}
}

// A path out of the package and back into another one leaves it, and the helper's join is read at each call.
func intoAnotherPackage() {
	_ = globUnder("../other") // want `This path takes a directory walk outside its package`
	_ = globUnder("../flagged")
}

func globUnder(root string) []string {
	paths, _ := filepath.Glob(filepath.Join(root, "testdata", "*.a"))
	return paths
}

// A helper that calls itself with a path joined onto its parameter still settles.
func recursiveJoin() {
	deeper("../..") // want `This path takes a directory walk outside its package`
}

func deeper(directory string) {
	if _, readError := os.ReadDir(directory); readError != nil {
		return
	}
	deeper(filepath.Join(directory, "next"))
}
