package main

import (
	"go/format"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_rename/rename"

	"golang.org/x/tools/go/packages"
)

// passResult is one fixture after the whole pass: the rewritten source, every candidate's decision, and
// what the package check refused.
type passResult struct {
	source     string
	candidates []candidate
	dropped    map[string]string
}

// runPass runs the pass over one fixture file exactly as run does over a package: candidates, the rename
// engine, the splits, and the package check, then the rewrite.
func runPass(test *testing.T, source string) passResult {
	test.Helper()
	checked := typeCheck(test, source)
	candidates := findCandidates(
		checked.packageTypes,
		checked.info,
		checked.fileSet,
		checked.files,
		fixtureWords(test),
		false,
		nil,
	)
	imports := map[string]*packages.Package{}
	for _, imported := range checked.packageTypes.Imports() {
		imports[imported.Path()] = &packages.Package{Types: imported}
	}
	goPackage := &packages.Package{ID: "fixture", PkgPath: "fixture", Fset: checked.fileSet, Syntax: checked.files,
		Types: checked.packageTypes, TypesInfo: checked.info, Imports: imports}
	renamePackage := rename.Package{
		FileSet: checked.fileSet,
		Types:   checked.packageTypes,
		Info:    checked.info,
		Files:   checked.files,
	}
	decided := decidePackage(goPackage, renamePackage, candidates)
	if decided.checkError != nil {
		test.Fatalf("the package check could not run: %v", decided.checkError)
	}
	result := passResult{source: source, candidates: candidates, dropped: map[string]string{}}
	for variable, reason := range decided.dropped {
		result.dropped[variable.Name()] = reason
	}
	if len(decided.edits) > 0 {
		filename := checked.fileSet.PositionFor(checked.files[0].Pos(), false).Filename
		_, rewritten, rewriteError := rename.RewriteFile(filename, decided.edits)
		if rewriteError != nil {
			test.Fatal(rewriteError)
		}
		result.source = string(rewritten)
	}
	return result
}

// fixtureSource wraps a body in a package with the imports the fixtures call.
func fixtureSource(test *testing.T, body string) string {
	test.Helper()
	source := "package fixture\n\nimport (\n\t\"encoding/json\"\n\t\"errors\"\n\t\"io/fs\"\n\t\"os\"\n" +
		"\t\"path/filepath\"\n\t\"strconv\"\n)\n\n" +
		"var _, _, _, _, _, _ = " +
		"json.Unmarshal, errors.New, fs.ValidPath, os.ReadFile, filepath.WalkDir, strconv.Atoi\n\n" +
		"func first() error  { return nil }\nfunc second() error { return nil }\n\n" + body
	formatted, formatError := format.Source([]byte(source))
	if formatError != nil {
		test.Fatalf("fixture does not format: %v\n%s", formatError, source)
	}
	return string(formatted)
}

