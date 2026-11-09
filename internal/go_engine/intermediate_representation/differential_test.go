package intermediate_representation_test

// The differential against go/ir (#6pt3bt8). Our IR is go/ir's builder, copied and renamed, built unlifted, so it
// should lower every function exactly as go/ir does in its NaiveForm mode: the same blocks and edges, the same
// instructions with the same operands, types, positions and fields. This builds each package both ways from one
// load and walks the two IRs in lockstep, function by function, and reports the first difference in each function.
//
// go/ir is built as buildir builds it, but for two things. Its mode is NaiveForm alone, without GlobalDebug, since
// ours carries no debug references (go-ir-shape.md, section 1: "DebugRef ... not built"). And both builds share
// one no-return predicate, differentialNoReturn, in place of buildir's ctrlflow facts, which need an analysis
// pass; the predicate only has to be the same on both sides for the lowering after a call to be compared.
//
// The ordinary run compares the package's own testdata, a fixture holding every control-flow form, and a few
// standard library packages. COHERE_GO_IR_DIFFERENTIAL widens it: root is every package of the cohere module, std
// the standard library, and anything else a go/packages pattern.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"reflect"
	"runtime/debug"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"golang.org/x/tools/go/packages"
	"honnef.co/go/tools/go/ir"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// differentialCorpusVariable names the corpus a wide run compares.
const differentialCorpusVariable = "COHERE_GO_IR_DIFFERENTIAL"

// differentialStandardPackages are the standard library packages the ordinary run compares.
var differentialStandardPackages = []string{"strings", "sort", "sync", "go/ast"}

// differentialKinds maps go/ir's instruction and value type names to ours, by the rename table (#7zbpd7w,
// go-ir-rename-table.md, section 1). A kind not listed keeps its name.
var differentialKinds = map[string]string{
	"Alloc":      "Allocation",
	"BinOp":      "BinaryOperation",
	"UnOp":       "UnaryOperation",
	"FieldAddr":  "FieldAddress",
	"IndexAddr":  "IndexAddress",
	"MakeChan":   "MakeChannel",
	"Recv":       "Receive",
	"Const":      "Constant",
	"FreeVar":    "FreeVariable",
	"NamedConst": "NamedConstant",
}

// differentialFields maps go/ir's exported instruction fields to ours, by kind and field, from the rename table's
// section 2 and the names spelled out after it (#6pt3bt8, wave 2a). A field not listed keeps its name.
var differentialFields = map[string]map[string]string{
	"BinOp":          {"Op": "Operator"},
	"UnOp":           {"Op": "Operator"},
	"If":             {"Cond": "Condition"},
	"TypeSwitch":     {"Conds": "Conditions"},
	"ConstantSwitch": {"Conds": "Conditions"},
	"CallCommon":     {"Args": "Arguments"},
	"MakeClosure":    {"Fn": "Function"},
	"Slice":          {"Max": "Maximum"},
	"Store":          {"Addr": "Address", "Val": "Value"},
	"BlankStore":     {"Val": "Value"},
	"CompositeValue": {"NumSet": "ExplicitCount"},
	"Recv":           {"Chan": "Channel"},
	"Send":           {"Chan": "Channel"},
	"SelectState":    {"Chan": "Channel", "Dir": "Direction", "Pos": "Position"},
	"MakeSlice":      {"Len": "Length", "Cap": "Capacity"},
	"Next":           {"Iter": "Iterator"},
}

// differentialNoReturn is the no-return predicate both builds share: a call to one of these ends its block in a
// Panic. buildir asks ctrlflow's facts instead; any predicate does, so long as both sides get the same one.
func differentialNoReturn(function *types.Func) bool {
	switch function.FullName() {
	case "os.Exit", "runtime.Goexit", "log.Fatal", "log.Fatalf", "log.Fatalln", "log.Panic", "log.Panicf",
		"log.Panicln":
		return true
	}
	return false
}

// differentialUnit is one package to build both ways: its syntax and types, from go/packages or checked in memory.
type differentialUnit struct {
	path     string
	fileSet  *token.FileSet
	types    *types.Package
	files    []*ast.File
	typeInfo *types.Info
}

// differentialDifference is the first place one function's two IRs part.
type differentialDifference struct {
	// cause groups differences that part the same way, as "operand of BinaryOperation".
	cause    string
	function string
	where    string
	theirs   string
	ours     string
}

