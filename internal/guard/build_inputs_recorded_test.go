package guard

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Every test that starts the go command on what it reads from disk records that reading with
// build_inputs.Record, in the same function, so Go's test cache keys the result on it (#h1e0b8b).
//
// Go keys a cached test result on the linked test binary and on what the test process itself opens and reads. A
// `go build` subprocess reads sources Go never sees, and code only the built binary reaches is dead in the test
// binary, so before #4p82x8d a pass replayed across an edit to exactly the code under test, under the landing
// gate too. build_inputs.Record closed every site there was; this keeps a new one from opening.
//
// It reads every _test.go file of ours as syntax. A call is the go command when it is os/exec's Command or
// CommandContext and its program is the literal "go". Its subcommand is the literal after the program, or the
// first element of an append to a []string literal; one it cannot read is taken as reading sources, so a
// spelling this does not follow is refused rather than passed. The subcommands that read packages are build,
// generate, install, list, run, test and vet; env, mod, tool, version and the rest do not.
//
// A command is covered one of two ways, each through whatever name the file imports build_inputs as:
//   - a call of Record anywhere in the same top-level declaration, closures included, as a sync.Once around the
//     build is;
//   - its Dir set from build_inputs.OutsideModule, for a module the test wrote somewhere of its own. Go rechecks
//     only files inside the test's module, so a command run outside it reads nothing the cache could miss, and
//     OutsideModule fails the test when the directory is not outside, so the claim is checked when it runs. A
//     command with no Dir runs in its package's directory, inside the module, and needs Record.
func TestEveryGoCommandATestStartsRecordsWhatItReads(test *testing.T) {
	test.Parallel()
	module, absolutePathError := filepath.Abs(filepath.Join("..", ".."))
	if absolutePathError != nil {
		test.Fatal(absolutePathError)
	}
	files := ourTestFiles(test, module)
	uncovered := []string{}
	started := 0
	for _, path := range files {
		fileSet := token.NewFileSet()
		file, parseError := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
		if parseError != nil {
			test.Fatal(parseError)
		}
		commands, unrecorded := goCommandsWithoutRecord(fileSet, file)
		started += commands
		for _, position := range unrecorded {
			relative, relativeError := filepath.Rel(module, position.Filename)
			if relativeError != nil {
				test.Fatal(relativeError)
			}
			uncovered = append(uncovered, filepath.ToSlash(relative)+":"+strconv.Itoa(position.Line))
		}
	}
	// A walk that found none would pass over a tree nobody examined.
	if started == 0 {
		test.Fatalf("found no go command started in %d test files, so a clean answer would mean nothing", len(files))
	}
	if len(uncovered) > 0 {
		sort.Strings(uncovered)
		test.Errorf(
			"%d of %d go commands tests start read from disk in a process of their own and record nothing, so "+
				"Go's test cache can replay a pass across an edit only that process reads (#4p82x8d). Call "+
				"build_inputs.Record(test, <its directory>, <its flags and packages>) in the same function:\n  %s",
			len(uncovered),
			started,
			strings.Join(uncovered, "\n  "),
		)
	}
}

