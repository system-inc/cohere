package intermediate_representation_test

// The irutil helpers against staticcheck's own (#q1vc0fd). Each helper translated from irutil is asked the same
// question of our IR and go/ir's, both lifted as buildir lifts, built from one load: the same function, block or
// call on each side, by its place in the package's source functions. The answers must agree everywhere, and the
// fixture holds cases that answer each question both ways, so agreement isn't agreement on a constant.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"slices"
	"testing"

	"honnef.co/go/tools/go/ir"
	"honnef.co/go/tools/go/ir/irutil"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// irutilFixture is the package the helpers are asked about. Its imports are all here, since the importer reads
// this file's alone.
const irutilFixture = `package helpers

import (
	"strings"
	"sync"
)

type speaker interface{ speak() string }

type dog struct{ name string }

func (d dog) speak() string { return d.name }

var shared int

var mutex sync.Mutex

func generic[T any](value T) T { return value }

func external() int

func returnsConstant() int { return 1 }

func returnsParameter(value int) int { return value }

func returnsGlobal() int { return shared }

func returnsAllocation() *int { return new(int) }

func returnsInterface() any { return 1 }

func panics() { panic("no") }

func callsTrivial() int { return returnsConstant() }

func callsExternal() int { return external() }

func adds(left, right int) int { return left + right }

func callsNontrivial() int { return adds(1, 2) }

func pingsPong(depth int) int { return pongsPing(depth) }

func pongsPing(depth int) int { return pingsPong(depth) }

func calls(text string, values []int, talker speaker, callback func() int) int {
	total := len(values)
	if strings.Contains(text, "x") {
		mutex.Lock()
		defer mutex.Unlock()
	}
	total += generic(total)
	total += callback()
	total += len(talker.speak())
	go returnsConstant()
	closure := func() int { return total }
	total += closure()
	total += func() int { return 2 }()
	return total
}

func branches(limit int) int {
	total := 0
	for index := 0; index < limit; index++ {
		if index%2 == 0 {
			continue
		}
		total += index
	}
	switch {
	case total > 10:
		return total
	case total < 0:
		panic("negative")
	}
	return -total
}

func ExampleInSource() {}
`

// irutilFixtureTest is the fixture's _test.go file, where an Example function is an example.
const irutilFixtureTest = `package helpers

func ExampleSpeaker() {
	_ = func() {}
}

func ExampleDog_speak() {}

func NotAnExample() {}
`

// irutilUnits are the units the helpers are asked about: the fixture, and the differential's own units.
func irutilUnits(test *testing.T) []differentialUnit {
	test.Helper()
	fileSet := token.NewFileSet()
	var files []*ast.File
	for _, each := range []struct{ name, source string }{
		{"helpers.go", irutilFixture},
		{"helpers_test.go", irutilFixtureTest},
	} {
		file, parseError := parser.ParseFile(fileSet, each.name, each.source, parser.ParseComments)
		if parseError != nil {
			test.Fatal(parseError)
		}
		files = append(files, file)
	}
	typeInfo := differentialTypeInfo()
	configuration := &types.Config{Importer: differentialImporter(test, irutilFixture)}
	checked, checkError := configuration.Check("helpers", fileSet, files, typeInfo)
	if checkError != nil {
		test.Fatal(checkError)
	}
	fixture := differentialUnit{path: "helpers", fileSet: fileSet, types: checked, files: files, typeInfo: typeInfo}
	return append([]differentialUnit{fixture}, differentialTestdataUnits(test)...)
}