// differentialDescribe renders a difference for the log: the function, where in it, and both sides.
func differentialDescribe(difference differentialDifference) string {
	return fmt.Sprintf("%s: %s\n\tgo/ir: %s\n\tours:  %s", difference.function, difference.where,
		difference.theirs, difference.ours)
}

// differentialTally is what comparing a set of units found.
type differentialTally struct {
	packages  int
	skipped   []string
	functions int
	identical int

	// instructions are the instructions in the identical functions, each compared field by field.
	instructions int
	differences  []differentialDifference

	// aliasSpelledInstances are the instantiation wrappers go/ir named through an alias, compared unaliased.
	aliasSpelledInstances int
}

// differentialMerge is what tally and other found together.
func differentialMerge(tally differentialTally, other differentialTally) differentialTally {
	return differentialTally{
		packages:     tally.packages + other.packages,
		skipped:      append(slices.Clone(tally.skipped), other.skipped...),
		functions:    tally.functions + other.functions,
		identical:    tally.identical + other.identical,
		instructions: tally.instructions + other.instructions,
		differences:  append(slices.Clone(tally.differences), other.differences...),

		aliasSpelledInstances: tally.aliasSpelledInstances + other.aliasSpelledInstances,
	}
}

// differentialOurMutation, when set, changes our build of a package before it is compared. The mutant test sets
// it to show the comparator fails on a lowering that differs.
type differentialOurMutation func(*intermediate_representation.Package)

// TestDifferential compares the ordinary corpus, or the one COHERE_GO_IR_DIFFERENTIAL names, and fails on any
// difference, listing them grouped by cause.
func TestDifferential(test *testing.T) {
	test.Parallel()
	corpus := os.Getenv(differentialCorpusVariable)
	started := time.Now()
	var tally differentialTally
	if corpus == "" {
		for _, unit := range differentialTestdataUnits(test) {
			tally = differentialMerge(tally, differentialCompareUnit(test, unit, nil))
		}
		tally = differentialMerge(tally, differentialCompareLoaded(test, differentialStandardPackages))
	} else {
		pattern := corpus
		switch corpus {
		case "root":
			pattern = "./..."
		case "std":
			pattern = "std"
		}
		tally = differentialMerge(tally, differentialCompareLoaded(test, []string{pattern}))
	}
	differentialReport(test, corpus, tally, time.Since(started))
}

// TestDifferentialFailsOnASwappedOperand plants a mutant in our build of a fixed function, its subtraction's
// operands swapped, and holds the comparator to finding it there and nowhere else.
func TestDifferentialFailsOnASwappedOperand(test *testing.T) {
	test.Parallel()
	unit := differentialCheck(test, "mutant", `package mutant

func Difference(minuend, subtrahend int) int { return minuend - subtrahend }

func Sum(left, right int) int { return left + right }
`)
	swapped := 0
	tally := differentialCompareUnit(test, unit, func(built *intermediate_representation.Package) {
		for _, block := range built.Function("Difference").Blocks {
			for _, instruction := range block.Instructions {
				if operation, isOperation := instruction.(*intermediate_representation.BinaryOperation); isOperation {
					operation.X, operation.Y = operation.Y, operation.X
					swapped++
				}
			}
		}
	})
	if swapped != 1 {
		test.Fatalf("the mutant swapped %d subtractions, want 1", swapped)
	}
	if len(tally.differences) != 1 {
		test.Fatalf("the comparator found %d differences in the mutant, want 1: %v", len(tally.differences),
			tally.differences)
	}
	found := tally.differences[0]
	test.Logf("the comparator reports the mutant: %s", differentialDescribe(found))
	if found.function != "mutant.Difference" || found.cause != "operand of BinaryOperation" {
		test.Errorf("the comparator found %q in %s, want the swapped operand in mutant.Difference:\n%s", found.cause,
			found.function, differentialDescribe(found))
	}
}

