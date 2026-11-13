package guard

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Every COHERE_ variable a test sets is read by something, so a test can't believe it switched a behavior off
// with a name nothing honors (#ketvj0d).
//
// Five tests set `COHERE_RUN_CACHE=off` for a child cohere, a name a31bc10d replaced with --no-cache, so nothing
// had read it since. A test that sets a variable to change what it watches, and is wrong about the name, watches
// whatever the default does, and nothing says so. This reads every _test.go of ours as syntax and collects each
// COHERE_ name a test sets: a string literal "COHERE_NAME=...", an os or testing Setenv of the name, or a
// constant holding the name joined to "=...". Each must be read somewhere:
//   - mentioned in any source of ours that is not a Go test, in any language, since a program a test starts may
//     be cohere, the Swift engine, a script or an editor extension;
//   - read by a Go test with os.Getenv or os.LookupEnv, of the literal or of a constant holding it, the way a
//     test binary started again as its own helper child reads its marker;
//   - read as `$NAME` or `${NAME}` inside a string literal of a Go test, a shell script the test writes;
//   - read by needs.OptIn, whose second argument is the name of the variable it reads (#xc29pnd).
//
// testdata is left out on both sides: its files are fixtures some test reads, not tests that run.
func TestEveryCohereVariableATestSetsIsRead(test *testing.T) {
	test.Parallel()
	module, absolutePathError := filepath.Abs(filepath.Join("..", ".."))
	if absolutePathError != nil {
		test.Fatal(absolutePathError)
	}
	testFiles := ourTestFiles(test, module)
	sets := map[string][]string{}
	readByTests := map[string]bool{}
	// One directory's constants, read once for all of its test files.
	constantsByDirectory := map[string]map[string]string{}
	for _, path := range testFiles {
		fileSet := token.NewFileSet()
		file, parseError := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
		if parseError != nil {
			test.Fatal(parseError)
		}
		directory := filepath.Dir(path)
		if constantsByDirectory[directory] == nil {
			constantsByDirectory[directory] = packageConstants(test, directory)
		}
		found := cohereVariablesOf(fileSet, file, constantsByDirectory[directory])
		for name, positions := range found.set {
			for _, position := range positions {
				relative, relativeError := filepath.Rel(module, position.Filename)
				if relativeError != nil {
					test.Fatal(relativeError)
				}
				sets[name] = append(sets[name], filepath.ToSlash(relative)+":"+strconv.Itoa(position.Line))
			}
		}
		for name := range found.read {
			readByTests[name] = true
		}
	}
	// A walk that found none would pass over a tree nobody examined.
	if len(sets) == 0 {
		test.Fatalf("found no COHERE_ variable set in %d test files, so a clean answer would mean nothing",
			len(testFiles))
	}
	mentioned := mentionedOutsideTests(test, module, sets)
	unread := []string{}
	for name, places := range sets {
		if readByTests[name] || mentioned[name] {
			continue
		}
		sort.Strings(places)
		unread = append(unread, name+", set at "+strings.Join(places, ", "))
	}
	if len(unread) > 0 {
		sort.Strings(unread)
		test.Errorf(
			"%d of %d COHERE_ variables tests set are read by nothing, so setting them changes nothing and a "+
				"test relying on one watches the default instead. Use the switch the code reads, or delete the "+
				"variable:\n  %s",
			len(unread),
			len(sets),
			strings.Join(unread, "\n  "),
		)
	}
}

// The guard collects each way a test sets a COHERE_ variable and each way it reads one, and nothing else.
func TestTheVariableGuardTellsASetFromARead(test *testing.T) {
	test.Parallel()
	const fixture = `package fixture

import (
	"os"
	"os/exec"
	"testing"
)

const helperVariable = "COHERE_HELPER"

const markerVariable = "COHERE_MARKER"

func sets(test *testing.T, command *exec.Cmd) {
	command.Env = append(os.Environ(), "COHERE_LITERAL=off")
	command.Env = append(command.Env, helperVariable+"=1")
	test.Setenv("COHERE_TESTING_SETENV", "1")
	os.Setenv(markerVariable, "1")
	_ = "COHERE_NOT_A_SET"
	_ = "OTHER_NAME=1"
}

func reads() {
	_ = os.Getenv("COHERE_LITERAL_READ")
	_, _ = os.LookupEnv(markerVariable)
	_ = "#!/bin/sh\necho $COHERE_SCRIPT ${COHERE_BRACED}\n"
	_ = needs.OptIn(nil, "COHERE_OPTED_IN", "a census")
	_ = other.OptIn(nil, "COHERE_OTHERS_OPT_IN", "not needs")
}
`
	fileSet := token.NewFileSet()
	file, parseError := parser.ParseFile(fileSet, "fixture_test.go", fixture, parser.SkipObjectResolution)
	if parseError != nil {
		test.Fatal(parseError)
	}
	found := cohereVariablesOf(fileSet, file, constantsOf(file))
	set := sortedKeys(found.set)
	read := sortedKeys(found.read)
	if want := "COHERE_HELPER COHERE_LITERAL COHERE_MARKER COHERE_TESTING_SETENV"; strings.Join(set, " ") != want {
		test.Errorf("collected as set %v, want %s", set, want)
	}
	if want := "COHERE_BRACED COHERE_LITERAL_READ COHERE_MARKER COHERE_OPTED_IN COHERE_SCRIPT"; strings.Join(read,
		" ") != want {
		test.Errorf("collected as read %v, want %s", read, want)
	}
}

func sortedKeys[Value any](values map[string]Value) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// cohereVariables is what one test file sets and reads of the COHERE_ variables, set ones with where.
type cohereVariables struct {
	set  map[string][]token.Position
	read map[string]bool
}