// TestSplits runs the pass over one function per decision the split makes, and compares the function as
// rewritten, exactly. A case that must be left alone expects its body unchanged and names its category.
func TestSplits(test *testing.T) {
	test.Parallel()
	testCases := []struct {
		name string
		body string
		// want is the body after the pass, or "" when it must be unchanged.
		want string
		// category is the reused error's category when the pass leaves it.
		category string
	}{
		{
			name: "two short declarations become two variables named by their calls",
			body: `func caller(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	value, err := strconv.Atoi(string(data))
	if err != nil {
		return 0, err
	}
	return value, nil
}
`,
			want: `func caller(path string) (int, error) {
	data, readError := os.ReadFile(path)
	if readError != nil {
		return 0, readError
	}
	value, parseError := strconv.Atoi(string(data))
	if parseError != nil {
		return 0, parseError
	}
	return value, nil
}
`,
		},
		{
			name: "a plain assignment becomes a short declaration",
			body: `func caller(path string, value *int) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	err = json.Unmarshal(data, value)
	if err != nil {
		return err
	}
	return nil
}
`,
			want: `func caller(path string, value *int) error {
	data, readError := os.ReadFile(path)
	if readError != nil {
		return readError
	}
	decodeError := json.Unmarshal(data, value)
	if decodeError != nil {
		return decodeError
	}
	return nil
}
`,
		},
		{
			name: "an assignment beside a name of the same scope and a blank becomes a short declaration reusing it",
			body: `func caller(path string) (int, error) {
	value := 0
	_, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	value, err = strconv.Atoi(path)
	if err != nil {
		return 0, err
	}
	_, err = os.ReadFile(path)
	if err != nil {
		return value, err
	}
	return value, nil
}
`,
			want: `func caller(path string) (int, error) {
	value := 0
	_, readError := os.ReadFile(path)
	if readError != nil {
		return 0, readError
	}
	value, parseError := strconv.Atoi(path)
	if parseError != nil {
		return 0, parseError
	}
	_, readError = os.ReadFile(path)
	if readError != nil {
		return value, readError
	}
	return value, nil
}
`,
		},
		{
			name: "webs of the same name share one variable, so it is a plain rename",
			body: `func caller() error {
	_, err := os.ReadFile("a")
	if err != nil {
		return err
	}
	_, err = os.ReadFile("b")
	return err
}
`,
			want: `func caller() error {
	_, readError := os.ReadFile("a")
	if readError != nil {
		return readError
	}
	_, readError = os.ReadFile("b")
	return readError
}
`,
		},
		{
			name: "a zero-valued declaration is claimed by the web that cannot declare",
			body: `type holder struct{ data []byte }

func caller(target *holder) error {
	var err error
	if target.data, err = os.ReadFile("a"); err != nil {
		return err
	}
	value := 0
	value, err = strconv.Atoi(string(target.data))
	if err != nil {
		return err
	}
	_ = value
	return nil
}
`,
			want: `type holder struct{ data []byte }

func caller(target *holder) error {
	var readError error
	if target.data, readError = os.ReadFile("a"); readError != nil {
		return readError
	}
	value := 0
	value, parseError := strconv.Atoi(string(target.data))
	if parseError != nil {
		return parseError
	}
	_ = value
	return nil
}
`,
		},
		{
			name: "branches that merge are one web, and two calls name it differently",
			body: `func caller(condition bool) error {
	var err error
	if condition {
		err = first()
	} else {
		err = second()
	}
	return err
}
`,
			category: "the original keeps err: written by calls that name it differently",
		},
		{
			name: "a loop carries the write inside it to the read after it",
			body: `func caller() error {
	err := first()
	for index := 0; index < 3; index++ {
		err = second()
	}
	return err
}
`,
			category: "the original keeps err: written by calls that name it differently",
		},
		{
			name: "a write in a branch that does not return reaches the read after it",
			body: `func caller(condition bool) error {
	err := first()
	if condition {
		err = second()
	}
	return err
}
`,
			category: "the original keeps err: written by calls that name it differently",
		},
		{
			name: "a goto carries the first write past the second",
			body: `func caller() error {
	err := first()
	if err != nil {
		goto done
	}
	err = second()
done:
	return err
}
`,
			category: "the original keeps err: written by calls that name it differently",
		},
		{
			name: "a deferred closure reading the error keeps it whole",
			body: `func caller() error {
	err := first()
	defer func() { _ = err }()
	err = second()
	return err
}
`,
			category: "read or written inside a function literal",
		},
		{
			name: "a write inside a branch read after the branch is declared by a var before the branch",
			body: `func caller(condition bool) error {
	err := first()
	if err != nil {
		return err
	}
	if condition {
		_, err = os.ReadFile("a")
	} else {
		return nil
	}
	return err
}
`,
			want: `func caller(condition bool) error {
	firstError := first()
	if firstError != nil {
		return firstError
	}
	var readError error
	if condition {
		_, readError = os.ReadFile("a")
	} else {
		return nil
	}
	return readError
}
`,
		},
		{
			name: "a write into a field beside the error is declared by a var above it and its comment",
			body: `type holder struct{ data []byte }

func caller(target *holder) error {
	err := first()
	if err != nil {
		return err
	}
	// The data comes from a.
	target.data, err = os.ReadFile("a")
	size := len(target.data)
	_ = size
	return err
}
`,
			want: `type holder struct{ data []byte }

func caller(target *holder) error {
	firstError := first()
	if firstError != nil {
		return firstError
	}
	var readError error
	// The data comes from a.
	target.data, readError = os.ReadFile("a")
	size := len(target.data)
	_ = size
	return readError
}
`,
		},
		{
			name: "a var is not inserted where its name is already declared in the block",
			body: `type holder struct{ data []byte }

func caller(target *holder) error {
	readError := first()
	err := first()
	if err != nil || readError != nil {
		return err
	}
	target.data, err = os.ReadFile("a")
	return err
}
`,
			category: "readError is already declared in the block holding line 25",
		},
		{
			name: "an assignment in an if statement's init beside an outer name is declared by a var, not a second value",
			body: `func caller(path string) (int, error) {
	value := 0
	_, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	if value, err = strconv.Atoi(path); err != nil {
		return 0, err
	}
	return value, nil
}
`,
			want: `func caller(path string) (int, error) {
	value := 0
	_, readError := os.ReadFile(path)
	if readError != nil {
		return 0, readError
	}
	var parseError error
	if value, parseError = strconv.Atoi(path); parseError != nil {
		return 0, parseError
	}
	return value, nil
}
`,
		},
		{
			name: "a web two calls name keeps err as a variable of its own, and the rest is named",
			body: `func caller() error {
	err := first()
	if err != nil {
		return err
	}
	for err = second(); err == nil; err = first() {
	}
	return nil
}
`,
			want: `func caller() error {
	firstError := first()
	if firstError != nil {
		return firstError
	}
	for err := second(); err == nil; err = first() {
	}
	return nil
}
`,
			category: "a new variable keeps err: written by calls that name it differently",
		},
		{
			name: "a range clause assigning the error is left whole",
			body: `func caller(errors []error) error {
	err := first()
	for _, err = range errors {
	}
	return err
}
`,
			category: "assigned by a range clause",
		},
		{
			name: "a select case receiving into the error is written in its case, not before the select",
			body: `func caller(channel chan error) error {
	err := first()
	if err != nil {
		return err
	}
	select {
	case err = <-channel:
	default:
	}
	return err
}
`,
			category: "the original keeps err: a write is not a call",
		},
	}
	for _, testCase := range testCases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			source := fixtureSource(test, testCase.body)
			result := runPass(test, source)
			want := source
			if testCase.want != "" {
				want = fixtureSource(test, testCase.want)
			}
			if result.source != want {
				test.Fatalf("got:\n%s\nwant:\n%s\ndropped: %v", result.source, want, result.dropped)
			}
			if testCase.category == "" {
				return
			}
			for _, decided := range result.candidates {
				if decided.variable.Name() == "err" && decided.category == testCase.category {
					return
				}
			}
			for _, decided := range result.candidates {
				test.Logf(
					"candidate %s: name %q category %q",
					decided.variable.Name(),
					decided.newName,
					decided.category,
				)
			}
			test.Fatalf("no err candidate with category %q", testCase.category)
		})
	}
}