// differentialEveryForm is a fixture holding each control-flow form the lowering handles, so the ordinary run
// compares them all even where the standard library packages it reads don't.
const differentialEveryForm = `package everyform

import (
	"iter"
	"os"
)

type shape interface{ area() int }

type square struct{ side int }

func (s square) area() int { return s.side * s.side }

type pair[T any] struct{ first, second T }

func (p pair[T]) swap() pair[T] { return pair[T]{p.second, p.first} }

func first[T any](values []T) (result T, found bool) {
	for _, value := range values {
		return value, true
	}
	return result, false
}

func sequence(limit int) iter.Seq[int] {
	return func(yield func(int) bool) {
		for index := range limit {
			if !yield(index) {
				return
			}
		}
	}
}

func forms(
	values []int,
	table map[string]int,
	text string,
	channel chan int,
	done chan struct{},
	any interface{},
) (total int, failure error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			total = -1
		}
	}()
	for index := 0; index < len(values); index++ {
		if values[index] < 0 {
			continue
		}
		total += values[index]
	}
	for index, value := range values {
		total += index * value
	}
	for key, value := range table {
		total += len(key) + value
	}
	for offset, character := range text {
		total += offset + int(character)
	}
	for value := range channel {
		total += value
		break
	}
	for index := range 3 {
		total += index
	}
	for value := range sequence(4) {
		if value == 2 {
			break
		}
		total += value
	}
	var array [3]int
	for index := range array {
		array[index] = index
	}
	switch {
	case total > 10:
		total--
		fallthrough
	case total > 5:
		total++
	default:
		total = 0
	}
	switch value := any.(type) {
	case int:
		total += value
	case string, []byte:
		total++
	case nil:
	}
	select {
	case value := <-channel:
		total += value
	case channel <- total:
	case <-done:
		return 0, nil
	default:
	}
outer:
	for {
		for {
			if total > 100 {
				break outer
			}
			total += 50
			continue outer
		}
	}
	if total < 0 {
		goto finish
	}
	total *= 2
finish:
	counter := 0
	increment := func() { counter++ }
	increment()
	area := square{side: 2}.area
	total += area() + counter
	var described shape = square{side: 3}
	total += described.area()
	method := square.area
	total += method(square{side: 1})
	swapped := pair[int]{1, 2}.swap()
	total += swapped.first
	if found, ok := first(values); ok {
		total += found
	}
	slice := values[1:2:3]
	total += len(slice) + cap(slice)
	pointer := &array
	pointer[0] = len(values)
	converted := []byte(text)
	total += len(string(converted))
	go func() { done <- struct{}{} }()
	defer close(done)
	if total == 7 {
		os.Exit(1)
	}
	if total == 8 {
		panic("eight")
	}
	return total, nil
}
`

// differentialTestdataUnits are the units the ordinary run checks in memory: each program in testdata/fixedbugs
// and the every-form fixture.
func differentialTestdataUnits(test *testing.T) []differentialUnit {
	test.Helper()
	paths, globError := filepath.Glob(filepath.Join("testdata", "fixedbugs", "*.go"))
	if globError != nil {
		test.Fatal(globError)
	}
	var units []differentialUnit
	for _, path := range paths {
		source, readError := os.ReadFile(path)
		if readError != nil {
			test.Fatal(readError)
		}
		units = append(units, differentialCheck(test, strings.TrimSuffix(filepath.Base(path), ".go"), string(source)))
	}
	return append(units, differentialCheck(test, "everyform", differentialEveryForm))
}

// differentialCheck parses and type-checks source as the package at path, its imports from the toolchain's export
// data.
func differentialCheck(test *testing.T, path string, source string) differentialUnit {
	test.Helper()
	fileSet := token.NewFileSet()
	file, parseError := parser.ParseFile(fileSet, path+".go", source, parser.ParseComments)
	if parseError != nil {
		test.Fatal(parseError)
	}
	typeInfo := differentialTypeInfo()
	configuration := &types.Config{Importer: differentialImporter(test, source)}
	checked, checkError := configuration.Check(path, fileSet, []*ast.File{file}, typeInfo)
	if checkError != nil {
		test.Fatal(checkError)
	}
	return differentialUnit{path: path, fileSet: fileSet, types: checked, files: []*ast.File{file},
		typeInfo: typeInfo}
}

func differentialTypeInfo() *types.Info {
	return &types.Info{
		Types:        map[ast.Expr]types.TypeAndValue{},
		Defs:         map[*ast.Ident]types.Object{},
		Uses:         map[*ast.Ident]types.Object{},
		Implicits:    map[ast.Node]types.Object{},
		Scopes:       map[ast.Node]*types.Scope{},
		Selections:   map[*ast.SelectorExpr]*types.Selection{},
		Instances:    map[*ast.Ident]types.Instance{},
		FileVersions: map[*ast.File]string{},
	}
}