// The guard refuses each spelling of the go command that reads sources with no Record beside it, and passes the
// ones that record, the subcommands that read nothing, and other programs.
func TestTheRecordGuardTellsACoveredGoCommandFromAnUncoveredOne(test *testing.T) {
	test.Parallel()
	const fixture = `package fixture

import (
	"context"
	"os/exec"
	shell "os/exec"
	"testing"

	inputs "github.com/system-inc/cohere/internal/build_inputs"
	other "example.com/other"
)

func uncoveredBuild(test *testing.T) {
	exec.Command("go", "build", "-o", "binary", ".") // want
}

func uncoveredContext(test *testing.T) {
	exec.CommandContext(context.Background(), "go", "test", "./...") // want
}

func uncoveredAppendedList(test *testing.T, arguments ...string) {
	exec.Command("go", append([]string{"list"}, arguments...)...) // want
}

func uncoveredAlias(test *testing.T) {
	shell.Command("go", "run", "./cmd/show") // want
}

func uncoveredUnknown(test *testing.T, arguments []string) {
	exec.Command("go", arguments...) // want
}

func uncoveredOtherRecord(test *testing.T) {
	other.Record(test, ".", ".")
	exec.Command("go", "vet", ".") // want
}

func uncoveredPlainDirectory(test *testing.T, fixture string) {
	command := exec.Command("go", "build", ".") // want
	command.Dir = fixture
}

func uncoveredAnotherCommandOutside(test *testing.T, fixture string) {
	outside := exec.Command("go", "list", ".")
	outside.Dir = inputs.OutsideModule(test, fixture)
	inside := exec.Command("go", "build", ".") // want
	_ = inside
}

func coveredOutside(test *testing.T, fixture string) {
	command := exec.Command("go", "test", "./...")
	command.Dir, command.Env = inputs.OutsideModule(test, fixture), nil
	run := func() {
		var later *exec.Cmd
		later = exec.Command("go", "run", ".")
		later.Dir = inputs.OutsideModule(test, fixture)
	}
	run()
}

func coveredInAClosure(test *testing.T) {
	inputs.Record(test, ".", ".")
	build := func() { exec.Command("go", "build", ".") }
	build()
}

func coveredAfter(test *testing.T) {
	exec.Command("go", "install", ".")
	inputs.Record(test, ".", ".")
}

func readsNothing(test *testing.T) {
	exec.Command("go", "env", "GOROOT")
	exec.Command("go", "mod", "edit", "-json")
	exec.Command("git", "build")
}
`
	fileSet := token.NewFileSet()
	file, parseError := parser.ParseFile(fileSet, "fixture_test.go", fixture, parser.ParseComments)
	if parseError != nil {
		test.Fatal(parseError)
	}
	wanted := []int{}
	for index, line := range strings.Split(fixture, "\n") {
		if strings.HasSuffix(line, "// want") {
			wanted = append(wanted, index+1)
		}
	}
	commands, unrecorded := goCommandsWithoutRecord(fileSet, file)
	got := []int{}
	for _, position := range unrecorded {
		got = append(got, position.Line)
	}
	if strings.Join(lineNumbers(got), " ") != strings.Join(lineNumbers(wanted), " ") {
		test.Errorf("refused lines %v, want %v", got, wanted)
	}
	if commands != 15 {
		test.Errorf("counted %d go commands, want 15: eight refused, five covered and two reading nothing", commands)
	}
}

// lineNumbers spells each line number, so two lists compare as text.
func lineNumbers(lines []int) []string {
	spelled := make([]string, len(lines))
	for index, line := range lines {
		spelled[index] = strconv.Itoa(line)
	}
	return spelled
}

// ourTestFiles is every _test.go file the module's packages hold: the TypeScript submodule and third_party's
// vendored copies, which are upstream's, directories whose name begins with a dot, node_modules and testdata left
// out, the last since its files are fixtures some test reads, not tests that run.
func ourTestFiles(test *testing.T, module string) []string {
	test.Helper()
	files := []string{}
	walkError := filepath.WalkDir(module, func(path string, entry fs.DirEntry, pathError error) error {
		if pathError != nil {
			return pathError
		}
		if entry.IsDir() {
			if path != module && (path == filepath.Join(module, "TypeScript") ||
				path == filepath.Join(module, "third_party") ||
				strings.HasPrefix(entry.Name(), ".") || entry.Name() == "node_modules" ||
				entry.Name() == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(entry.Name(), "_test.go") && !strings.HasPrefix(entry.Name(), ".") {
			files = append(files, path)
		}
		return nil
	})
	if walkError != nil {
		test.Fatal(walkError)
	}
	return files
}

// readsPackages are the go subcommands that read packages from disk.
var readsPackages = map[string]bool{
	"build":    true,
	"generate": true,
	"install":  true,
	"list":     true,
	"run":      true,
	"test":     true,
	"vet":      true,
}

// buildInputsPath is the package whose Record covers a go command.
const buildInputsPath = "github.com/system-inc/cohere/internal/build_inputs"

// goCommandsWithoutRecord counts the go commands a file starts and returns where each that reads packages sits
// in a top-level declaration with no call of build_inputs' Record.
func goCommandsWithoutRecord(fileSet *token.FileSet, file *ast.File) (int, []token.Position) {
	execNames := importNames(file, "os/exec")
	buildInputsNames := importNames(file, buildInputsPath)
	commands := 0
	unrecorded := []token.Position{}
	for _, declaration := range file.Decls {
		recorded := false
		reading := []*ast.CallExpr{}
		holders := commandHolders{heldIn: map[*ast.CallExpr]string{}, outside: map[string]bool{}}
		ast.Inspect(declaration, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.AssignStmt:
				holders.note(typed.Lhs, typed.Rhs, buildInputsNames)
			case *ast.ValueSpec:
				names := make([]ast.Expr, len(typed.Names))
				for index, name := range typed.Names {
					names[index] = name
				}
				holders.note(names, typed.Values, buildInputsNames)
			case *ast.CallExpr:
				if isPackageCall(typed, buildInputsNames, "Record") {
					recorded = true
					return true
				}
				subcommand, isGo := goSubcommand(typed, execNames)
				if !isGo {
					return true
				}
				commands++
				if subcommand == "" || readsPackages[subcommand] {
					reading = append(reading, typed)
				}
			}
			return true
		})
		if recorded {
			continue
		}
		for _, call := range reading {
			if name, held := holders.heldIn[call]; held && holders.outside[name] {
				continue
			}
			unrecorded = append(unrecorded, fileSet.Position(call.Pos()))
		}
	}
	return commands, unrecorded
}