// TestNamesFromProducers runs the pass over the decisions that name an error from something other than a
// declared function: a function value, a function a call made, a walk callback's parameter, a named result,
// and an abbreviated name.
func TestNamesFromProducers(test *testing.T) {
	test.Parallel()
	testCases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "a function value named with a trailing Function word",
			body: "func caller(walkFunction func() error) error {\n\terr := walkFunction()\n\treturn err\n}\n",
			want: "func caller(walkFunction func() error) error {\n\twalkError := walkFunction()\n\treturn walkError\n}\n",
		},
		{
			name: "a field holding a function",
			body: "type runner struct{ load func() error }\n\nfunc caller(run runner) error {\n\terr := run.load()\n\treturn err\n}\n",
			want: "type runner struct{ load func() error }\n\nfunc caller(run runner) error {\n\tloadError := run.load()\n\treturn loadError\n}\n",
		},
		{
			name: "a function a call made, less its trailing preposition",
			body: "func DecodeOptionsInto[Value any]() func([]byte) (Value, error) { return nil }\n\n" +
				"func caller(raw []byte) (int, error) {\n\tvalue, err := DecodeOptionsInto[int]()(raw)\n\treturn value, err\n}\n",
			want: "func DecodeOptionsInto[Value any]() func([]byte) (Value, error) { return nil }\n\n" +
				"func caller(raw []byte) (int, error) {\n\tvalue, decodeOptionsError := DecodeOptionsInto[int]()(raw)\n\treturn value, decodeOptionsError\n}\n",
		},
		{
			name: "a walk callback's error is the error related to its path",
			body: "func caller() error {\n\treturn filepath.WalkDir(\".\", func(path string, entry fs.DirEntry, err error) error {\n\t\treturn err\n\t})\n}\n",
			want: "func caller() error {\n\treturn filepath.WalkDir(\".\", func(path string, entry fs.DirEntry, pathError error) error {\n\t\treturn pathError\n\t})\n}\n",
		},
		{
			name: "a walk callback's error reused by a later call splits, and the parameter is no longer reassigned",
			body: "func caller(root string) error {\n" +
				"\treturn filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {\n" +
				"\t\tif err != nil {\n\t\t\treturn err\n\t\t}\n\t\trelative, err := filepath.Rel(root, path)\n" +
				"\t\tif err != nil {\n\t\t\treturn err\n\t\t}\n" +
				"\t\t_ = relative\n\t\treturn nil\n\t})\n}\n",
			want: "func caller(root string) error {\n" +
				"\treturn filepath.WalkDir(root, func(path string, entry fs.DirEntry, pathError error) error {\n" +
				"\t\tif pathError != nil {\n\t\t\treturn pathError\n\t\t}\n" +
				"\t\trelative, relativePathError := filepath.Rel(root, path)\n" +
				"\t\tif relativePathError != nil {\n\t\t\treturn relativePathError\n\t\t}\n\t\t_ = relative\n\t\treturn nil\n\t})\n}\n",
		},
		{
			name: "a named result takes its function's name",
			body: "type writer struct{}\n\nfunc (output *writer) Close() (err error) {\n\terr = first()\n\treturn\n}\n",
			want: "type writer struct{}\n\nfunc (output *writer) Close() (closeError error) {\n\tcloseError = first()\n\treturn\n}\n",
		},
		{
			name: "a named result an unrelated call reuses keeps its function's name, and the call's error is its own",
			body: "func moduleAt(directory string) (root string, err error) {\n" +
				"\tabsolute, err := filepath.Abs(directory)\n\tif err != nil {\n" +
				"\t\treturn \"\", err\n\t}\n\treturn absolute, nil\n}\n",
			want: "func moduleAt(directory string) (root string, moduleAtError error) {\n" +
				"\tabsolute, absolutePathError := filepath.Abs(directory)\n" +
				"\tif absolutePathError != nil {\n\t\treturn \"\", absolutePathError\n\t}\n\treturn absolute, nil\n}\n",
		},
		{
			name: "a write a bare return reads stays in the named result",
			body: "func load(path string) (data []byte, err error) {\n\t_, err = os.Stat(path)\n\tif err != nil {\n" +
				"\t\treturn nil, err\n\t}\n" +
				"\tdata, err = os.ReadFile(path)\n\treturn\n}\n",
			want: "func load(path string) (data []byte, loadError error) {\n\t_, statError := os.Stat(path)\n" +
				"\tif statError != nil {\n" +
				"\t\treturn nil, statError\n\t}\n\tdata, loadError = os.ReadFile(path)\n\treturn\n}\n",
		},
		{
			name: "an abbreviated Err ending is spelled out",
			body: "func caller() error {\n\t_, readErr := os.ReadFile(\"a\")\n\treturn readErr\n}\n",
			want: "func caller() error {\n\t_, readError := os.ReadFile(\"a\")\n\treturn readError\n}\n",
		},
		{
			name: "a leading err moves to the end",
			body: "func caller() bool {\n\t_, errOne := os.ReadFile(\"a\")\n\t_, errOther := os.ReadFile(\"b\")\n\treturn errOne == nil && errOther == nil\n}\n",
			want: "func caller() bool {\n\t_, oneError := os.ReadFile(\"a\")\n\t_, otherError := os.ReadFile(\"b\")\n\treturn oneError == nil && otherError == nil\n}\n",
		},
		{
			name: "an error named e is named from its call",
			body: "func caller() error {\n\te := first()\n\treturn e\n}\n",
			want: "func caller() error {\n\tfirstError := first()\n\treturn firstError\n}\n",
		},
	}
	for _, testCase := range testCases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := runPass(test, fixtureSource(test, testCase.body))
			if want := fixtureSource(test, testCase.want); result.source != want {
				test.Fatalf("got:\n%s\nwant:\n%s\ndropped: %v", result.source, want, result.dropped)
			}
		})
	}
}