// irutilFunctions builds unit both ways, lifted, and pairs their source functions, which it requires to agree in
// number, name and block count.
func irutilFunctions(test *testing.T, unit differentialUnit) ([]*ir.Function,
	[]*intermediate_representation.Function) {
	test.Helper()
	// buildir's mode but for GlobalDebug, as the lifted differential builds it.
	theirProgram := ir.NewProgram(unit.fileSet, 0)
	theirProgram.SetNoReturn(differentialNoReturn)
	ourProgram := intermediate_representation.NewProgram(unit.fileSet)
	ourProgram.SetNoReturn(differentialNoReturn)
	for _, imported := range unit.types.Imports() {
		theirProgram.CreatePackage(imported, nil, nil, true)
		ourProgram.CreatePackage(imported, nil, nil, true)
	}
	theirPackage := theirProgram.CreatePackage(unit.types, unit.files, unit.typeInfo, false)
	theirPackage.Build()
	ourPackage := ourProgram.CreatePackage(unit.types, unit.files, unit.typeInfo, false)
	ourPackage.Build()
	theirFunctions := differentialTheirSourceFunctions(theirPackage)
	ourFunctions := differentialOurSourceFunctions(ourPackage)
	if len(theirFunctions) != len(ourFunctions) {
		test.Fatalf("%s: go/ir has %d source functions, ours %d", unit.path, len(theirFunctions), len(ourFunctions))
	}
	for index, theirs := range theirFunctions {
		ours := ourFunctions[index]
		if theirs.RelString(nil) != ours.RelativeString(nil) || len(theirs.Blocks) != len(ours.Blocks) {
			test.Fatalf("%s: function %d is %s with %d blocks in go/ir, %s with %d in ours", unit.path, index,
				theirs.RelString(nil), len(theirs.Blocks), ours.RelativeString(nil), len(ours.Blocks))
		}
	}
	return theirFunctions, ourFunctions
}

// irutilTheirCalls are the calls, gos and defers in function, in block and instruction order.
func irutilTheirCalls(function *ir.Function) []*ir.CallCommon {
	var calls []*ir.CallCommon
	for _, block := range function.Blocks {
		for _, instruction := range block.Instrs {
			if call, isCall := instruction.(ir.CallInstruction); isCall {
				calls = append(calls, call.Common())
			}
		}
	}
	return calls
}

// irutilOurCalls is irutilTheirCalls for our IR.
func irutilOurCalls(function *intermediate_representation.Function) []*intermediate_representation.CallCommon {
	var calls []*intermediate_representation.CallCommon
	for _, block := range function.Blocks {
		for _, instruction := range block.Instructions {
			if call, isCall := instruction.(intermediate_representation.CallInstruction); isCall {
				calls = append(calls, call.Common())
			}
		}
	}
	return calls
}

// irutilAnswers counts the answers a helper gave, so a test can require both.
type irutilAnswers struct {
	yes int
	no  int
}

func (answers *irutilAnswers) add(answer bool) {
	if answer {
		answers.yes++
	} else {
		answers.no++
	}
}

// requireBoth fails test unless the helper answered both ways.
func (answers irutilAnswers) requireBoth(test *testing.T, helper string) {
	test.Helper()
	test.Logf("%s answered yes %d times and no %d times, the same on both sides", helper, answers.yes, answers.no)
	if answers.yes == 0 || answers.no == 0 {
		test.Errorf("%s answered yes %d times and no %d times; the fixture should make it answer both ways",
			helper, answers.yes, answers.no)
	}
}

// irutilEachCall hands each pair of calls in units' functions to compare, after requiring the same number of calls
// on each side.
func irutilEachCall(test *testing.T,
	compare func(function string, theirs *ir.CallCommon, ours *intermediate_representation.CallCommon)) {
	test.Helper()
	for _, unit := range irutilUnits(test) {
		theirFunctions, ourFunctions := irutilFunctions(test, unit)
		for index, theirFunction := range theirFunctions {
			theirCalls := irutilTheirCalls(theirFunction)
			ourCalls := irutilOurCalls(ourFunctions[index])
			if len(theirCalls) != len(ourCalls) {
				test.Fatalf("%s: go/ir has %d calls, ours %d", theirFunction.RelString(nil), len(theirCalls),
					len(ourCalls))
			}
			for callIndex, theirCall := range theirCalls {
				compare(theirFunction.RelString(nil), theirCall, ourCalls[callIndex])
			}
		}
	}
}

// TestCallNameMatchesIrutil names every call on both sides and requires the same name.
func TestCallNameMatchesIrutil(test *testing.T) {
	test.Parallel()
	var named irutilAnswers
	irutilEachCall(test, func(function string, theirs *ir.CallCommon,
		ours *intermediate_representation.CallCommon) {
		theirName := irutil.CallName(theirs)
		ourName := intermediate_representation.CallName(ours)
		if theirName != ourName {
			test.Errorf("%s: irutil names the call %q, ours %q", function, theirName, ourName)
		}
		named.add(theirName != "")
	})
	named.requireBoth(test, "CallName (named)")
}

