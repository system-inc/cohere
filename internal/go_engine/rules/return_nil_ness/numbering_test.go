package return_nil_ness

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rules/build_intermediate_representation"
)

// numberingSource is a package of two functions with pointers enough to number: parameters, a nil constant, calls,
// loads and a phi.
const numberingSource = `package numbered

type node struct{ next *node }

func walk(start *node, limit int) *node {
	current := start
	for index := 0; index < limit && current != nil; index++ {
		current = current.next
	}
	if current == nil {
		return start
	}
	return last(current)
}

func last(from *node) *node {
	for from.next != nil {
		from = from.next
	}
	return from
}
`

// builtNumberingFunctions are numberingSource's walk and last, built.
func builtNumberingFunctions(test *testing.T) []*intermediate_representation.Function {
	test.Helper()
	fileSet := token.NewFileSet()
	file, parseError := parser.ParseFile(fileSet, "numbered.go", numberingSource, 0)
	if parseError != nil {
		test.Fatalf("parsing the source: %v", parseError)
	}
	info := &types.Info{
		Types:      map[ast.Expr]types.TypeAndValue{},
		Defs:       map[*ast.Ident]types.Object{},
		Uses:       map[*ast.Ident]types.Object{},
		Implicits:  map[ast.Node]types.Object{},
		Instances:  map[*ast.Ident]types.Instance{},
		Scopes:     map[ast.Node]*types.Scope{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
	}
	configuration := types.Config{Importer: importer.Default()}
	packageTypes, checkError := configuration.Check("numbered", fileSet, []*ast.File{file}, info)
	if checkError != nil {
		test.Fatalf("type checking the source: %v", checkError)
	}
	irPackage := build_intermediate_representation.NewPackage(fileSet, packageTypes, []*ast.File{file}, info,
		func(*types.Func) bool { return false }, false)
	var functions []*intermediate_representation.Function
	for _, name := range []string{"walk", "last"} {
		object, isFunction := packageTypes.Scope().Lookup(name).(*types.Func)
		if !isFunction {
			test.Fatalf("the source declares no function %s", name)
		}
		function := irPackage.Program.FunctionValue(object)
		function.Build()
		functions = append(functions, function)
	}
	return functions
}

// numberingValues is every value function's instructions name, operands and the instructions that are values, in
// block order, the way a judgment meets them, repeats among them.
func numberingValues(function *intermediate_representation.Function) []intermediate_representation.Value {
	var values []intermediate_representation.Value
	var operands []*intermediate_representation.Value
	for _, block := range function.Blocks {
		for _, instruction := range block.Instructions {
			operands = instruction.Operands(operands[:0])
			for _, operand := range operands {
				if *operand != nil {
					values = append(values, *operand)
				}
			}
			if value, isValue := instruction.(intermediate_representation.Value); isValue {
				values = append(values, value)
			}
		}
	}
	return values
}

// A numbering gives each value the number a map numbering values in the order first asked gives it, however the Ids
// fall: asked of walk's values and then of last's, whose instructions' Ids are walk's again, those of last's that find
// their Id claimed by one of walk's are numbered beside the parameters and constants. And a numbering taken from the
// pool and given back costs no allocation (#fex6s0v), where one made afresh made its map and grew it for every
// function.
//
// Not parallel: testing.AllocsPerRun reads the process's allocation counters, which a test running beside it would
// add to.
func TestANumberingNumbersAsTheMapDidAndAllocatesNothingOnceWarm(test *testing.T) {
	functions := builtNumberingFunctions(test)
	var asked []intermediate_representation.Value
	for _, function := range functions {
		asked = append(asked, numberingValues(function)...)
	}

	values := numberingOf(functions[0])
	expected := map[intermediate_representation.Value]int{}
	for index, value := range asked {
		number, numbered := expected[value]
		if !numbered {
			number = len(expected)
			expected[value] = number
		}
		if got := values.number(value); got != number {
			test.Fatalf("value %d asked, %s, numbered %d, where the map numbered it %d", index, value.Name(), got,
				number)
		}
	}
	claimedByAnother := 0
	for value := range values.others {
		if _, isInstruction := value.(intermediate_representation.Instruction); isInstruction {
			claimedByAnother++
		}
	}
	if claimedByAnother == 0 {
		test.Errorf("no instruction of last found its Id claimed, so numbering one whose Id is taken went untested")
	}
	values.release()

	allocations := testing.AllocsPerRun(100, func() {
		values := numberingOf(functions[0])
		for _, value := range asked {
			values.number(value)
		}
		values.release()
	})
	test.Logf("numbering %d values from the pool allocated %.2f times a run", len(asked), allocations)
	// Below one rather than zero: a collection during the runs empties the pool, and the next run makes a numbering.
	if allocations >= 1 {
		test.Errorf("numbering %d values from the pool allocated %.2f times a run, want none", len(asked),
			allocations)
	}
}