// TestLeftAlone runs the pass over what it must not change, each with the category it reports.
func TestLeftAlone(test *testing.T) {
	test.Parallel()
	testCases := []struct {
		name     string
		body     string
		variable string
		category string
	}{
		{
			name:     "a function literal called in place names nothing",
			body:     "func caller() error {\n\terr := func() error { return nil }()\n\treturn err\n}\n",
			variable: "err",
			category: "a function literal called in place",
		},
		{
			name:     "a parameter outside a walk callback waits on a rule",
			body:     "func describe(err error) string {\n\treturn err.Error()\n}\n",
			variable: "err",
			category: "parameter",
		},
		{
			name:     "a callback that is not a walk's keeps its parameter",
			body:     "func caller(visit func(func(path string, err error) error)) {\n\tvisit(func(path string, err error) error { return err })\n}\n",
			variable: "err",
			category: "parameter",
		},
		{
			name:     "a function literal's named result has no function name",
			body:     "var caller = func() (err error) { return nil }\n",
			variable: "err",
			category: "named result of a function literal or type",
		},
	}
	for _, testCase := range testCases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			source := fixtureSource(test, testCase.body)
			result := runPass(test, source)
			if result.source != source {
				test.Fatalf("changed:\n%s", result.source)
			}
			for _, decided := range result.candidates {
				if decided.variable.Name() == testCase.variable &&
					decided.newName == "" &&
					decided.category == testCase.category {
					return
				}
			}
			for _, decided := range result.candidates {
				test.Logf(
					"candidate %s: name %q category %q",
					decided.variable.Name(),
					decided.newName,
					decided.category,
				)
			}
			test.Fatalf("no %s candidate with category %q", testCase.variable, testCase.category)
		})
	}
}