// differentialCompareLoaded loads patterns from the module root with full syntax and types and compares each
// package that loaded without errors, one at a time, so no more than one package's two IRs are live at once.
func differentialCompareLoaded(test *testing.T, patterns []string) differentialTally {
	test.Helper()
	// This package sits three directories below the module root.
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
	var tally differentialTally
	for _, each := range loaded {
		if len(each.Errors) > 0 || each.Types == nil || each.TypesInfo == nil {
			tally.skipped = append(tally.skipped, each.PkgPath)
			continue
		}
		unit := differentialUnit{path: each.PkgPath, fileSet: each.Fset, types: each.Types, files: each.Syntax,
			typeInfo: each.TypesInfo}
		tally = differentialMerge(tally, differentialCompareUnit(test, unit, nil))
		// Let the package's syntax and types go once both IRs are compared.
		each.Syntax, each.TypesInfo = nil, nil
	}
	return tally
}

// differentialCompareUnit builds unit both ways and compares every source function, in buildir's SrcFuncs order:
// the package's functions, then each one's anonymous functions, recursively. A build that panics is a difference
// of its own.
func differentialCompareUnit(test *testing.T, unit differentialUnit,
	mutation differentialOurMutation) (tally differentialTally) {
	tally.packages = 1
	var theirPackage *ir.Package
	theirPanic := differentialBuild(func() {
		theirProgram := ir.NewProgram(unit.fileSet, ir.NaiveForm)
		theirProgram.SetNoReturn(differentialNoReturn)
		for _, imported := range unit.types.Imports() {
			theirProgram.CreatePackage(imported, nil, nil, true)
		}
		theirPackage = theirProgram.CreatePackage(unit.types, unit.files, unit.typeInfo, false)
		theirPackage.Build()
	})
	var ourPackage *intermediate_representation.Package
	ourPanic := differentialBuild(func() {
		ourProgram := intermediate_representation.NewProgram(unit.fileSet)
		// Unlifted, since go/ir is built in NaiveForm here; the lifted comparison is TestLiftedDifferential.
		intermediate_representation.BuildUnliftedForTest(test, ourProgram)
		ourProgram.SetNoReturn(differentialNoReturn)
		for _, imported := range unit.types.Imports() {
			ourProgram.CreatePackage(imported, nil, nil, true)
		}
		ourPackage = ourProgram.CreatePackage(unit.types, unit.files, unit.typeInfo, false)
		ourPackage.Build()
		if mutation != nil {
			mutation(ourPackage)
		}
	})
	if theirPanic != "" || ourPanic != "" {
		cause := "both builds panicked"
		switch {
		case theirPanic == "":
			cause = "our build panicked"
		case ourPanic == "":
			cause = "go/ir's build panicked"
		}
		tally.differences = append(tally.differences, differentialDifference{cause: cause, function: unit.path,
			where: "building the package", theirs: theirPanic, ours: ourPanic})
		return tally
	}

	theirFunctions := differentialTheirSourceFunctions(theirPackage)
	tally.aliasSpelledInstances = differentialAliasSpelledInstances(theirFunctions)
	ourFunctions := differentialOurSourceFunctions(ourPackage)
	if len(theirFunctions) != len(ourFunctions) {
		tally.differences = append(tally.differences, differentialDifference{cause: "source function count",
			function: unit.path, where: "the package's source functions",
			theirs: fmt.Sprint(len(theirFunctions)), ours: fmt.Sprint(len(ourFunctions))})
		return tally
	}
	for index, theirs := range theirFunctions {
		tally.functions++
		difference, differs := differentialCompareFunction(theirs, ourFunctions[index])
		if differs {
			tally.differences = append(tally.differences, difference)
			continue
		}
		tally.identical++
		for _, block := range theirs.Blocks {
			tally.instructions += len(block.Instrs)
		}
	}
	return tally
}

// differentialBuild runs build and returns what it panicked with and where, or the empty string when it didn't.
func differentialBuild(build func()) (panicked string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			panicked = fmt.Sprintf("%v at %s", recovered, differentialPanicSite(string(debug.Stack())))
		}
	}()
	build()
	return ""
}

// differentialPanicSite is the innermost frame of stack in one of the two IR packages, the builder line that
// panicked.
func differentialPanicSite(stack string) string {
	lines := strings.Split(stack, "\n")
	for index, line := range lines {
		isBuilder := strings.HasPrefix(line, "honnef.co/go/tools/go/ir.") ||
			strings.HasPrefix(line, "github.com/system-inc/cohere/internal/go_engine/intermediate_representation.")
		if isBuilder && index+1 < len(lines) {
			return strings.TrimSpace(line) + " " + strings.TrimSpace(lines[index+1])
		}
	}
	return "an unknown frame"
}

