package high_level_intermediate_representation

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/lint/testing"
)

// reactCompilerFixture is one of React Compiler's vendored fixtures: its path made a file name, and its
// source.
type reactCompilerFixture struct {
	name, source string
}

// reactCompilerFixtures are React Compiler's vendored fixtures in path order, Flow left out, as the
// conformance corpus leaves them out: the parser can't read them.
func reactCompilerFixtures(test *testing.T) []reactCompilerFixture {
	test.Helper()
	root := filepath.Join("..", "..", "rules", "react", "conformance", "testdata", "fixtures")
	var paths []string
	walkError := filepath.WalkDir(root, func(path string, entry fs.DirEntry, pathError error) error {
		if pathError != nil {
			return pathError
		}
		if !entry.IsDir() && !strings.HasSuffix(path, ".md") {
			paths = append(paths, path)
		}
		return nil
	})
	if walkError != nil {
		test.Fatal(walkError)
	}
	sort.Strings(paths)
	var fixtures []reactCompilerFixture
	for _, path := range paths {
		contents, readError := os.ReadFile(path)
		if readError != nil {
			test.Fatal(readError)
		}
		source := string(contents)
		if strings.Contains(source, "@flow") {
			continue
		}
		name, relativePathError := filepath.Rel(root, path)
		if relativePathError != nil {
			test.Fatal(relativePathError)
		}
		fixtures = append(
			fixtures,
			reactCompilerFixture{name: strings.NewReplacer("/", "-", " ", "-").Replace(name), source: source},
		)
	}
	return fixtures
}

// lowerReactCompilerFixture lowers every outermost function in a fixture's source through lower, with a
// real type checker.
func lowerReactCompilerFixture(
	test *testing.T,
	source string,
	lower func(rule.Context, *ast.Node) *Function,
) []*Function {
	test.Helper()
	var lowered []*Function
	probe := rule.Rule[rule.NoOptions]{
		Name:             "react-compiler-fixture-lowering",
		NeedsTypeChecker: true,
		Run: func(ruleContext rule.Context, options rule.Given[rule.NoOptions]) rule.Listeners {
			return rule.Listeners{
				ast.KindSourceFile: func(node *ast.Node) {
					if ruleContext.TypeChecker == nil {
						test.Fatal("the harness produced no type checker, so every reference would lower as a global")
					}
					forEachFunctionLike(node, func(function *ast.Node) {
						if loweredFunction := lower(ruleContext, function); loweredFunction != nil {
							lowered = append(lowered, loweredFunction)
						}
					})
				},
			}
		},
	}
	rule_testing.RunTyped(test, probe, "fixture.tsx", source)
	return lowered
}