// TestALongerNameLandsPastTheWidth confirms the pass leaves width to the line wrap run after it: a rename that
// pushes a line past 120 columns is made, not dropped.
func TestALongerNameLandsPastTheWidth(test *testing.T) {
	test.Parallel()
	body := "func aVeryLongFunctionNameThatNamesTheErrorAtLength() error { return nil }\n\n" +
		"func caller() error {\n" +
		"\tif err := aVeryLongFunctionNameThatNamesTheErrorAtLength(); err != nil && err != os.ErrNotExist {\n" +
		"\t\treturn err\n\t}\n\treturn nil\n}\n"
	result := runPass(test, fixtureSource(test, body))
	if !strings.Contains(result.source, "aVeryLongFunctionNameThatNamesTheErrorAtLengthError != nil") ||
		len(result.dropped) != 0 {
		test.Fatalf("dropped %v, source:\n%s", result.dropped, result.source)
	}
}

// TestGlobalErrorNames names the declarations past one function: a sentinel keeps its err prefix spelled
// out, as ST1012 asks, an error-making function's leading err moves to the end, an Err word is spelled out, a
// field Err becomes Error, and Go's exported ErrSomething convention is kept.
func TestGlobalErrorNames(test *testing.T) {
	test.Parallel()
	testCases := []struct {
		name            string
		packageVariable bool
		newName         string
		renamed         bool
	}{
		{name: "errPoolNeverEmpty", packageVariable: true, newName: "errorPoolNeverEmpty", renamed: true},
		{name: "errMemoryFSReadOnly", packageVariable: true, newName: "errorMemoryFsReadOnly", renamed: true},
		{name: "errNothingToRepeat", newName: "nothingToRepeatError", renamed: true},
		{name: "loadErr", packageVariable: true, newName: "loadError", renamed: true},
		{name: "wantErr", newName: "wantError", renamed: true},
		{name: "Err", newName: "Error", renamed: true},
		{name: "ErrSkipped"},
		{name: "errors"},
		{name: "Error"},
	}
	words := fixtureWords(test)
	for _, testCase := range testCases {
		newName, renamed := globalErrorName(testCase.name, testCase.packageVariable, words)
		if newName != testCase.newName || renamed != testCase.renamed {
			test.Errorf(
				"%s: got %q %v, want %q %v",
				testCase.name,
				newName,
				renamed,
				testCase.newName,
				testCase.renamed,
			)
		}
	}
}