// commandHolders is, for one declaration, the variable each go command is held in, and the variables whose Dir is
// set from build_inputs.OutsideModule.
type commandHolders struct {
	heldIn  map[*ast.CallExpr]string
	outside map[string]bool
}

// note reads one assignment of values to targets into the holders. Only an assignment of one value to each target
// is read; one call that yields several values sets no Dir.
func (holders *commandHolders) note(targets []ast.Expr, values []ast.Expr, buildInputsNames map[string]bool) {
	if len(targets) != len(values) {
		return
	}
	for index, target := range targets {
		value := values[index]
		if call, isCall := value.(*ast.CallExpr); isCall {
			if identifier, isIdentifier := target.(*ast.Ident); isIdentifier {
				holders.heldIn[call] = identifier.Name
			}
		}
		selector, isSelector := target.(*ast.SelectorExpr)
		if !isSelector || selector.Sel.Name != "Dir" {
			continue
		}
		holder, isIdentifier := selector.X.(*ast.Ident)
		call, isCall := value.(*ast.CallExpr)
		if isIdentifier && isCall && isPackageCall(call, buildInputsNames, "OutsideModule") {
			holders.outside[holder.Name] = true
		}
	}
}

// importNames is every name a file refers to an import path by: its own name, or the alias it is imported as.
func importNames(file *ast.File, importPath string) map[string]bool {
	names := map[string]bool{}
	for _, imported := range file.Imports {
		unquoted, unquoteError := strconv.Unquote(imported.Path.Value)
		if unquoteError != nil || unquoted != importPath {
			continue
		}
		if imported.Name != nil {
			names[imported.Name.Name] = true
			continue
		}
		names[importPath[strings.LastIndex(importPath, "/")+1:]] = true
	}
	return names
}

// isPackageCall reports whether call is function of a package imported under one of names.
func isPackageCall(call *ast.CallExpr, names map[string]bool, function string) bool {
	selector, isSelector := call.Fun.(*ast.SelectorExpr)
	if !isSelector || selector.Sel.Name != function {
		return false
	}
	identifier, isIdentifier := selector.X.(*ast.Ident)
	return isIdentifier && names[identifier.Name]
}

// goSubcommand reports whether call starts the go command through os/exec, and with which subcommand, empty when
// it cannot be read.
func goSubcommand(call *ast.CallExpr, execNames map[string]bool) (string, bool) {
	arguments := call.Args
	switch {
	case isPackageCall(call, execNames, "Command"):
	case isPackageCall(call, execNames, "CommandContext") && len(arguments) > 0:
		arguments = arguments[1:]
	default:
		return "", false
	}
	if len(arguments) == 0 || stringLiteral(arguments[0]) != "go" {
		return "", false
	}
	if len(arguments) == 1 {
		return "", true
	}
	if subcommand := stringLiteral(arguments[1]); subcommand != "" {
		return subcommand, true
	}
	if appended, isCall := arguments[1].(*ast.CallExpr); isCall && len(appended.Args) > 0 {
		if function, isIdentifier := appended.Fun.(*ast.Ident); isIdentifier && function.Name == "append" {
			if literal, isLiteral := appended.Args[0].(*ast.CompositeLit); isLiteral && len(literal.Elts) > 0 {
				return stringLiteral(literal.Elts[0]), true
			}
		}
	}
	return "", true
}

// stringLiteral is the value of a string literal, or empty when expression is not one.
func stringLiteral(expression ast.Expr) string {
	literal, isLiteral := expression.(*ast.BasicLit)
	if !isLiteral || literal.Kind != token.STRING {
		return ""
	}
	value, unquoteError := strconv.Unquote(literal.Value)
	if unquoteError != nil {
		return ""
	}
	return value
}