// irutilProbeNames are the names IsCallTo and IsCallToAny are asked about: the fixture's callees and an empty one.
var irutilProbeNames = []string{"", "len", "strings.Contains", "(*sync.Mutex).Lock", "(*sync.Mutex).Unlock",
	"helpers.generic", "helpers.returnsConstant", "helpers.adds"}

// TestIsCallToMatchesIrutil asks of every call whether it calls each probe name, on both sides.
func TestIsCallToMatchesIrutil(test *testing.T) {
	test.Parallel()
	var called irutilAnswers
	irutilEachCall(test, func(function string, theirs *ir.CallCommon,
		ours *intermediate_representation.CallCommon) {
		for _, name := range irutilProbeNames {
			theirAnswer := irutil.IsCallTo(theirs, name)
			ourAnswer := intermediate_representation.IsCallTo(ours, name)
			if theirAnswer != ourAnswer {
				test.Errorf("%s: irutil.IsCallTo(%q) is %t, ours %t", function, name, theirAnswer, ourAnswer)
			}
			called.add(theirAnswer)
		}
	})
	called.requireBoth(test, "IsCallTo")
}

// TestIsCallToAnyMatchesIrutil asks of every call whether it calls any of each window of probe names, on both
// sides.
func TestIsCallToAnyMatchesIrutil(test *testing.T) {
	test.Parallel()
	var called irutilAnswers
	irutilEachCall(test, func(function string, theirs *ir.CallCommon,
		ours *intermediate_representation.CallCommon) {
		for start := range irutilProbeNames {
			names := irutilProbeNames[start:min(start+3, len(irutilProbeNames))]
			theirAnswer := irutil.IsCallToAny(theirs, names...)
			ourAnswer := intermediate_representation.IsCallToAny(ours, names...)
			if theirAnswer != ourAnswer {
				test.Errorf("%s: irutil.IsCallToAny(%q) is %t, ours %t", function, names, theirAnswer, ourAnswer)
			}
			called.add(theirAnswer)
		}
	})
	called.requireBoth(test, "IsCallToAny")
}

// TestIsTrivialMatchesIrutil asks of every source function whether it is trivial, on both sides.
func TestIsTrivialMatchesIrutil(test *testing.T) {
	test.Parallel()
	var trivial irutilAnswers
	for _, unit := range irutilUnits(test) {
		theirFunctions, ourFunctions := irutilFunctions(test, unit)
		for index, theirFunction := range theirFunctions {
			theirAnswer := irutil.IsTrivial(theirFunction)
			ourAnswer := intermediate_representation.IsTrivial(ourFunctions[index])
			if theirAnswer != ourAnswer {
				test.Errorf("%s: irutil.IsTrivial is %t, ours %t", theirFunction.RelString(nil), theirAnswer,
					ourAnswer)
			}
			trivial.add(theirAnswer)
		}
	}
	trivial.requireBoth(test, "IsTrivial")
}

// TestIsExampleMatchesIrutil asks of every source function whether it is an example, on both sides.
func TestIsExampleMatchesIrutil(test *testing.T) {
	test.Parallel()
	var example irutilAnswers
	for _, unit := range irutilUnits(test) {
		theirFunctions, ourFunctions := irutilFunctions(test, unit)
		for index, theirFunction := range theirFunctions {
			theirAnswer := irutil.IsExample(theirFunction)
			ourAnswer := intermediate_representation.IsExample(ourFunctions[index])
			if theirAnswer != ourAnswer {
				test.Errorf("%s: irutil.IsExample is %t, ours %t", theirFunction.RelString(nil), theirAnswer,
					ourAnswer)
			}
			example.add(theirAnswer)
		}
	}
	example.requireBoth(test, "IsExample")
}

