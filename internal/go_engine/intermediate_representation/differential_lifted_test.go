package intermediate_representation_test

// The lifted differential (#ee33njz): go/ir lifted, as buildir builds it but for GlobalDebug, against ours lifted
// through the shared core. It reuses the naive differential's loading and its instruction comparison, and differs
// in what it compares:
//
//   - Phis aren't compared in place. Braun's construction and lift.go's dominance frontiers may place different
//     phis for the same values, so an operand, on either side, is compared by the set of non-phi values it
//     reaches through phis, recursively. A different reaching value still differs; a different placement doesn't.
//   - Every other instruction is compared in lockstep, by its index among its block's non-phi instructions.
//   - Phis are counted on both sides. go/ir keeps the phis its simplifyPhis replaced in their blocks, with no
//     referrers; ours drops them (lifting.go), so go/ir's are counted with and without them.

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/tools/go/packages"
	"honnef.co/go/tools/go/ir"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/static_single_assignment"
)

// differentialLiftedTally adds phi counts to a differential tally.
type differentialLiftedTally struct {
	differentialTally

	// theirPhis are go/ir's phis, theirPhisWithoutReferrers the ones among them simplifyPhis replaced and left in
	// place, and ourPhis ours.
	theirPhis                 int
	theirPhisWithoutReferrers int
	ourPhis                   int

	// verifiedRounds are the rounds of our lifting the core's verifier checked.
	verifiedRounds int

	// phiDeltas counts the compared functions by go/ir's phis with referrers less ours, and phiDeltaExamples
	// names up to three functions for each delta but zero.
	phiDeltas        map[int]int
	phiDeltaExamples map[int][]string
}

func differentialMergeLifted(tally differentialLiftedTally, other differentialLiftedTally) differentialLiftedTally {
	deltas := maps.Clone(tally.phiDeltas)
	if deltas == nil {
		deltas = map[int]int{}
	}
	for delta, count := range other.phiDeltas {
		deltas[delta] += count
	}
	examples := map[int][]string{}
	for _, each := range []map[int][]string{tally.phiDeltaExamples, other.phiDeltaExamples} {
		for delta, names := range each {
			examples[delta] = append(examples[delta], names...)[:min(3, len(examples[delta])+len(names))]
		}
	}
	return differentialLiftedTally{
		differentialTally:         differentialMerge(tally.differentialTally, other.differentialTally),
		theirPhis:                 tally.theirPhis + other.theirPhis,
		theirPhisWithoutReferrers: tally.theirPhisWithoutReferrers + other.theirPhisWithoutReferrers,
		ourPhis:                   tally.ourPhis + other.ourPhis,
		verifiedRounds:            tally.verifiedRounds + other.verifiedRounds,
		phiDeltas:                 deltas,
		phiDeltaExamples:          examples,
	}
}

// TestLiftedDifferential compares the ordinary corpus lifted, or the one COHERE_GO_IR_DIFFERENTIAL names, and
// fails on any difference.
func TestLiftedDifferential(test *testing.T) {
	test.Parallel()
	corpus := os.Getenv(differentialCorpusVariable)
	started := time.Now()
	var tally differentialLiftedTally
	if corpus == "" {
		for _, unit := range differentialTestdataUnits(test) {
			tally = differentialMergeLifted(tally, differentialCompareLiftedUnit(test, unit, false))
		}
		tally = differentialMergeLifted(tally, differentialCompareLiftedLoaded(test, differentialStandardPackages))
	} else {
		pattern := corpus
		switch corpus {
		case "root":
			pattern = "./..."
		case "std":
			pattern = "std"
		}
		tally = differentialMergeLifted(tally, differentialCompareLiftedLoaded(test, []string{pattern}))
	}
	deltas := slices.Sorted(maps.Keys(tally.phiDeltas))
	var rendered []string
	for _, delta := range deltas {
		entry := fmt.Sprintf("%+d: %d", delta, tally.phiDeltas[delta])
		if examples := tally.phiDeltaExamples[delta]; len(examples) > 0 {
			entry += " (" + strings.Join(examples, ", ") + ")"
		}
		rendered = append(rendered, entry)
	}
	test.Logf("phis: go/ir %d (%d of them replaced and left without referrers), ours %d; functions by go/ir's "+
		"phis with referrers less ours: %s; %d rounds of our lifting verified", tally.theirPhis,
		tally.theirPhisWithoutReferrers, tally.ourPhis, strings.Join(rendered, ", "), tally.verifiedRounds)
	differentialReport(test, "lifted "+differentialCorpusName(corpus), tally.differentialTally, time.Since(started))
}