// TestHouseCasing confirms an initialism in a producing function's name is written as a word in the error's
// name, and a walk parameter its function reassigns is renamed all the same, its reassignment finding
// re-worded under the new name.
func TestHouseCasing(test *testing.T) {
	test.Parallel()
	body := "func parseCSS() error { return nil }\n\nfunc caller() error {\n\terr := parseCSS()\n\treturn err\n}\n"
	want := "func parseCSS() error { return nil }\n\nfunc caller() error {\n\tparseCssError := parseCSS()\n\treturn parseCssError\n}\n"
	if result := runPass(test, fixtureSource(test, body)); result.source != fixtureSource(test, want) {
		test.Fatalf("got:\n%s", result.source)
	}
	reassigned := "func caller() error {\n" +
		"\treturn filepath.WalkDir(\".\", func(path string, entry fs.DirEntry, err error) error {\n" +
		"\t\tif err != nil {\n\t\t\terr = nil\n\t\t}\n\t\treturn err\n\t})\n}\n"
	renamed := "func caller() error {\n" +
		"\treturn filepath.WalkDir(\".\", func(path string, entry fs.DirEntry, pathError error) error {\n" +
		"\t\tif pathError != nil {\n\t\t\tpathError = nil\n\t\t}\n\t\treturn pathError\n\t})\n}\n"
	if result := runPass(test, fixtureSource(test, reassigned)); result.source != fixtureSource(test, renamed) {
		test.Fatalf("a reassigned walk parameter, whose reassignment finding is re-worded, got:\n%s", result.source)
	}
}

// TestOnlyErrWordsAreAbbreviatedErrors confirms a name holding an advice-only word and no Err is not this
// pass's (another pass expands it).
func TestOnlyErrWordsAreAbbreviatedErrors(test *testing.T) {
	test.Parallel()
	result := runPass(test, fixtureSource(test, "func caller() int {\n\targsCount := 1\n\treturn argsCount\n}\n"))
	if len(result.candidates) != 0 {
		test.Fatalf("candidates %+v, want none", result.candidates)
	}
}