// TestReachableMatchesIrutil asks of every ordered pair of blocks in every source function whether the second is
// reachable from the first, on both sides.
func TestReachableMatchesIrutil(test *testing.T) {
	test.Parallel()
	var reachable irutilAnswers
	for _, unit := range irutilUnits(test) {
		theirFunctions, ourFunctions := irutilFunctions(test, unit)
		for index, theirFunction := range theirFunctions {
			ourFunction := ourFunctions[index]
			for fromIndex, theirFrom := range theirFunction.Blocks {
				for toIndex, theirTo := range theirFunction.Blocks {
					theirAnswer := irutil.Reachable(theirFrom, theirTo)
					ourAnswer := intermediate_representation.Reachable(ourFunction.Blocks[fromIndex],
						ourFunction.Blocks[toIndex])
					if theirAnswer != ourAnswer {
						test.Errorf("%s: irutil.Reachable(%d, %d) is %t, ours %t", theirFunction.RelString(nil),
							fromIndex, toIndex, theirAnswer, ourAnswer)
					}
					reachable.add(theirAnswer)
				}
			}
		}
	}
	reachable.requireBoth(test, "Reachable")
}

// TestWalkMatchesIrutil walks from every block of every source function on both sides, once visiting on and once
// stopping below each odd-numbered block, and requires the same blocks visited in the same order.
func TestWalkMatchesIrutil(test *testing.T) {
	test.Parallel()
	var whole irutilAnswers
	for _, unit := range irutilUnits(test) {
		theirFunctions, ourFunctions := irutilFunctions(test, unit)
		for index, theirFunction := range theirFunctions {
			ourFunction := ourFunctions[index]
			for blockIndex, theirBlock := range theirFunction.Blocks {
				for _, stopsBelowOdd := range []bool{false, true} {
					var theirOrder, ourOrder []int
					irutil.Walk(theirBlock, func(visited *ir.BasicBlock) bool {
						theirOrder = append(theirOrder, visited.Index)
						return !stopsBelowOdd || visited.Index%2 == 0
					})
					intermediate_representation.Walk(ourFunction.Blocks[blockIndex],
						func(visited *intermediate_representation.BasicBlock) bool {
							ourOrder = append(ourOrder, visited.Index)
							return !stopsBelowOdd || visited.Index%2 == 0
						})
					if !slices.Equal(theirOrder, ourOrder) {
						test.Errorf("%s: irutil.Walk from %d (stopping below odd blocks: %t) visits %v, ours %v",
							theirFunction.RelString(nil), blockIndex, stopsBelowOdd, theirOrder, ourOrder)
					}
					whole.add(len(theirOrder) == len(theirFunction.Blocks))
				}
			}
		}
	}
	whole.requireBoth(test, "Walk (visited every block)")
}

// TestIsTrivialBuildsAnUnreadCallee builds our IR lazily, as the analyzer's SourceFunctions does: only the caller,
// whose callee is declared after it, does real work, and hasn't been read. IsTrivial must build the callee before
// reading its blocks, since an unbuilt callee has none and would read as trivial, and answer false as irutil does
// over go/ir, which buildir builds whole.
func TestIsTrivialBuildsAnUnreadCallee(test *testing.T) {
	test.Parallel()
	unit := differentialCheck(test, "lazy", `package lazy

func caller() int { return callee(1, 2) }

func callee(left, right int) int { return left + right }
`)
	theirProgram := ir.NewProgram(unit.fileSet, 0)
	theirPackage := theirProgram.CreatePackage(unit.types, unit.files, unit.typeInfo, false)
	theirPackage.Build()
	if irutil.IsTrivial(theirPackage.Func("caller")) {
		test.Fatal("irutil reads caller as trivial, so the case doesn't hold a callee that does work")
	}
	ourProgram := intermediate_representation.NewProgram(unit.fileSet)
	ourPackage := ourProgram.CreatePackage(unit.types, unit.files, unit.typeInfo, false)
	caller := ourPackage.Function("caller")
	callee := ourPackage.Function("callee")
	caller.Build()
	if callee.Blocks != nil {
		test.Fatal("building caller built callee too, so the case holds no unbuilt callee")
	}
	if intermediate_representation.IsTrivial(caller) {
		test.Error("IsTrivial reads caller as trivial over a callee it didn't build, where irutil reads it as not")
	}
	if callee.Blocks == nil {
		test.Error("IsTrivial left callee unbuilt")
	}
}