// TestLiftedDifferentialFailsOnASkippedStore plants a mutant in our lifting of a fixed function: the first Store
// it lifts defines nothing, so the value stored there reaches no Load. The comparator finds that function.
func TestLiftedDifferentialFailsOnASkippedStore(test *testing.T) {
	test.Parallel()
	unit := differentialCheck(test, "mutant", `package mutant

func Overwritten(first int, second int) int {
	value := first
	value = second
	return value
}
`)
	tally := differentialCompareLiftedUnit(test, unit, true)
	if len(tally.differences) == 0 {
		test.Fatal("the comparator found nothing in the mutant")
	}
	for _, difference := range tally.differences {
		test.Logf("the comparator reports the mutant: %s", differentialDescribe(difference))
		if difference.function != "mutant.Overwritten" {
			test.Errorf("the comparator found a difference outside the mutant, in %s", difference.function)
		}
	}
}

// differentialCorpusName is the corpus a run names, ordinary when it names none.
func differentialCorpusName(corpus string) string {
	if corpus == "" {
		return "ordinary"
	}
	return corpus
}

// differentialCompareLiftedLoaded is differentialCompareLoaded for the lifted comparison.
func differentialCompareLiftedLoaded(test *testing.T, patterns []string) differentialLiftedTally {
	test.Helper()
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
	var tally differentialLiftedTally
	for _, each := range loaded {
		if len(each.Errors) > 0 || each.Types == nil || each.TypesInfo == nil {
			tally.skipped = append(tally.skipped, each.PkgPath)
			continue
		}
		unit := differentialUnit{path: each.PkgPath, fileSet: each.Fset, types: each.Types, files: each.Syntax,
			typeInfo: each.TypesInfo}
		tally = differentialMergeLifted(tally, differentialCompareLiftedUnit(test, unit, false))
		// Let the package's syntax and types go once both IRs are compared.
		each.Syntax, each.TypesInfo = nil, nil
	}
	return tally
}

