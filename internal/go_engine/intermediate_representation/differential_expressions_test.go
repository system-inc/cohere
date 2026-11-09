package intermediate_representation_test

// The expression map's differential (#ee33njz): go/ir built exactly as buildir builds it, lifted with GlobalDebug,
// against ours lifted with SetDebugMode on every package. Every function's non-phi instructions are compared in
// lockstep as the lifted differential compares them, then every expression in its syntax: go/ir's ValueForExpr and
// our ValueForExpression must both be absent, or name corresponding values, by the lifted differential's naming,
// with the same isAddress.
//
// TestExpressionMapCost measures what the map costs: ours and go/ir's builds of a corpus with it and without it.

import (
	"fmt"
	"go/ast"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"golang.org/x/tools/go/packages"
	"honnef.co/go/tools/go/ir"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// differentialExpressionTally adds the expressions compared to a differential tally.
type differentialExpressionTally struct {
	differentialTally

	// expressions are the expressions compared, present those among them both maps hold.
	expressions int
	present     int
}

func differentialMergeExpressions(tally differentialExpressionTally,
	other differentialExpressionTally) differentialExpressionTally {
	return differentialExpressionTally{
		differentialTally: differentialMerge(tally.differentialTally, other.differentialTally),
		expressions:       tally.expressions + other.expressions,
		present:           tally.present + other.present,
	}
}

// TestExpressionDifferential compares the ordinary corpus with the expression map on, or the one
// COHERE_GO_IR_DIFFERENTIAL names, and fails on any difference.
func TestExpressionDifferential(test *testing.T) {
	test.Parallel()
	corpus := os.Getenv(differentialCorpusVariable)
	started := time.Now()
	var tally differentialExpressionTally
	for _, unit := range differentialCorpusUnits(test, corpus) {
		tally = differentialMergeExpressions(tally, differentialCompareExpressionUnit(test, unit, false))
	}
	test.Logf("expressions: %d compared, %d in both maps", tally.expressions, tally.present)
	differentialReport(test, "expressions "+differentialCorpusName(corpus), tally.differentialTally,
		time.Since(started))
}

// TestExpressionDifferentialFailsOnADroppedReference plants a mutant through the test seam: each of our
// functions leaves its first debug reference out of its map. The comparator finds the fixed function.
func TestExpressionDifferentialFailsOnADroppedReference(test *testing.T) {
	test.Parallel()
	unit := differentialCheck(test, "mutant", `package mutant

func Sum(first int, second int) int {
	total := first + second
	return total
}
`)
	tally := differentialCompareExpressionUnit(test, unit, true)
	if len(tally.differences) == 0 {
		test.Fatal("the comparator found nothing in the mutant")
	}
	for _, difference := range tally.differences {
		test.Logf("the comparator reports the mutant: %s", differentialDescribe(difference))
		if difference.function != "mutant.Sum" || difference.cause != "expression map" {
			test.Errorf("the comparator found %q in %s, want the dropped reference in mutant.Sum", difference.cause,
				difference.function)
		}
	}
}

// TestExpressionMapCost builds the corpus four ways, one after another, and logs each build's time, the bytes it
// allocated and the bytes its packages hold once built: ours with the map off and on, go/ir without GlobalDebug
// and with it. Run it alone for numbers, since a test running beside it shares the heap it measures.
func TestExpressionMapCost(test *testing.T) {
	test.Parallel()
	corpus := os.Getenv(differentialCorpusVariable)
	units := differentialCorpusUnits(test, corpus)
	// One build first, unmeasured, so the method sets and type caches go/types fills on first use are no
	// build's cost.
	for _, unit := range units {
		differentialBuildTheirs(unit, 0)
	}
	for _, build := range []struct {
		name  string
		build func(unit differentialUnit) differentialBuilt
	}{
		{"ours, map off", func(unit differentialUnit) differentialBuilt {
			return differentialBuilt{ours: differentialBuildOurs(unit, false)}
		}},
		{"ours, map on", func(unit differentialUnit) differentialBuilt {
			return differentialBuilt{ours: differentialBuildOurs(unit, true)}
		}},
		{"go/ir, lifted", func(unit differentialUnit) differentialBuilt {
			return differentialBuilt{theirs: differentialBuildTheirs(unit, 0)}
		}},
		{"go/ir, GlobalDebug", func(unit differentialUnit) differentialBuilt {
			return differentialBuilt{theirs: differentialBuildTheirs(unit, ir.GlobalDebug)}
		}},
	} {
		runtime.GC()
		var before runtime.MemStats
		runtime.ReadMemStats(&before)
		started := time.Now()
		built := make([]differentialBuilt, 0, len(units))
		for _, unit := range units {
			built = append(built, build.build(unit))
		}
		elapsed := time.Since(started)
		runtime.GC()
		var after runtime.MemStats
		runtime.ReadMemStats(&after)
		runtime.KeepAlive(built)
		test.Logf("cost, %s, corpus %s: %d packages built in %s, %.1f MiB allocated, %.1f MiB held after", build.name,
			differentialCorpusName(corpus), len(units), elapsed.Round(time.Millisecond),
			float64(after.TotalAlloc-before.TotalAlloc)/(1<<20),
			(float64(after.HeapAlloc)-float64(before.HeapAlloc))/(1<<20))
	}
}

// differentialBuilt holds one unit's build, ours or go/ir's, alive while the cost test measures it.
type differentialBuilt struct {
	ours   *intermediate_representation.Package
	theirs *ir.Package
}

// differentialCorpusUnits are the units corpus names, kept whole: the ordinary corpus's testdata and standard
// packages, or every package root, std or a pattern loads without errors.
func differentialCorpusUnits(test *testing.T, corpus string) []differentialUnit {
	test.Helper()
	patterns := differentialStandardPackages
	var units []differentialUnit
	switch corpus {
	case "":
		units = differentialTestdataUnits(test)
	case "root":
		patterns = []string{"./..."}
	case "std":
		patterns = []string{"std"}
	default:
		patterns = []string{corpus}
	}
	root, rootError := filepath.Abs(filepath.Join("..", "..", ".."))
	if rootError != nil {
		test.Fatal(rootError)
	}
	configuration := &packages.Config{
		Dir: root,
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports,
	}
	loaded, loadError := packages.Load(configuration, patterns...)
	if loadError != nil {
		test.Fatal(loadError)
	}
	for _, each := range loaded {
		if len(each.Errors) > 0 || each.Types == nil || each.TypesInfo == nil {
			continue
		}
		units = append(units, differentialUnit{path: each.PkgPath, fileSet: each.Fset, types: each.Types,
			files: each.Syntax, typeInfo: each.TypesInfo})
	}
	return units
}

// differentialBuildTheirs builds unit with go/ir in mode, buildir's way.
func differentialBuildTheirs(unit differentialUnit, mode ir.BuilderMode) *ir.Package {
	program := ir.NewProgram(unit.fileSet, mode)
	program.SetNoReturn(differentialNoReturn)
	for _, imported := range unit.types.Imports() {
		program.CreatePackage(imported, nil, nil, true)
	}
	built := program.CreatePackage(unit.types, unit.files, unit.typeInfo, false)
	built.Build()
	return built
}

// differentialBuildOurs builds unit with ours, every package's expression map on when debug is set, as go/ir's
// GlobalDebug sets it on every package it creates.
func differentialBuildOurs(unit differentialUnit, debug bool) *intermediate_representation.Package {
	program := intermediate_representation.NewProgram(unit.fileSet)
	program.SetNoReturn(differentialNoReturn)
	return differentialBuildOursIn(program, unit, debug)
}

func differentialBuildOursIn(program *intermediate_representation.Program, unit differentialUnit,
	debug bool) *intermediate_representation.Package {
	for _, imported := range unit.types.Imports() {
		program.CreatePackage(imported, nil, nil, true).SetDebugMode(debug)
	}
	built := program.CreatePackage(unit.types, unit.files, unit.typeInfo, false)
	built.SetDebugMode(debug)
	built.Build()
	return built
}

// differentialCompareExpressionUnit builds unit both ways with the map on and compares every source function's
// instructions and expressions. mutant plants the dropped-reference mutant in ours.
func differentialCompareExpressionUnit(test *testing.T, unit differentialUnit,
	mutant bool) (tally differentialExpressionTally) {
	tally.packages = 1
	var theirPackage *ir.Package
	theirPanic := differentialBuild(func() { theirPackage = differentialBuildTheirs(unit, ir.GlobalDebug) })
	var ourPackage *intermediate_representation.Package
	ourPanic := differentialBuild(func() {
		program := intermediate_representation.NewProgram(unit.fileSet)
		if mutant {
			intermediate_representation.DropDebugReferenceForTest(test, program)
		}
		program.SetNoReturn(differentialNoReturn)
		ourPackage = differentialBuildOursIn(program, unit, true)
	})
	if theirPanic != "" || ourPanic != "" {
		tally.differences = append(tally.differences, differentialDifference{cause: "a build with the map panicked",
			function: unit.path, where: "building the package", theirs: theirPanic, ours: ourPanic})
		return tally
	}
	theirFunctions := differentialTheirSourceFunctions(theirPackage)
	ourFunctions := differentialOurSourceFunctions(ourPackage)
	if len(theirFunctions) != len(ourFunctions) {
		tally.differences = append(tally.differences, differentialDifference{cause: "source function count",
			function: unit.path, where: "the package's source functions",
			theirs: fmt.Sprint(len(theirFunctions)), ours: fmt.Sprint(len(ourFunctions))})
		return tally
	}
	for index, theirs := range theirFunctions {
		ours := ourFunctions[index]
		tally.functions++
		differentialClearDebugNodes(theirs)
		if difference, differs := differentialCompareLiftedFunction(theirs, ours); differs {
			tally.differences = append(tally.differences, difference)
			continue
		}
		expressions, present, difference, differs := differentialCompareExpressions(unit, theirs, ours)
		tally.expressions += expressions
		tally.present += present
		if differs {
			tally.differences = append(tally.differences, difference)
			continue
		}
		tally.identical++
		for _, block := range theirs.Blocks {
			tally.instructions += len(block.Instrs) - len(block.Phis())
		}
	}
	return tally
}

// differentialClearDebugNodes clears go/ir's SelectState.DebugNode in function. It is the receive go/ir keeps on
// the state for the debug references it emits after a select; ours keeps it in a slice local to the builder, so
// the state has no such field, and the references both emit from it are compared through the maps.
func differentialClearDebugNodes(function *ir.Function) {
	for _, block := range function.Blocks {
		for _, instruction := range block.Instrs {
			if selection, isSelect := instruction.(*ir.Select); isSelect {
				for _, state := range selection.States {
					state.DebugNode = nil
				}
			}
		}
	}
}

// differentialCompareExpressions compares the two maps on every expression in theirs's syntax, returning how
// many it compared, how many both maps hold, and the first difference.
func differentialCompareExpressions(unit differentialUnit, theirs *ir.Function,
	ours *intermediate_representation.Function) (int, int, differentialDifference, bool) {
	syntax := theirs.Syntax()
	if syntax == nil {
		return 0, 0, differentialDifference{}, false
	}
	var expressions []ast.Expr
	ast.Inspect(syntax, func(node ast.Node) bool {
		if expression, isExpression := node.(ast.Expr); isExpression {
			expressions = append(expressions, expression)
		}
		return true
	})
	var theirValues []ir.Value
	var ourValues []intermediate_representation.Value
	for _, expression := range expressions {
		if value, _ := theirs.ValueForExpr(expression); value != nil {
			theirValues = append(theirValues, value)
		}
		if value, _ := ours.ValueForExpression(expression); value != nil {
			ourValues = append(ourValues, value)
		}
	}
	theirPlaces := differentialLiftedTheirPlaces(theirs, theirValues...)
	ourPlaces := differentialLiftedOurPlaces(ours, ourValues...)
	present := 0
	for _, expression := range expressions {
		theirValue, theirIsAddress := theirs.ValueForExpr(expression)
		ourValue, ourIsAddress := ours.ValueForExpression(expression)
		theirRendering := "absent"
		if theirValue != nil {
			theirRendering = fmt.Sprintf("%s, address %t", theirPlaces[theirValue], theirIsAddress)
		}
		ourRendering := "absent"
		if ourValue != nil {
			ourRendering = fmt.Sprintf("%s, address %t", ourPlaces[ourValue], ourIsAddress)
		}
		if theirRendering != ourRendering {
			where := fmt.Sprintf("%T at %s", expression, unit.fileSet.Position(expression.Pos()))
			return len(expressions), present, differentialDifference{cause: "expression map",
				function: theirs.RelString(nil), where: where, theirs: theirRendering, ours: ourRendering}, true
		}
		if theirValue != nil {
			present++
		}
	}
	return len(expressions), present, differentialDifference{}, false
}

// TestValueForExpressionIsEmptyWithTheModeOff builds a package without SetDebugMode, as the engine builds a
// dependency, and holds its map empty: every expression in its syntax answers nil. The same package built with the
// mode answers some, so the check can fail.
func TestValueForExpressionIsEmptyWithTheModeOff(test *testing.T) {
	test.Parallel()
	unit := differentialCheck(test, "form", liftingForms["if"])
	if answered := differentialAnswered(differentialBuildOurs(unit, false).Function("f")); answered > 0 {
		test.Errorf("%d expressions answered with the mode off, want none", answered)
	}
	if answered := differentialAnswered(differentialBuildOurs(unit, true).Function("f")); answered == 0 {
		test.Error("no expression answered with the mode on, so the check with it off proves nothing")
	}
}

// differentialAnswered counts the expressions in function's syntax its map answers.
func differentialAnswered(function *intermediate_representation.Function) int {
	answered := 0
	ast.Inspect(function.Syntax(), func(node ast.Node) bool {
		if expression, isExpression := node.(ast.Expr); isExpression {
			if value, _ := function.ValueForExpression(expression); value != nil {
				answered++
			}
		}
		return true
	})
	return answered
}
