package guard

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// optionsDisplayName is the uncounted options accessor, rule.Given's ForDisplay.
const optionsDisplayName = "ForDisplay"

// optionsDisplayHome is the directory the accessor is declared in, relative to the module root.
const optionsDisplayHome = "internal/lint/rule"

// optionsDisplayReaders are the directories allowed to reach the accessor: its own package, and the test
// harness, whose docs capture records what each run was handed.
var optionsDisplayReaders = map[string]bool{optionsDisplayHome: true, "internal/lint/testing": true}

// No rule reads its options through the uncounted accessor.
//
// rule.OptionsReads counts every Given.Read so the registry can tell a rule that reached its options from one
// that ran without reaching them (#zwd43jn). Given.ForDisplay returns the same value uncounted, for the docs
// capture's record of what a run was handed, and a rule reading through it would look like a rule that never
// reaches its options while it does, or worse, hide one that reaches them only on some paths. Two censuses
// keep it the harness's: every method named ForDisplay in the module is Given's in internal/lint/rule, so the
// name means that method and nothing else, and every reference to a selector of that name, called or taken as
// a value, is in internal/lint/rule or internal/lint/testing.
func TestOnlyTheHarnessReadsOptionsUncounted(test *testing.T) {
	test.Parallel()

	// This package sits two directories below the module root.
	module, absolutePathError := filepath.Abs(filepath.Join("..", ".."))
	if absolutePathError != nil {
		test.Fatal(absolutePathError)
	}
	for _, problem := range optionsDisplayProblems(test, module) {
		test.Error(problem)
	}
}

// The guard can fail both ways, on a tree written for it: a second method of the name, and a reference from
// a rule package. The clean tree passes, so the failures are the planted shapes and not the walk.
func TestTheOptionsDisplayGuardSeesWhatItRefuses(test *testing.T) {
	test.Parallel()

	declaration := `package rule
type Given[Options any] struct{ value Options; configured bool }
func (options Given[Options]) ForDisplay() (Options, bool) { return options.value, options.configured }`
	harness := `package rule_testing
func record(options rule.Given[int]) { _, _ = options.ForDisplay() }`
	testCases := []struct {
		name  string
		files map[string]string
		// want is a fragment each expected problem carries, in order, and empty for a clean tree.
		want []string
	}{
		{
			name: "only Given declares it and only the harness reads it",
			files: map[string]string{
				"internal/lint/rule/given.go":      declaration,
				"internal/lint/testing/capture.go": harness,
			},
		},
		{
			name: "a rule package calls it",
			files: map[string]string{
				"internal/lint/rule/given.go":      declaration,
				"internal/lint/testing/capture.go": harness,
				"internal/lint/rules/probe/probe.go": `package probe
func run(options rule.Given[int]) bool { enabled, _ := options.ForDisplay(); return enabled > 0 }`,
			},
			want: []string{"internal/lint/rules/probe/probe.go:2 reaches ForDisplay"},
		},
		{
			// A method value dodges a census of calls, so every selector is counted.
			name: "a rule package takes it as a value",
			files: map[string]string{
				"internal/lint/rule/given.go":      declaration,
				"internal/lint/testing/capture.go": harness,
				"internal/lint/rules/probe/probe.go": `package probe
func run(options rule.Given[int]) bool { read := options.ForDisplay; enabled, _ := read(); return enabled > 0 }`,
			},
			want: []string{"internal/lint/rules/probe/probe.go:2 reaches ForDisplay"},
		},
		{
			// Another type's method of the name would make a reference ambiguous, so it is refused itself.
			name: "another type declares a method of the name",
			files: map[string]string{
				"internal/lint/rule/given.go":      declaration,
				"internal/lint/testing/capture.go": harness,
				"internal/lint/rules/probe/probe.go": `package probe
type settings struct{}
func (value settings) ForDisplay() string { return "" }`,
			},
			want: []string{"internal/lint/rules/probe/probe.go:3 declares ForDisplay on settings"},
		},
		{
			name:  "the declaration is gone",
			files: map[string]string{"internal/lint/testing/capture.go": harness},
			want:  []string{"found 0 declarations of ForDisplay"},
		},
		{
			name:  "nothing reads it",
			files: map[string]string{"internal/lint/rule/given.go": declaration},
			want:  []string{"found no reference to ForDisplay"},
		},
	}
	for _, testCase := range testCases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()

			module := test.TempDir()
			for relative, source := range testCase.files {
				path := filepath.Join(module, filepath.FromSlash(relative))
				if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
					test.Fatal(makeError)
				}
				if writeError := os.WriteFile(path, []byte(source), 0o644); writeError != nil {
					test.Fatal(writeError)
				}
			}
			problems := optionsDisplayProblems(test, module)
			if len(problems) != len(testCase.want) {
				test.Fatalf("got %d problems, want %d: %q", len(problems), len(testCase.want), problems)
			}
			for index, want := range testCase.want {
				if !strings.Contains(problems[index], want) {
					test.Errorf("problem %q does not say %q", problems[index], want)
				}
			}
		})
	}
}