// differentialCompareLiftedUnit builds unit both ways, lifted, and compares every source function. mutant plants
// the skipped-store mutant in ours.
func differentialCompareLiftedUnit(test *testing.T, unit differentialUnit,
	mutant bool) (tally differentialLiftedTally) {
	tally.packages = 1
	tally.phiDeltas = map[int]int{}
	var theirPackage *ir.Package
	theirPanic := differentialBuild(func() {
		// buildir's mode but for GlobalDebug: lifted, no debug references.
		theirProgram := ir.NewProgram(unit.fileSet, 0)
		theirProgram.SetNoReturn(differentialNoReturn)
		for _, imported := range unit.types.Imports() {
			theirProgram.CreatePackage(imported, nil, nil, true)
		}
		theirPackage = theirProgram.CreatePackage(unit.types, unit.files, unit.typeInfo, false)
		theirPackage.Build()
	})
	var ourPackage *intermediate_representation.Package
	var verifying sync.Mutex
	ourPanic := differentialBuild(func() {
		ourProgram := intermediate_representation.NewProgram(unit.fileSet)
		if mutant {
			intermediate_representation.SkipStoreDefinitionForTest(test, ourProgram)
		} else {
			intermediate_representation.VerifyLiftingForTest(test, ourProgram, func(
				function *intermediate_representation.Function,
				violations []static_single_assignment.StaticSingleAssignmentViolation,
			) {
				verifying.Lock()
				defer verifying.Unlock()
				tally.verifiedRounds++
				for _, violation := range violations {
					tally.differences = append(tally.differences, differentialDifference{
						cause: "the core's verifier", function: function.RelativeString(nil),
						where: "a round of lifting", theirs: "", ours: violation.Detail})
				}
			})
		}
		ourProgram.SetNoReturn(differentialNoReturn)
		for _, imported := range unit.types.Imports() {
			ourProgram.CreatePackage(imported, nil, nil, true)
		}
		ourPackage = ourProgram.CreatePackage(unit.types, unit.files, unit.typeInfo, false)
		ourPackage.Build()
	})
	if theirPanic != "" || ourPanic != "" {
		cause := "both lifted builds panicked"
		switch {
		case theirPanic == "":
			cause = "our lifted build panicked"
		case ourPanic == "":
			cause = "go/ir's lifted build panicked"
		}
		tally.differences = append(tally.differences, differentialDifference{cause: cause, function: unit.path,
			where: "building the package", theirs: theirPanic, ours: ourPanic})
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
		theirPhis, theirPhisWithoutReferrers, unexplained := differentialTheirPhiCounts(theirs)
		if unexplained > 0 {
			tally.differences = append(tally.differences, differentialDifference{
				cause: "go/ir phi with no referrers that simplifyPhis didn't replace", function: theirs.RelString(nil),
				where: "the function's phis", theirs: fmt.Sprint(unexplained), ours: ""})
			continue
		}
		ourPhis := differentialOurPhiCount(ours)
		tally.theirPhis += theirPhis
		tally.theirPhisWithoutReferrers += theirPhisWithoutReferrers
		tally.ourPhis += ourPhis
		delta := theirPhis - theirPhisWithoutReferrers - ourPhis
		tally.phiDeltas[delta]++
		if delta != 0 && len(tally.phiDeltaExamples[delta]) < 3 {
			if tally.phiDeltaExamples == nil {
				tally.phiDeltaExamples = map[int][]string{}
			}
			tally.phiDeltaExamples[delta] = append(tally.phiDeltaExamples[delta], theirs.RelString(nil))
		}
		difference, differs := differentialCompareLiftedFunction(theirs, ours)
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

// differentialTheirPhiCounts counts go/ir's phis, those with no referrers, and those among the second that aren't
// ones simplifyPhis replaced: a phi it replaced has edges that all name one value, or constants of one type and
// value, but for itself. lift.go marks such a phi live once it has moved its uses (lift.go:466-471), so it stays in
// its block; any other phi without referrers is a difference of its own.
func differentialTheirPhiCounts(function *ir.Function) (phis int, withoutReferrers int, unexplained int) {
	for _, block := range function.Blocks {
		for _, instruction := range block.Phis() {
			phis++
			phi, isPhi := instruction.(*ir.Phi)
			if !isPhi || len(*phi.Referrers()) > 0 {
				continue
			}
			withoutReferrers++
			if !differentialTheirUselessPhi(phi) {
				unexplained++
			}
		}
	}
	return phis, withoutReferrers, unexplained
}

// differentialTheirUselessPhi is go/ir's isUselessPhi, read from outside the package.
func differentialTheirUselessPhi(phi *ir.Phi) bool {
	var first ir.Value
	for _, edge := range phi.Edges {
		if edge == phi || edge == first {
			continue
		}
		if first == nil {
			first = edge
			continue
		}
		firstConstant, firstIsConstant := first.(*ir.Const)
		edgeConstant, edgeIsConstant := edge.(*ir.Const)
		if firstIsConstant && edgeIsConstant && firstConstant.Type() == edgeConstant.Type() &&
			firstConstant.Value == edgeConstant.Value {
			continue
		}
		return false
	}
	return first != nil
}

func differentialOurPhiCount(function *intermediate_representation.Function) int {
	count := 0
	for _, block := range function.Blocks {
		count += len(block.Phis())
	}
	return count
}

// differentialCompareLiftedFunction is differentialCompareFunction with phis set aside: blocks and edges, then
// each block's non-phi instructions in lockstep, operands named by the values they reach.
func differentialCompareLiftedFunction(
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
	if len(theirs.Blocks) != len(ours.Blocks) {
		return part("block count", "the function", fmt.Sprint(len(theirs.Blocks)), fmt.Sprint(len(ours.Blocks)))
	}
	if len(theirs.Locals) != len(ours.Locals) {
		return part("local count", "the function", fmt.Sprint(len(theirs.Locals)), fmt.Sprint(len(ours.Locals)))
	}
	theirPlaces := differentialLiftedTheirPlaces(theirs)
	ourPlaces := differentialLiftedOurPlaces(ours)
	for blockIndex, theirBlock := range theirs.Blocks {
		ourBlock := ours.Blocks[blockIndex]
		where := fmt.Sprintf("block %d", blockIndex)
		theirEdges := differentialTheirEdges(theirBlock)
		ourEdges := differentialOurEdges(ourBlock)
		if theirEdges != ourEdges {
			return part("edges", where, theirEdges, ourEdges)
		}
		theirInstructions := theirBlock.Instrs[len(theirBlock.Phis()):]
		ourInstructions := ourBlock.Instructions[len(ourBlock.Phis()):]
		if len(theirInstructions) != len(ourInstructions) {
			return part("non-phi instruction count", where, differentialTheirKinds(theirBlock),
				differentialOurKinds(ourBlock))
		}
		for index, theirInstruction := range theirInstructions {
			instructionWhere := fmt.Sprintf("block %d, non-phi instruction %d (go/ir: %s)", blockIndex, index,
				theirInstruction.String())
			cause, theirRendering, ourRendering, differs := differentialCompareInstruction(theirPlaces, ourPlaces,
				theirInstruction, ourInstructions[index])
			if differs {
				return part(cause, instructionWhere, theirRendering, ourRendering)
			}
		}
	}
	return differentialDifference{}, false
}

// differentialLiftedTheirPlaces names each value go/ir's lifted function uses by the set of non-phi values it
// reaches: a non-phi value is the set of itself, a phi the union of its edges', recursively. A non-phi instruction
// is named by its block and its index among the block's non-phi instructions.
func differentialLiftedTheirPlaces(function *ir.Function) differentialTheirPlaces {
	names := map[ir.Value]string{}
	for index, parameter := range function.Params {
		names[parameter] = fmt.Sprintf("parameter %d", index)
	}
	for index, free := range function.FreeVars {
		names[free] = fmt.Sprintf("free variable %d", index)
	}
	var values []ir.Value
	for _, block := range function.Blocks {
		phis := len(block.Phis())
		for index, instruction := range block.Instrs {
			if value, isValue := instruction.(ir.Value); isValue && index >= phis {
				names[value] = fmt.Sprintf("b%d.%d", block.Index, index-phis)
			}
			for _, operand := range instruction.Operands(nil) {
				if *operand != nil {
					values = append(values, *operand)
				}
			}
		}
	}
	name := func(value ir.Value) string {
		if found, isNamed := names[value]; isNamed {
			return found
		}
		return differentialTheirOperand(nil, value)
	}
	places := differentialTheirPlaces{}
	for _, value := range values {
		reached := map[string]bool{}
		differentialTheirReach(value, name, reached, map[*ir.Phi]bool{})
		places[value] = differentialReachedSet(reached)
	}
	return places
}

// differentialTheirReach adds the names value reaches to reached, through phis, each phi once.
//
// Mutates: reached the recursion gathers the set there
// Mutates: visited the recursion marks each phi there so a cycle of phis ends
func differentialTheirReach(value ir.Value, name func(ir.Value) string, reached map[string]bool,
	visited map[*ir.Phi]bool) {
	phi, isPhi := value.(*ir.Phi)
	if !isPhi {
		reached[name(value)] = true
		return
	}
	if visited[phi] {
		return
	}
	visited[phi] = true
	for _, edge := range phi.Edges {
		differentialTheirReach(edge, name, reached, visited)
	}
}

// differentialLiftedOurPlaces is differentialLiftedTheirPlaces for ours.
func differentialLiftedOurPlaces(function *intermediate_representation.Function) differentialOurPlaces {
	names := map[intermediate_representation.Value]string{}
	for index, parameter := range function.Parameters {
		names[parameter] = fmt.Sprintf("parameter %d", index)
	}
	for index, free := range function.FreeVariables {
		names[free] = fmt.Sprintf("free variable %d", index)
	}
	var values []intermediate_representation.Value
	for _, block := range function.Blocks {
		phis := len(block.Phis())
		for index, instruction := range block.Instructions {
			if value, isValue := instruction.(intermediate_representation.Value); isValue && index >= phis {
				names[value] = fmt.Sprintf("b%d.%d", block.Index, index-phis)
			}
			for _, operand := range instruction.Operands(nil) {
				if *operand != nil {
					values = append(values, *operand)
				}
			}
		}
	}
	name := func(value intermediate_representation.Value) string {
		if found, isNamed := names[value]; isNamed {
			return found
		}
		return differentialOurOperand(nil, value)
	}
	places := differentialOurPlaces{}
	for _, value := range values {
		reached := map[string]bool{}
		differentialOurReach(value, name, reached, map[*intermediate_representation.Phi]bool{})
		places[value] = differentialReachedSet(reached)
	}
	return places
}

// differentialOurReach adds the names value reaches to reached, through phis, each phi once.
//
// Mutates: reached the recursion gathers the set there
// Mutates: visited the recursion marks each phi there so a cycle of phis ends
func differentialOurReach(value intermediate_representation.Value, name func(intermediate_representation.Value) string,
	reached map[string]bool, visited map[*intermediate_representation.Phi]bool) {
	phi, isPhi := value.(*intermediate_representation.Phi)
	if !isPhi {
		reached[name(value)] = true
		return
	}
	if visited[phi] {
		return
	}
	visited[phi] = true
	for _, edge := range phi.Edges {
		differentialOurReach(edge, name, reached, visited)
	}
}

// differentialReachedSet renders a set of reached values in one order.
func differentialReachedSet(reached map[string]bool) string {
	names := slices.Collect(maps.Keys(reached))
	sort.Strings(names)
	return "{" + strings.Join(names, ", ") + "}"
}