func differentialTheirSourceFunctions(built *ir.Package) []*ir.Function {
	functions := slices.Clone(built.Functions)
	var addAnonymous func(function *ir.Function)
	addAnonymous = func(function *ir.Function) {
		for _, anonymous := range function.AnonFuncs {
			functions = append(functions, anonymous)
			addAnonymous(anonymous)
		}
	}
	for _, function := range built.Functions {
		addAnonymous(function)
	}
	return functions
}

func differentialOurSourceFunctions(
	built *intermediate_representation.Package,
) []*intermediate_representation.Function {
	functions := slices.Clone(built.Functions)
	var addAnonymous func(function *intermediate_representation.Function)
	addAnonymous = func(function *intermediate_representation.Function) {
		for _, anonymous := range function.AnonymousFunctions {
			functions = append(functions, anonymous)
			addAnonymous(anonymous)
		}
	}
	for _, function := range built.Functions {
		addAnonymous(function)
	}
	return functions
}

// differentialCompareFunction walks one function's two IRs in lockstep and returns the first place they part.
func differentialCompareFunction(
	theirs *ir.Function,
	ours *intermediate_representation.Function,
) (differentialDifference, bool) {
	name := theirs.RelString(nil)
	part := func(cause string, where string, theirRendering string,
		ourRendering string) (differentialDifference, bool) {
		return differentialDifference{cause: cause, function: name, where: where, theirs: theirRendering,
			ours: ourRendering}, true
	}
	if ourName := ours.RelativeString(nil); ourName != name {
		return part("function name", "the function", name, ourName)
	}
	if theirs.Synthetic != ours.Synthetic {
		return part("synthetic", "the function", theirs.Synthetic, ours.Synthetic)
	}
	if theirs.Pos() != ours.Pos() {
		return part("function position", "the function", fmt.Sprint(theirs.Pos()), fmt.Sprint(ours.Pos()))
	}
	if !types.Identical(theirs.Signature, ours.Signature) {
		return part("signature", "the function", theirs.Signature.String(), ours.Signature.String())
	}
	if len(theirs.Params) != len(ours.Parameters) || len(theirs.FreeVars) != len(ours.FreeVariables) ||
		len(theirs.Locals) != len(ours.Locals) || len(theirs.AnonFuncs) != len(ours.AnonymousFunctions) {
		return part("parameter, free variable, local or anonymous function count", "the function",
			fmt.Sprintf("%d %d %d %d", len(theirs.Params), len(theirs.FreeVars), len(theirs.Locals),
				len(theirs.AnonFuncs)),
			fmt.Sprintf("%d %d %d %d", len(ours.Parameters), len(ours.FreeVariables), len(ours.Locals),
				len(ours.AnonymousFunctions)))
	}
	for index, parameter := range theirs.Params {
		ourParameter := ours.Parameters[index]
		if parameter.Name() != ourParameter.Name() || !types.Identical(parameter.Type(), ourParameter.Type()) ||
			parameter.Object() != ourParameter.Object() {
			return part("parameter", fmt.Sprintf("parameter %d", index),
				parameter.Name()+" "+parameter.Type().String(), ourParameter.Name()+" "+ourParameter.Type().String())
		}
	}
	for index, free := range theirs.FreeVars {
		ourFree := ours.FreeVariables[index]
		if free.Name() != ourFree.Name() || !types.Identical(free.Type(), ourFree.Type()) {
			return part("free variable", fmt.Sprintf("free variable %d", index),
				free.Name()+" "+free.Type().String(), ourFree.Name()+" "+ourFree.Type().String())
		}
	}
	if len(theirs.Blocks) != len(ours.Blocks) {
		return part("block count", "the function", fmt.Sprint(len(theirs.Blocks)), fmt.Sprint(len(ours.Blocks)))
	}
	theirRecover, ourRecover := -1, -1
	if theirs.Recover != nil {
		theirRecover = theirs.Recover.Index
	}
	if ours.Recover != nil {
		ourRecover = ours.Recover.Index
	}
	if theirRecover != ourRecover {
		return part("recover block", "the function", fmt.Sprint(theirRecover), fmt.Sprint(ourRecover))
	}
	theirPlaces := differentialPlaceTheirs(theirs)
	ourPlaces := differentialPlaceOurs(ours)
	for blockIndex, theirBlock := range theirs.Blocks {
		ourBlock := ours.Blocks[blockIndex]
		where := fmt.Sprintf("block %d", blockIndex)
		theirEdges := differentialTheirEdges(theirBlock)
		ourEdges := differentialOurEdges(ourBlock)
		if theirEdges != ourEdges {
			return part("edges", where, theirEdges, ourEdges)
		}
		// go/ir labels the range-over-slice header "rangeindex.loop"; ours marks it RangeIndexLoop instead
		// (go-ir-shape.md, section 4, delta 1).
		if (theirBlock.Comment == "rangeindex.loop") != ourBlock.RangeIndexLoop {
			return part("range index loop marker", where, theirBlock.Comment, fmt.Sprint(ourBlock.RangeIndexLoop))
		}
		if len(theirBlock.Instrs) != len(ourBlock.Instructions) {
			return part("instruction count", where, differentialTheirKinds(theirBlock), differentialOurKinds(ourBlock))
		}
		for index, theirInstruction := range theirBlock.Instrs {
			ourInstruction := ourBlock.Instructions[index]
			instructionWhere := fmt.Sprintf("block %d, instruction %d (go/ir: %s)", blockIndex, index,
				theirInstruction.String())
			cause, theirRendering, ourRendering, differs := differentialCompareInstruction(theirPlaces, ourPlaces,
				theirInstruction, ourInstruction)
			if differs {
				return part(cause, instructionWhere, theirRendering, ourRendering)
			}
		}
	}
	return differentialDifference{}, false
}