// cohereName is a COHERE_ variable's name.
var cohereName = regexp.MustCompile(`^COHERE_[A-Z0-9_]+$`)

// scriptReference is a COHERE_ variable read by a shell script: `$NAME` or `${NAME}`.
var scriptReference = regexp.MustCompile(`\$\{?(COHERE_[A-Z0-9_]+)`)

// cohereVariablesOf collects, from one file, the COHERE_ variables it sets and the ones it reads. constants maps
// each string constant its package declares to its value, so a name held in a constant is followed.
func cohereVariablesOf(fileSet *token.FileSet, file *ast.File, constants map[string]string) cohereVariables {
	found := cohereVariables{set: map[string][]token.Position{}, read: map[string]bool{}}
	nameOf := func(expression ast.Expr) string {
		if value := stringLiteral(expression); cohereName.MatchString(value) {
			return value
		}
		if identifier, isIdentifier := expression.(*ast.Ident); isIdentifier &&
			cohereName.MatchString(constants[identifier.Name]) {
			return constants[identifier.Name]
		}
		return ""
	}
	ast.Inspect(file, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.BasicLit:
			value := stringLiteral(typed)
			if name, _, isAssignment := strings.Cut(value, "="); isAssignment && cohereName.MatchString(name) {
				found.set[name] = append(found.set[name], fileSet.Position(typed.Pos()))
			}
			for _, match := range scriptReference.FindAllStringSubmatch(value, -1) {
				found.read[match[1]] = true
			}
		case *ast.BinaryExpr:
			// A name in a constant joined to its value: helperVariable+"=1".
			if typed.Op != token.ADD || !strings.HasPrefix(stringLiteral(typed.Y), "=") {
				return true
			}
			if name := nameOf(typed.X); name != "" {
				found.set[name] = append(found.set[name], fileSet.Position(typed.Pos()))
			}
		case *ast.CallExpr:
			// needs.OptIn(test, name, why) reads its second argument, inside package needs as OptIn alone.
			if function, isIdentifier := typed.Fun.(*ast.Ident); isIdentifier && function.Name == "OptIn" &&
				len(typed.Args) > 1 {
				if name := nameOf(typed.Args[1]); name != "" {
					found.read[name] = true
				}
			}
			if selector, isSelector := typed.Fun.(*ast.SelectorExpr); isSelector && selector.Sel.Name == "OptIn" &&
				len(typed.Args) > 1 {
				if owner, isIdentifier := selector.X.(*ast.Ident); isIdentifier && owner.Name == "needs" {
					if name := nameOf(typed.Args[1]); name != "" {
						found.read[name] = true
					}
				}
			}
			selector, isSelector := typed.Fun.(*ast.SelectorExpr)
			if !isSelector || len(typed.Args) == 0 {
				return true
			}
			name := nameOf(typed.Args[0])
			if name == "" {
				return true
			}
			switch selector.Sel.Name {
			case "Setenv":
				found.set[name] = append(found.set[name], fileSet.Position(typed.Pos()))
			case "Getenv", "LookupEnv":
				found.read[name] = true
			}
		}
		return true
	})
	return found
}

// packageConstants is every string constant the Go files of one directory declare, by name, test files
// included, since a constant a test sets a variable through may be declared in either.
func packageConstants(test *testing.T, directory string) map[string]string {
	test.Helper()
	paths, globError := filepath.Glob(filepath.Join(directory, "*.go"))
	if globError != nil {
		test.Fatal(globError)
	}
	constants := map[string]string{}
	for _, path := range paths {
		file, parseError := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if parseError != nil {
			test.Fatal(parseError)
		}
		for name, value := range constantsOf(file) {
			constants[name] = value
		}
	}
	return constants
}

// constantsOf is every constant a file declares whose value is one string literal.
func constantsOf(file *ast.File) map[string]string {
	constants := map[string]string{}
	for _, declaration := range file.Decls {
		general, isGeneral := declaration.(*ast.GenDecl)
		if !isGeneral || general.Tok != token.CONST {
			continue
		}
		for _, specification := range general.Specs {
			value, isValue := specification.(*ast.ValueSpec)
			if !isValue || len(value.Names) != len(value.Values) {
				continue
			}
			for index, name := range value.Names {
				if literal := stringLiteral(value.Values[index]); literal != "" {
					constants[name.Name] = literal
				}
			}
		}
	}
	return constants
}

// sourceExtensions are the files a program a test starts may be written in: cohere and its tools, the Swift
// engine, scripts, and the editor extensions.
var sourceExtensions = map[string]bool{
	".go": true, ".swift": true, ".sh": true, ".zsh": true, ".bash": true,
	".js": true, ".mjs": true, ".cjs": true, ".ts": true, ".py": true,
}

// mentionedOutsideTests reports, of the names given, the ones a source of ours that is not a test mentions.
func mentionedOutsideTests(test *testing.T, module string, names map[string][]string) map[string]bool {
	test.Helper()
	mentioned := map[string]bool{}
	walkError := filepath.WalkDir(module, func(path string, entry fs.DirEntry, pathError error) error {
		if pathError != nil {
			return pathError
		}
		if entry.IsDir() {
			if path != module && (path == filepath.Join(module, "TypeScript") ||
				strings.HasPrefix(entry.Name(), ".") || entry.Name() == "node_modules" ||
				entry.Name() == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if !sourceExtensions[filepath.Ext(path)] || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		contents, readError := os.ReadFile(path)
		if readError != nil {
			return readError
		}
		for name := range names {
			if !mentioned[name] && strings.Contains(string(contents), name) {
				mentioned[name] = true
			}
		}
		return nil
	})
	if walkError != nil {
		test.Fatal(walkError)
	}
	return mentioned
}