// TestParameterRuling names error parameters no walk names, by the ruling: the one operation every caller
// hands in, read through the call sites (clause 2), else candidateError for a function that tests the error
// and sourceError for one that turns it into something else or reports it (clause 3), and typeCheckError for
// a go/types Config's Error callback (clause 4).
func TestParameterRuling(test *testing.T) {
	test.Parallel()
	body := `func exitCodeOf(err error) int { return len(err.Error()) }

func isRangeError(err error) bool { return err != nil }

func describe(err error) string { return err.Error() }

func skipWithout(err error) bool { return err == nil }

func callers() {
	err := first()
	_ = exitCodeOf(err)
	_ = exitCodeOf(first())
	_ = isRangeError(first())
	_ = isRangeError(second())
	_ = describe(first())
	_ = describe(second())
	_ = skipWithout(first())
	_ = skipWithout(second())
	fail := func(err error) { _ = err }
	fail(first())
	fail(second())
	_ = types.Config{Error: func(err error) { _ = err }}
}
`
	source := strings.Replace(fixtureSource(test, body), "\"errors\"\n", "\"errors\"\n\t\"go/types\"\n", 1)
	checked := typeCheck(test, source)
	finalNames := map[string]string{}
	candidates := findCandidates(
		checked.packageTypes,
		checked.info,
		checked.fileSet,
		checked.files,
		fixtureWords(test),
		false,
		finalNames,
	)
	goPackage := &packages.Package{
		Fset:      checked.fileSet,
		Syntax:    checked.files,
		Types:     checked.packageTypes,
		TypesInfo: checked.info,
	}
	sites := map[string]map[int][]string{}
	callSites(goPackage, finalNames, fixtureWords(test), sites)
	got := map[string]string{}
	for _, decided := range candidates {
		if decided.pending == nil {
			continue
		}
		name, clause := nameParameter(decided.pending, sites)
		function := decided.pending.function
		if decided.pending.typeCheckErrors {
			function = "types.Config.Error"
		}
		got[function] = name + " by " + clause[:8]
	}
	want := map[string]string{
		"exitCodeOf":         "firstError by clause 2",
		"isRangeError":       "candidateError by clause 3",
		"describe":           "sourceError by clause 3",
		"skipWithout":        "candidateError by clause 3",
		"fail":               "sourceError by clause 3",
		"types.Config.Error": "typeCheckError by clause 4",
	}
	for function, name := range want {
		if got[function] != name {
			test.Errorf("%s: got %q, want %q", function, got[function], name)
		}
	}
	if len(got) != len(want) {
		test.Errorf("got %v, want %v", got, want)
	}
}

// TestReceivesAndWalkParameters names an error received from a channel by the channel where its name says
// what it carries, leaves one named for when, and gives a walk callback's walkError parameter pathError.
func TestReceivesAndWalkParameters(test *testing.T) {
	test.Parallel()
	testCases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "a channel of results",
			body: "func caller(results chan error) error {\n\tselect {\n\tcase err := <-results:\n\t\treturn err\n\t}\n}\n",
			want: "func caller(results chan error) error {\n\tselect {\n\tcase resultError := <-results:\n\t\treturn resultError\n\t}\n}\n",
		},
		{
			name: "a channel of errors",
			body: "func caller(errs chan error) error {\n\terr := <-errs\n\treturn err\n}\n",
			want: "func caller(errs chan error) error {\n\treceivedError := <-errs\n\treturn receivedError\n}\n",
		},
		{
			name: "a channel named for when names nothing",
			body: "func caller(finished chan error) error {\n\terr := <-finished\n\treturn err\n}\n",
		},
		{
			name: "a walk callback's walkError parameter beside the walk's own walkError",
			body: "func caller() error {\n" +
				"\twalkError := filepath.WalkDir(\".\", " +
				"func(path string, entry fs.DirEntry, walkError error) error {\n" +
				"\t\treturn walkError\n\t})\n\treturn walkError\n}\n",
			want: "func caller() error {\n" +
				"\twalkError := filepath.WalkDir(\".\", " +
				"func(path string, entry fs.DirEntry, pathError error) error {\n" +
				"\t\treturn pathError\n\t})\n\treturn walkError\n}\n",
		},
	}
	for _, testCase := range testCases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			source := fixtureSource(test, testCase.body)
			want := source
			if testCase.want != "" {
				want = fixtureSource(test, testCase.want)
			}
			if result := runPass(test, source); result.source != want {
				test.Fatalf("got:\n%s\nwant:\n%s", result.source, want)
			}
		})
	}
}