func differentialTheirEdges(block *ir.BasicBlock) string {
	var predecessors []int
	var successors []int
	for _, predecessor := range block.Preds {
		predecessors = append(predecessors, predecessor.Index)
	}
	for _, successor := range block.Succs {
		successors = append(successors, successor.Index)
	}
	return fmt.Sprintf("predecessors %v, successors %v", predecessors, successors)
}

func differentialOurEdges(block *intermediate_representation.BasicBlock) string {
	var predecessors []int
	var successors []int
	for _, predecessor := range block.Predecessors {
		predecessors = append(predecessors, predecessor.Index)
	}
	for _, successor := range block.Successors {
		successors = append(successors, successor.Index)
	}
	return fmt.Sprintf("predecessors %v, successors %v", predecessors, successors)
}

func differentialTheirKinds(block *ir.BasicBlock) string {
	var kinds []string
	for _, instruction := range block.Instrs {
		kinds = append(kinds, differentialKind(reflect.TypeOf(instruction)))
	}
	return strings.Join(kinds, " ")
}

func differentialOurKinds(block *intermediate_representation.BasicBlock) string {
	var kinds []string
	for _, instruction := range block.Instructions {
		kinds = append(kinds, differentialKind(reflect.TypeOf(instruction)))
	}
	return strings.Join(kinds, " ")
}

// differentialReport logs the tally, the differences grouped by cause with a few of each, and the time and peak
// memory taken, then fails the test if any function differs.
func differentialReport(test *testing.T, corpus string, tally differentialTally, elapsed time.Duration) {
	test.Helper()
	corpusName := corpus
	if corpusName == "" {
		corpusName = "ordinary"
	}
	peak := differentialPeakResident()
	test.Logf("corpus %s: %d packages compared, %d skipped for load errors, %d functions, %d identical "+
		"(%d instructions), %d differ, %d go/ir instance names spelled through an alias; %s, peak resident %s",
		corpusName, tally.packages, len(tally.skipped), tally.functions, tally.identical, tally.instructions,
		len(tally.differences), tally.aliasSpelledInstances, elapsed.Round(time.Millisecond), peak)
	if len(tally.skipped) > 0 {
		test.Logf("skipped: %s", strings.Join(tally.skipped, " "))
	}
	if len(tally.differences) == 0 {
		return
	}
	byCause := map[string][]differentialDifference{}
	for _, difference := range tally.differences {
		byCause[difference.cause] = append(byCause[difference.cause], difference)
	}
	causes := make([]string, 0, len(byCause))
	for cause := range byCause {
		causes = append(causes, cause)
	}
	sort.Slice(causes, func(left int, right int) bool {
		return len(byCause[causes[left]]) > len(byCause[causes[right]])
	})
	for _, cause := range causes {
		examples := byCause[cause]
		test.Errorf("%d functions differ by %s", len(examples), cause)
		for _, example := range examples[:min(len(examples), 3)] {
			test.Logf("%s", differentialDescribe(example))
		}
	}
}