// optionsDisplayProblems reads every Go file under module, tests included, and says what breaks the accessor's
// two censuses, sorted. Skipped, as the gofmt guard skips them, are the TypeScript submodule, directories whose name
// begins with a dot, and node_modules, none of which can import cohere's rule package.
func optionsDisplayProblems(test *testing.T, module string) []string {
	test.Helper()

	var problems []string
	declarations := 0
	references := 0
	walkError := filepath.WalkDir(module, func(path string, entry fs.DirEntry, pathError error) error {
		if pathError != nil {
			return pathError
		}
		if entry.IsDir() {
			if path != module && (path == filepath.Join(module, "TypeScript") ||
				strings.HasPrefix(entry.Name(), ".") || entry.Name() == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".go") || strings.HasPrefix(entry.Name(), ".") {
			return nil
		}
		relative, relativeError := filepath.Rel(module, path)
		if relativeError != nil {
			return relativeError
		}
		relative = filepath.ToSlash(relative)
		directory := filepath.ToSlash(filepath.Dir(relative))

		fileSet := token.NewFileSet()
		parsed, parseError := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
		if parseError != nil {
			return parseError
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.FuncDecl:
				if typed.Name.Name != optionsDisplayName || typed.Recv == nil {
					return true
				}
				declarations++
				receiver := receiverTypeName(typed.Recv.List[0].Type)
				if directory != optionsDisplayHome || receiver != "Given" {
					problems = append(problems, relative+":"+lineOf(fileSet, typed.Pos())+" declares "+
						optionsDisplayName+" on "+receiver+", so the name no longer means only rule.Given's "+
						"uncounted read and the reference census cannot tell them apart; name it otherwise")
				}
			case *ast.SelectorExpr:
				if typed.Sel.Name != optionsDisplayName {
					return true
				}
				references++
				if !optionsDisplayReaders[directory] {
					problems = append(problems, relative+":"+lineOf(fileSet, typed.Pos())+" reaches "+
						optionsDisplayName+", which reads a rule's options without moving rule.OptionsReads; "+
						"a rule reads its options through Read, so the registry can see it reach them")
				}
			}
			return true
		})
		return nil
	})
	if walkError != nil {
		test.Fatalf("walking %s: %v", module, walkError)
	}

	// Each census must have found its subject, or a clean answer would be about a tree nobody examined.
	if declarations == 0 {
		problems = append(problems, "found 0 declarations of "+optionsDisplayName+
			", so the walk missed rule.Given or it was renamed; this guard proved nothing")
	}
	if references == 0 {
		problems = append(problems, "found no reference to "+optionsDisplayName+
			", though the docs capture reads through it; this guard proved nothing")
	}
	sort.Strings(problems)
	return problems
}

// receiverTypeName is the type a method's receiver names, through a pointer and type parameters.
func receiverTypeName(receiver ast.Expr) string {
	switch typed := receiver.(type) {
	case *ast.StarExpr:
		return receiverTypeName(typed.X)
	case *ast.IndexExpr:
		return receiverTypeName(typed.X)
	case *ast.IndexListExpr:
		return receiverTypeName(typed.X)
	case *ast.Ident:
		return typed.Name
	}
	return "an unnamed type"
}

// lineOf is the line a position falls on, as text.
func lineOf(fileSet *token.FileSet, position token.Pos) string {
	return strconv.Itoa(fileSet.Position(position).Line)
}
