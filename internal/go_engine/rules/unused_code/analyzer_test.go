package unused_code

import (
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"text/scanner"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/unused"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// expectation is what the original's tests expect of an object, as their notes name it.
type expectation string

const (
	shouldBeUsed   expectation = "used"
	shouldBeUnused expectation = "unused"
	shouldBeQuiet  expectation = "quiet"
)

// expectationKey is the object a note names: its bare name, and the file and line it is declared on. The original's
// tests note that the key isn't great: because of generics, several objects can be declared at one place, and it
// works because instantiations are never nodes.
type expectationKey struct {
	name string
	file string
	line int
}

// judgment is one object's verdict, as the expectations are checked against it.
type judgment struct {
	shortName string
	position  token.Position
	state     expectation
}

// fixtureActions is each fixture package's action of the original, of originalReport, which reports the original's
// verdict, and of the port, a package's test variant among them, in the order the checker returned them.
type fixtureActions struct {
	original         []*checker.Action
	originalReported []*checker.Action
	port             []*checker.Action
}

// fixtureRun is the original and the port run once over the copy of the original's testdata, which every test here
// reads, so the fixtures are loaded and checked once per test binary.
var fixtureRun = sync.OnceValues(analyzeFixtures)

// analyzeFixtures loads every package of testdata/src/example.com, as the original's TestAll names them, and runs the
// original, its report and the port over them in one checker.
func analyzeFixtures() (fixtureActions, error) {
	directory := analysistest.TestData()
	paths, globError := filepath.Glob(filepath.Join(directory, "src", "example.com", "*"))
	if globError != nil {
		return fixtureActions{}, globError
	}
	patterns := []string{}
	for _, path := range paths {
		patterns = append(patterns, filepath.Join("example.com", filepath.Base(path)))
	}
	// The fixtures are a tree laid out by import path, loaded as analysistest loads one, test variants included.
	loaded, loadError := load.Packages(directory, patterns, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		return fixtureActions{}, fmt.Errorf("loading the fixtures: %w", loadError)
	}
	// The checker skips a package that does not type-check, so a fixture with an error would compare nothing and still
	// read green: every fixture must load clean, and a load of none would read green the same way.
	if len(loaded.Roots) == 0 {
		return fixtureActions{}, fmt.Errorf("loading the fixtures %v found no package", patterns)
	}
	for _, rootPackage := range loaded.Roots {
		if len(rootPackage.Errors) > 0 {
			return fixtureActions{}, fmt.Errorf("the fixture %s does not load clean: %v", rootPackage.ID,
				rootPackage.Errors)
		}
	}
	graph, analyzeError := checker.Analyze(
		[]*analysis.Analyzer{unused.Analyzer.Analyzer, originalReport, Analyzer}, loaded.Roots, &checker.Options{},
	)
	if analyzeError != nil {
		return fixtureActions{}, fmt.Errorf("analyzing the fixtures: %w", analyzeError)
	}
	actions := fixtureActions{}
	for _, action := range graph.Roots {
		if action.Err != nil {
			return fixtureActions{}, fmt.Errorf("running %s: %w", action, action.Err)
		}
		switch action.Analyzer {
		case unused.Analyzer.Analyzer:
			actions.original = append(actions.original, action)
		case originalReport:
			actions.originalReported = append(actions.originalReported, action)
		case Analyzer:
			actions.port = append(actions.port, action)
		}
	}
	return actions, nil
}

// The original's own tests, its TestAll, ported: every package of its testdata, test variants included, and every
// object each declares is used, unused or quiet as the notes beside it say, with a note for every object the verdict
// names in the package's files. The port meets every expectation the original meets.
func TestPortMeetsTheOriginalsExpectations(test *testing.T) {
	test.Parallel()
	actions, runError := fixtureRun()
	if runError != nil {
		test.Fatal(runError)
	}
	met := 0
	for _, action := range actions.port {
		verdict, isVerdict := action.Result.(result)
		if !isVerdict {
			test.Fatalf("the port's result over %s is %T", action.Package, action.Result)
		}
		met += checkExpectations(test, action, judgmentsOfPort(action.Package.Fset, verdict))
	}
	test.Logf("the port met %d expectations over %d packages", met, len(actions.port))
	if met == 0 {
		test.Fatal("no expectation was read, so meeting them proves nothing")
	}
}

// The same expectations, held to the original as it runs here: the reader of the notes reads what the original's
// harness reads, so the port is held to the original's own bar and no other.
func TestOriginalMeetsItsOwnExpectations(test *testing.T) {
	test.Parallel()
	actions, runError := fixtureRun()
	if runError != nil {
		test.Fatal(runError)
	}
	met := 0
	for _, action := range actions.original {
		verdict, isVerdict := action.Result.(unused.Result)
		if !isVerdict {
			test.Fatalf("the original's result over %s is %T", action.Package, action.Result)
		}
		met += checkExpectations(test, action, judgmentsOfOriginal(verdict))
	}
	test.Logf("the original met %d expectations over %d packages", met, len(actions.original))
	if met == 0 {
		test.Fatal("no expectation was read, so meeting them proves nothing")
	}
}

// judgmentsOfPort is the port's verdict as the expectations are checked against it.
func judgmentsOfPort(fileSet *token.FileSet, verdict result) []judgment {
	judgments := []judgment{}
	add := func(objects []object, state expectation) {
		for _, each := range objects {
			judgments = append(judgments, judgment{
				shortName: each.shortName,
				position:  fileSet.PositionFor(each.position, false),
				state:     state,
			})
		}
	}
	add(verdict.used, shouldBeUsed)
	add(verdict.unused, shouldBeUnused)
	add(verdict.quiet, shouldBeQuiet)
	return judgments
}

// judgmentsOfOriginal is the original's verdict as the expectations are checked against it.
func judgmentsOfOriginal(verdict unused.Result) []judgment {
	judgments := []judgment{}
	add := func(objects []unused.Object, state expectation) {
		for _, each := range objects {
			judgments = append(judgments, judgment{shortName: each.ShortName, position: each.Position, state: state})
		}
	}
	add(verdict.Used, shouldBeUsed)
	add(verdict.Unused, shouldBeUnused)
	add(verdict.Quiet, shouldBeQuiet)
	return judgments
}

// checkExpectations holds judgments to the notes in action's package, as the original's check does, and returns how
// many it met. A note marked _test applies only to the package's test variant, and the others only to the package
// without its tests.
func checkExpectations(test *testing.T, action *checker.Action, judgments []judgment) int {
	test.Helper()
	fileSet := action.Package.Fset
	isTest := false
	for _, file := range action.Package.Syntax {
		if strings.HasSuffix(fileSet.Position(file.Pos()).Filename, "_test.go") {
			isTest = true
			break
		}
	}

	want := map[expectationKey]expectation{}
	files := map[string]bool{}
	for _, file := range action.Package.Syntax {
		filename := fileSet.Position(file.Pos()).Filename
		if !strings.HasSuffix(filename, ".go") {
			continue
		}
		files[filename] = true
		notes, readError := notesOf(file)
		if readError != nil {
			test.Fatalf("%s: %v", filename, readError)
		}
		for _, read := range notes {
			position := fileSet.PositionFor(read.position, false)
			state, appliesHere, malformed := expectationOf(read, isTest)
			if malformed {
				test.Fatalf("malformed directive at %s", position)
			}
			if appliesHere {
				want[expectationKey{read.arguments[0].text, position.Filename, position.Line}] = state
			}
		}
	}

	met := 0
	for _, judged := range judgments {
		if !files[judged.position.Filename] {
			continue
		}
		key := expectationKey{judged.shortName, judged.position.Filename, judged.position.Line}
		expected, found := want[key]
		if !found {
			test.Errorf("object at %s (%s) shouldn't exist but is %s (tests = %t)", relativePosition(judged.position),
				judged.shortName, judged.state, isTest)
			continue
		}
		delete(want, key)
		if judged.state != expected {
			test.Errorf("object at %s (%s) should be %s but is %s (tests = %t)", relativePosition(judged.position),
				judged.shortName, expected, judged.state, isTest)
			continue
		}
		met++
	}

	for key, expected := range want {
		test.Errorf("object at %s:%d should be %s but wasn't seen", relativePath(key.file), key.line, expected)
	}
	return met
}

// expectationOf is what read expects, and whether it applies to the package or its test variant as isTest says,
// or that it is malformed: used and used_test name an object and whether it is used, quiet and quiet_test an object
// that is quiet. A note of any other name applies nowhere.
func expectationOf(read note, isTest bool) (expectation, bool, bool) {
	switch read.name {
	case "quiet", "quiet_test":
		if len(read.arguments) != 1 || read.arguments[0].kind != scanner.String {
			return "", false, true
		}
		return shouldBeQuiet, isTest == (read.name == "quiet_test"), false
	case "used", "used_test":
		if len(read.arguments) != 2 || read.arguments[0].kind != scanner.String ||
			read.arguments[1].kind != scanner.Ident ||
			(read.arguments[1].text != "true" && read.arguments[1].text != "false") {
			return "", false, true
		}
		state := shouldBeUnused
		if read.arguments[1].text == "true" {
			state = shouldBeUsed
		}
		return state, isTest == (read.name == "used_test"), false
	}
	return "", false, false
}

// relativePosition is position's file relative to the working directory, its line and its column, as the original's
// tests print an object's place.
func relativePosition(position token.Position) string {
	text := position.Filename
	if position.IsValid() {
		if text != "" {
			text = relativePath(text) + ":"
		}
		text += strconv.Itoa(position.Line)
		if position.Column != 0 {
			text += ":" + strconv.Itoa(position.Column)
		}
	}
	if text == "" {
		text = "-"
	}
	return text
}

// relativePath is path relative to the working directory, or path itself where it has none.
func relativePath(path string) string {
	relative, relativeError := filepath.Rel(analysistest.TestData(), path)
	if relativeError != nil {
		return path
	}
	return filepath.Join("testdata", relative)
}

// note is one note an expectation comment holds, `used("name", true)`: its name, its arguments and where its
// comment is.
type note struct {
	name      string
	arguments []noteArgument
	position  token.Pos
}

// noteArgument is one argument of a note: a string, unquoted, or an identifier such as true.
type noteArgument struct {
	kind rune
	text string
}

// notesOf reads every note in file's comments: a comment whose text begins with @, or holds //@ after other text, is
// a comma-separated list of notes.
//
// It is the grammar of x/tools' expect package, which the original's tests read their notes with, cut to what those
// notes use: a name and a parenthesized list of strings and identifiers. The package left x/tools for a module of its
// own, deprecated, that cohere does not require, so the port reads the notes itself rather than add it.
func notesOf(file *ast.File) ([]note, error) {
	notes := []note{}
	for _, group := range file.Comments {
		for _, comment := range group.List {
			text, isNote := noteText(comment.Text)
			if !isNote {
				continue
			}
			parsed, parseError := parseNotes(text, comment.Pos())
			if parseError != nil {
				return nil, fmt.Errorf("reading the notes in %q: %w", comment.Text, parseError)
			}
			notes = append(notes, parsed...)
		}
	}
	return notes, nil
}

// noteText is the notes a comment holds, the text after its @, as the expect package finds them: a comment that
// begins with @, or one that holds //@ after other text.
func noteText(comment string) (string, bool) {
	text := comment
	if strings.HasPrefix(text, "/*") {
		text = strings.TrimSuffix(text, "*/")
	}
	text = text[2:]
	if index := strings.Index(text, "@"); index > 2 {
		if text[index-2:index] != "//" {
			return "", false
		}
		text = text[index:]
	}
	return strings.CutPrefix(text, "@")
}

// parseNotes reads text as a comma-separated list of notes, each placed at position.
func parseNotes(text string, position token.Pos) ([]note, error) {
	var reader scanner.Scanner
	reader.Init(strings.NewReader(text))
	reader.Mode = scanner.GoTokens
	var scanError error
	reader.Error = func(_ *scanner.Scanner, message string) {
		if scanError == nil {
			scanError = fmt.Errorf("%s", message)
		}
	}
	notes := []note{}
	for {
		switch reader.Scan() {
		case scanner.EOF:
			return notes, scanError
		case scanner.Ident:
		default:
			return nil, fmt.Errorf("expected a note's name, found %q", reader.TokenText())
		}
		parsed, parseError := parseNote(&reader, position)
		if parseError != nil {
			return nil, parseError
		}
		notes = append(notes, parsed)
		switch reader.Scan() {
		case scanner.EOF:
			return notes, scanError
		case ',':
		default:
			return nil, fmt.Errorf("expected a comma between notes, found %q", reader.TokenText())
		}
	}
}

// parseNote reads one note's arguments, reader having read its name.
func parseNote(reader *scanner.Scanner, position token.Pos) (note, error) {
	parsed := note{name: reader.TokenText(), position: position}
	if reader.Scan() != '(' {
		return note{}, fmt.Errorf("expected ( after %s, found %q", parsed.name, reader.TokenText())
	}
	for {
		switch kind := reader.Scan(); kind {
		case ')':
			return parsed, nil
		case scanner.String:
			unquoted, unquoteError := strconv.Unquote(reader.TokenText())
			if unquoteError != nil {
				return note{}, unquoteError
			}
			parsed.arguments = append(parsed.arguments, noteArgument{kind, unquoted})
		case scanner.Ident:
			parsed.arguments = append(parsed.arguments, noteArgument{kind, reader.TokenText()})
		default:
			return note{}, fmt.Errorf("unexpected %q in the arguments of %s", reader.TokenText(), parsed.name)
		}
		switch reader.Scan() {
		case ')':
			return parsed, nil
		case ',':
		default:
			return note{}, fmt.Errorf("expected , or ) in the arguments of %s, found %q", parsed.name,
				reader.TokenText())
		}
	}
}
