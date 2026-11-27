package guard

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// goLanguageDirectory is internal/go_language, relative to the module's root.
const goLanguageDirectory = "internal/go_language"

// No _test.go under internal/go_language starts git itself (#vb50z0q): its tests write the repositories they need
// with internal/testing/written_repository. cohere-go's own `git ls-files` at a go.work's root (project/gitlink.go)
// is the product's child, not a test's, and is not what this checks.
//
// It reads every _test.go file under the directory as syntax, testdata left out (ourTestFiles). A call starts git
// when it is os/exec's Command or CommandContext, through whatever name the file imports os/exec as, and its program
// is the literal "git". A program spelled any other way, a variable or a path, is not followed.
func TestNoTestUnderGoLanguageStartsGit(test *testing.T) {
	test.Parallel()
	module, absolutePathError := filepath.Abs(filepath.Join("..", "..", ".."))
	if absolutePathError != nil {
		test.Fatal(absolutePathError)
	}
	files := ourTestFiles(test, filepath.Join(module, filepath.FromSlash(goLanguageDirectory)))
	// A walk that found none would pass over a tree nobody examined.
	if len(files) == 0 {
		test.Fatalf("found no test file under %s, so a clean answer would mean nothing", goLanguageDirectory)
	}
	started := []string{}
	for _, path := range files {
		for _, position := range gitStartsIn(test, path) {
			relative, relativeError := filepath.Rel(module, position.Filename)
			if relativeError != nil {
				test.Fatal(relativeError)
			}
			started = append(started, filepath.ToSlash(relative)+":"+strconv.Itoa(position.Line))
		}
	}
	if len(started) > 0 {
		slices.Sort(started)
		test.Errorf("%d calls in tests under %s start git, a process not the test's own (#vb50z0q). Write the "+
			"repository with internal/testing/written_repository instead:\n  %s", len(started), goLanguageDirectory,
			strings.Join(started, "\n  "))
	}
}

// The guard finds git started through each spelling it reads, and passes another program and another package's
// Command.
func TestTheGitGuardFindsEachSpellingOfGit(test *testing.T) {
	test.Parallel()
	const planted = `package planted

import (
	"context"
	"os/exec"
	shell "os/exec"

	other "example.com/other"
)

func started() {
	exec.Command("git", "status") // want
	exec.CommandContext(context.Background(), "git", "init") // want
	shell.Command("git") // want
	exec.Command("go", "env", "GOROOT")
	other.Command("git", "status")
	exec.Command(program, "git")
}
`
	wanted := []int{}
	for index, line := range strings.Split(planted, "\n") {
		if strings.HasSuffix(line, "// want") {
			wanted = append(wanted, index+1)
		}
	}
	path := filepath.Join(test.TempDir(), "planted_test.go")
	fileSet := token.NewFileSet()
	file, parseError := parser.ParseFile(fileSet, path, planted, parser.SkipObjectResolution)
	if parseError != nil {
		test.Fatal(parseError)
	}
	got := []int{}
	for _, position := range gitStarts(fileSet, file) {
		got = append(got, position.Line)
	}
	if !slices.Equal(got, wanted) {
		test.Errorf("found git started on lines %v, want %v", got, wanted)
	}
}

// gitStartsIn parses the test file at path and returns where each of its calls starts git (gitStarts).
func gitStartsIn(test *testing.T, path string) []token.Position {
	test.Helper()
	fileSet := token.NewFileSet()
	file, parseError := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
	if parseError != nil {
		test.Fatal(parseError)
	}
	return gitStarts(fileSet, file)
}

// gitStarts is where each call in file starts git: os/exec's Command, or CommandContext past its context, with the
// literal program "git".
func gitStarts(fileSet *token.FileSet, file *ast.File) []token.Position {
	execNames := importNames(file, "os/exec")
	positions := []token.Position{}
	ast.Inspect(file, func(node ast.Node) bool {
		call, isCall := node.(*ast.CallExpr)
		if !isCall {
			return true
		}
		arguments := call.Args
		switch {
		case isPackageCall(call, execNames, "Command"):
		case isPackageCall(call, execNames, "CommandContext") && len(arguments) > 0:
			arguments = arguments[1:]
		default:
			return true
		}
		if len(arguments) > 0 && stringLiteral(arguments[0]) == "git" {
			positions = append(positions, fileSet.Position(call.Pos()))
		}
		return true
	})
	return positions
}
