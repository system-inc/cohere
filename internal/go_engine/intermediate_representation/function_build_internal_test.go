package intermediate_representation

// What Function.Build gives a caller whose builder did not run the body (#h0k644s). It is in the package's own tests
// because it reads which builder built what, by building a body alone.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/system-inc/go-types/types"
)

// boundWrapperSource holds a function whose body makes a bound method wrapper, the function a method value closes
// over, and starts it as a goroutine. It imports nothing, so it type-checks with no importer.
const boundWrapperSource = `package bound

type stopper struct{}

func (*stopper) stop() {}

func starts(value *stopper) {
	stop := value.stop
	go stop()
}
`

// A body built alone, on a builder that takes no further turn, leaves the bound method wrapper it queued unbuilt, and
// a Build of the function after it, which finds the body built, returns with the wrapper still unbuilt: the second
// caller's side of Function.Build's doc. Build on the wrapper itself builds it, which is why a reader of a wrapper's
// blocks calls it first.
func TestASecondCallerOfBuildReturnsBeforeAWrapperTheBodyQueued(test *testing.T) {
	test.Parallel()
	fileSet := token.NewFileSet()
	file, parseError := parser.ParseFile(fileSet, "bound.go", boundWrapperSource, parser.SkipObjectResolution)
	if parseError != nil {
		test.Fatal(parseError)
	}
	typeInfo := &types.Info{
		Types:        map[ast.Expr]types.TypeAndValue{},
		Defs:         map[*ast.Ident]types.Object{},
		Uses:         map[*ast.Ident]types.Object{},
		Implicits:    map[ast.Node]types.Object{},
		Scopes:       map[ast.Node]*types.Scope{},
		Selections:   map[*ast.SelectorExpr]*types.Selection{},
		Instances:    map[*ast.Ident]types.Instance{},
		FileVersions: map[*ast.File]string{},
	}
	checked, checkError := (&types.Config{}).Check("bound", fileSet, []*ast.File{file}, typeInfo)
	if checkError != nil {
		test.Fatal(checkError)
	}
	starts := NewProgram(fileSet).CreatePackage(checked, []*ast.File{file}, typeInfo, false).Function("starts")
	BuildBodyAloneForTest(starts)
	if starts.Blocks == nil {
		test.Fatal("starts has no blocks after its body was built")
	}
	var wrapper *Function
	for _, block := range starts.Blocks {
		for _, instruction := range block.Instructions {
			goStatement, isGo := instruction.(*Go)
			if !isGo {
				continue
			}
			if closure, isClosure := goStatement.Call.Value.(*MakeClosure); isClosure {
				wrapper, _ = closure.Function.(*Function)
			}
		}
	}
	if wrapper == nil || wrapper.Package != nil || wrapper.Parent() != nil {
		test.Fatalf("the go statement of starts starts %v, want a wrapper, a function of no package and no parent",
			wrapper)
	}
	if wrapper.Blocks != nil {
		test.Fatal("the wrapper is built by the build of the body alone, so the test holds no wrapper left unbuilt")
	}
	starts.Build()
	if wrapper.Blocks != nil {
		test.Error("a second Build of starts built the wrapper, which Function.Build's doc says it leaves to the " +
			"builder that ran the body")
	}
	wrapper.Build()
	if wrapper.Blocks == nil {
		test.Error("the wrapper has no blocks after its own Build")
	}
}
