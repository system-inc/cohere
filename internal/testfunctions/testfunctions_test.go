package testfunctions

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"testing"
)

// fixture is a test file with every kind of test function, each signed right or wrong, and the functions go test
// would refuse, which are the ones named Wrong.
const fixture = `package fixture

import "testing"

func TestRight(test *testing.T)               {}
func TestWrong(count int)                    {}
func Test(test *testing.T)                    {}
func Testify(count int)                       {}
func TestMain(main *testing.M)                {}
func BenchmarkRight(benchmark *testing.B)     {}
func BenchmarkWrong(test *testing.T)          {}
func FuzzRight(fuzz *testing.F)               {}
func FuzzWrong(fuzz *testing.F) error         { return nil }
func TestGenericWrong[Value any](test *testing.T) {}
func Test_Underscore(left, right *testing.T)  {}

type suite struct{}

func (each suite) TestMethod(count int) {}
`

var fileSetOfFixture = token.NewFileSet()

// parsedFixture is the fixture parsed once for every test here.
var parsedFixture = func() []*ast.File {
	file, parseError := parser.ParseFile(fileSetOfFixture, "fixture_test.go", fixture, parser.AllErrors)
	if parseError != nil {
		// Invariant: the fixture is a constant that parses.
		panic(parseError)
	}
	return []*ast.File{file}
}()

// Every refused function is named with go test's own words, at its line, and nothing else is: not a lower-case
// continuation like Testify, and not a method.
func TestProblemsNameEveryRefusedFunctionAndNoOther(test *testing.T) {
	test.Parallel()

	var named []string
	for _, problem := range Problems(fileSetOfFixture, parsedFixture) {
		named = append(named, problem.Position.String()+" "+problem.Message)
	}
	want := []string{
		"fixture_test.go:6:1 wrong signature for TestWrong, must be: func TestWrong(t *testing.T)",
		"fixture_test.go:11:1 wrong signature for BenchmarkWrong, must be: func BenchmarkWrong(b *testing.B)",
		"fixture_test.go:13:1 wrong signature for FuzzWrong, must be: func FuzzWrong(f *testing.F)",
		"fixture_test.go:14:1 test functions cannot have type parameters",
		"fixture_test.go:15:1 wrong signature for Test_Underscore, must be: func Test_Underscore(t *testing.T)",
	}
	if !slices.Equal(named, want) {
		test.Fatalf("want\n%q\ngot\n%q", want, named)
	}
}

// TestMain is a test when it takes a *testing.T, and otherwise must take a *testing.M.
func TestProblemsReadTestMainByItsParameter(test *testing.T) {
	test.Parallel()

	for declaration, want := range map[string]string{
		"func TestMain(test *testing.T) {}": "",
		"func TestMain(count int) {}":       "wrong signature for TestMain, must be: func TestMain(m *testing.M)",
	} {
		test.Run(declaration, func(test *testing.T) {
			test.Parallel()

			fileSet := token.NewFileSet()
			file, parseError := parser.ParseFile(fileSet, "main_test.go", "package fixture\n\n"+declaration+"\n", 0)
			if parseError != nil {
				test.Fatal(parseError)
			}
			var messages []string
			for _, problem := range Problems(fileSet, []*ast.File{file}) {
				messages = append(messages, problem.Message)
			}
			if want == "" && len(messages) > 0 || want != "" && !slices.Equal(messages, []string{want}) {
				test.Fatalf("want %q, got %q", want, messages)
			}
		})
	}
}
