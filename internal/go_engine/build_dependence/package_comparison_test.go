package build_dependence

import (
	"fmt"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis/passes/ctrlflow"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// packageComparison is one package's comparison under way: its two sides, the judgment it reads units by, the files
// the build compiles, by name, and what it found so far.
type packageComparison struct {
	pair       loweringPair
	judge      loweringJudge
	hosted     *packages.Package
	built      *packages.Package
	builtFiles map[string]bool
	tally      *loweringTally
}

// comparePackage builds a package both ways and compares every function the host's declares with its counterpart in
// the build, found by its file and offset, each anonymous function by its index in its parent, and each synthetic
// function either reaches after it, all counting toward the declared function's unit. The package initializer is
// compared unit by unit (compareInitializer). A host function with no counterpart is counted by why (unpaired).
//
// dropFromBuild, nil but in the test that proves an unpaired function goes red, names functions of the build left out
// of the counterparts, as though the build had not lowered them.
func comparePackage(hosted *packages.Package, built *packages.Package, hostFlows *ctrlflow.CFGs,
	buildFlows *ctrlflow.CFGs, judge loweringJudge,
	dropFromBuild func(*intermediate_representation.Function) bool) loweringTally {
	tally := loweringTally{packages: 1, unknownFields: map[string]bool{}}
	hostPackage, hostPanic := buildPackage(hosted, hostFlows)
	builtPackage, buildPanic := buildPackage(built, buildFlows)
	if hostPanic != "" || buildPanic != "" {
		tally.findings = append(tally.findings, loweringFinding{unit: hosted.ID, difference: loweringDifference{
			function: hosted.ID, where: "building the package", host: hostPanic, build: buildPanic}})
		return tally
	}
	comparison := &packageComparison{
		pair: loweringPair{
			host:  &loweringSide{fileSet: hosted.Fset, unknownFields: tally.unknownFields},
			build: &loweringSide{fileSet: built.Fset, unknownFields: tally.unknownFields},
		},
		judge:      judge,
		hosted:     hosted,
		built:      built,
		builtFiles: map[string]bool{},
		tally:      &tally,
	}
	for _, file := range built.Syntax {
		comparison.builtFiles[built.Fset.File(file.FileStart).Name()] = true
	}
	counterparts := map[string]*intermediate_representation.Function{}
	for _, function := range builtPackage.Functions {
		if dropFromBuild != nil && dropFromBuild(function) {
			continue
		}
		if place := syntaxPlace(built.Fset, function); place != "" {
			counterparts[place] = function
		}
	}
	for _, function := range hostPackage.Functions {
		if function.Synthetic == "package initializer" {
			comparison.compareInitializer(function, builtPackage.Function("init"))
			continue
		}
		place := syntaxPlace(hosted.Fset, function)
		counterpart, isShared := counterparts[place]
		if place == "" || !isShared {
			comparison.unpairedFunction(function)
			continue
		}
		unitIndex, isUnit := judge.unitAt(hosted.ID, hosted.Fset, function.Syntax().Pos())
		comparison.compareFunctions(function, counterpart, unitIndex, isUnit)
	}
	return tally
}

// unpairedFunction counts a host function with no counterpart in the build by why (unpaired): its syntax's file and
// the unit holding it.
func (comparison *packageComparison) unpairedFunction(function *intermediate_representation.Function) {
	name := function.RelativeString(nil)
	if function.Syntax() == nil {
		comparison.unpaired(name, "", 0, false)
		return
	}
	position := function.Syntax().Pos()
	unitIndex, isUnit := comparison.judge.unitAt(comparison.hosted.ID, comparison.hosted.Fset, position)
	comparison.unpaired(fmt.Sprintf("%s at %s", name, syntaxPlace(comparison.hosted.Fset, function)),
		comparison.hosted.Fset.File(position).Name(), unitIndex, isUnit)
}

// unpaired counts what the host lowered and the comparison found no counterpart for, by why: built from no syntax, in
// a file only the host compiles, both of which the build has no counterpart for by construction; in a unit block 1
// calls independent, which a shared file's always has, so it is a function the gate would pass uncompared; or
// anything else, named. The last two fail like a difference (loweringFailures).
func (comparison *packageComparison) unpaired(name string, file string, unitIndex int, isUnit bool) {
	tally := comparison.tally
	switch {
	case file == "":
		tally.unpairedNoSyntax++
	case !comparison.builtFiles[file]:
		tally.unpairedHostOnly++
	case isUnit && !comparison.judge.units[unitIndex].Dependent:
		judgedAgain := ""
		if comparison.judge.judgedAgain[unitIndex] {
			judgedAgain = ", judged again for a callee's no-return"
		}
		tally.unpairedIndependent = append(tally.unpairedIndependent, fmt.Sprintf("%s %s, in %s%s",
			comparison.hosted.ID, name, comparison.judge.unitLabel(unitIndex), judgedAgain))
	case isUnit:
		tally.unpairedOther = append(tally.unpairedOther, fmt.Sprintf("%s %s, in the dependent unit %s",
			comparison.hosted.ID, name, comparison.judge.unitLabel(unitIndex)))
	default:
		tally.unpairedOther = append(tally.unpairedOther, fmt.Sprintf("%s %s, in a shared file and no unit",
			comparison.hosted.ID, name))
	}
}

// syntaxPlace is the file and offset of the syntax function was built from, empty for a function built from none.
func syntaxPlace(fileSet *token.FileSet, function *intermediate_representation.Function) string {
	if function.Syntax() == nil {
		return ""
	}
	tokenFile := fileSet.File(function.Syntax().Pos())
	return fmt.Sprintf("%s:%d", tokenFile.Name(), tokenFile.Offset(function.Syntax().Pos()))
}

// compareFunctions compares hosted with built, then each anonymous function and each synthetic function they reach,
// all counting toward the unit at unitIndex: independent when isUnit and the judgment says so, and dependent when the
// graph knows no unit there, which is never read as independent.
func (comparison *packageComparison) compareFunctions(hosted *intermediate_representation.Function,
	built *intermediate_representation.Function, unitIndex int, isUnit bool) {
	tally, judge := comparison.tally, comparison.judge
	independent := isUnit && !judge.units[unitIndex].Dependent
	pairs := [][2]*intermediate_representation.Function{{hosted, built}}
	seen := map[*intermediate_representation.Function]bool{}
	for len(pairs) > 0 {
		current := pairs[0]
		pairs = pairs[1:]
		if seen[current[0]] {
			continue
		}
		seen[current[0]] = true
		comparison.pair.host.reached, comparison.pair.build.reached = nil, nil
		difference, isDifferent, instructions := comparison.pair.compareFunction(current[0], current[1])
		if current[0].Synthetic != "" {
			tally.synthetic++
		}
		if !isDifferent {
			for index, anonymous := range current[0].AnonymousFunctions {
				pairs = append(pairs, [2]*intermediate_representation.Function{anonymous,
					current[1].AnonymousFunctions[index]})
			}
			pairs = append(pairs, reachedPairs(comparison.pair)...)
		}
		if !independent {
			tally.dependent++
			if isDifferent {
				tally.dependentDiffer++
			}
			continue
		}
		tally.independent++
		tally.instructions += instructions
		if judge.judgedAgain[unitIndex] {
			tally.judgedAgain++
		}
		if isDifferent {
			tally.findings = append(tally.findings, loweringFinding{unit: judge.unitLabel(unitIndex),
				difference: difference, calleeDependent: judge.calleeDependent(unitIndex),
				judgedAgain: judge.judgedAgain[unitIndex]})
		}
	}
}

// reachedPairs are the synthetic functions the two sides' last renderings reached, paired: renderings that compared
// equal name the same synthetic functions, each marked built, in the same order.
func reachedPairs(pair loweringPair) [][2]*intermediate_representation.Function {
	pairs := make([][2]*intermediate_representation.Function, 0, len(pair.host.reached))
	for index, reached := range pair.host.reached {
		pairs = append(pairs, [2]*intermediate_representation.Function{reached, pair.build.reached[index]})
	}
	return pairs
}

// The package initializer's segments that belong to no unit: what runs before any variable's initializer, and the
// calls of the imported packages' initializers.
const (
	initializerFrame   = "the initializer's frame"
	importInitializers = "the imported packages' initializers"
)

// initializerSegment is the instructions of a package initializer that belong to one unit, or to the frame or the
// imports, in order.
type initializerSegment struct {
	key          string
	unitIndex    int
	isUnit       bool
	instructions []intermediate_representation.Instruction
}

// initializerSegments splits a package initializer's instructions by what they belong to, each segment in order. An
// instruction is anchored by its position to the unit holding it; a store without a position into a declared
// variable, to the variable's unit; into the initializer's guard, to the frame; a store into the blank identifier,
// to the next blank variable in the initialization order (initOrder) not yet stored, since the initializer lowers
// the variables in that order and each blank one ends in its store; and a call of an imported package's initializer,
// to the imports. Any other instruction without a position waits for the next anchored one and joins its segment,
// since a variable's initializer computes its value before its store, but for a branch or return without one, which
// ends the segment it is in, and the closing jump to the initializer's return and that return, which are the frame's,
// as is whatever is left waiting at the end. A position in a file only one build compiles names no unit the host and
// build share, and its segment is left out of the comparison.
//
// It returns the segments, their keys in order, and each instruction that is a value named by its segment and its
// index there, the places by which the operands of every other instruction name it, so a segment's lowering reads the
// same wherever in the initializer it sits.
func (comparison *packageComparison) initializerSegments(side *loweringSide,
	function *intermediate_representation.Function, initOrder []*types.Initializer) (map[string]*initializerSegment,
	[]string, map[intermediate_representation.Value]string) {
	// Each variable initializer's segment, whether it stores to the blank identifier, and whether that store was met.
	initializerKeys := make([]string, len(initOrder))
	blankInitializer := make([]bool, len(initOrder))
	blankStored := make([]bool, len(initOrder))
	for index, initializer := range initOrder {
		initializerKeys[index], _, _ = comparison.anchorAt(side, initializer.Lhs[0].Pos())
		blankInitializer[index] = len(initializer.Lhs) == 1 && initializer.Lhs[0].Name() == "_"
	}
	cursor := 0
	segments := map[string]*initializerSegment{}
	var order []string
	places := map[intermediate_representation.Value]string{}
	current := &initializerSegment{key: initializerFrame}
	segments[initializerFrame] = current
	order = append(order, initializerFrame)
	var waiting []intermediate_representation.Instruction
	join := func(segment *initializerSegment, instructions ...intermediate_representation.Instruction) {
		for _, instruction := range instructions {
			if value, isValue := instruction.(intermediate_representation.Value); isValue {
				places[value] = fmt.Sprintf("%s #%d", segment.key, len(segment.instructions))
			}
			segment.instructions = append(segment.instructions, instruction)
		}
	}
	for _, block := range function.Blocks {
		for _, instruction := range block.Instructions {
			key, unitIndex, isUnit, isAnchored := comparison.anchor(side, instruction)
			if store, isBlankStore := instruction.(*intermediate_representation.BlankStore); isBlankStore &&
				!store.Pos().IsValid() {
				for index := cursor; index < len(initOrder); index++ {
					if blankInitializer[index] && !blankStored[index] {
						blankStored[index], cursor = true, index
						key, unitIndex, isUnit = comparison.anchorAt(side, initOrder[index].Lhs[0].Pos())
						isAnchored = true
						break
					}
				}
			}
			if isAnchored {
				// The initialization order moves on to the first variable initializer the anchor names.
				for index := cursor; index < len(initOrder); index++ {
					if initializerKeys[index] == key {
						cursor = index
						break
					}
				}
			}
			if !isAnchored {
				switch instruction := instruction.(type) {
				case *intermediate_representation.Return:
					join(segments[initializerFrame], append(waiting, instruction)...)
					waiting = nil
				case *intermediate_representation.Jump:
					if closesInitializer(instruction) {
						join(segments[initializerFrame], append(waiting, instruction)...)
					} else {
						join(current, append(waiting, instruction)...)
					}
					waiting = nil
				case *intermediate_representation.If, *intermediate_representation.Panic:
					join(current, append(waiting, instruction)...)
					waiting = nil
				default:
					waiting = append(waiting, instruction)
				}
				continue
			}
			segment, isSegment := segments[key]
			if !isSegment {
				segment = &initializerSegment{key: key, unitIndex: unitIndex, isUnit: isUnit}
				segments[key] = segment
				order = append(order, key)
			}
			join(segment, append(waiting, instruction)...)
			waiting = nil
			current = segment
		}
	}
	join(segments[initializerFrame], waiting...)
	return segments, order, places
}

// anchor is the segment an instruction of a package initializer is anchored to, and its unit, if it is anchored
// (initializerSegments).
func (comparison *packageComparison) anchor(side *loweringSide,
	instruction intermediate_representation.Instruction) (string, int, bool, bool) {
	position := instruction.Pos()
	if !position.IsValid() {
		switch instruction := instruction.(type) {
		case *intermediate_representation.Store:
			global, isGlobal := instruction.Address.(*intermediate_representation.Global)
			switch {
			case !isGlobal:
				return "", 0, false, false
			case global.Object() == nil:
				return initializerFrame, 0, false, true
			}
			position = global.Object().Pos()
		case *intermediate_representation.Call:
			if callee := instruction.Call.StaticCallee(); callee != nil && callee.Synthetic == "package initializer" {
				return importInitializers, 0, false, true
			}
			return "", 0, false, false
		default:
			return "", 0, false, false
		}
	}
	key, unitIndex, isUnit := comparison.anchorAt(side, position)
	return key, unitIndex, isUnit, true
}

// anchorAt is the segment of the unit holding position, and that unit, or a segment of its own for a position no unit
// the host and build share holds.
func (comparison *packageComparison) anchorAt(side *loweringSide, position token.Pos) (string, int, bool) {
	unitIndex, isUnit := comparison.judge.unitAt(comparison.hosted.ID, side.fileSet, position)
	if !isUnit {
		return "no unit at " + side.place(position), 0, false
	}
	return fmt.Sprintf("unit %d", unitIndex), unitIndex, true
}

// closesInitializer is whether jump goes to a block that only returns, the package initializer's last.
func closesInitializer(jump *intermediate_representation.Jump) bool {
	successors := jump.Block().Successors
	if len(successors) != 1 || len(successors[0].Instructions) != 1 {
		return false
	}
	_, isReturn := successors[0].Instructions[0].(*intermediate_representation.Return)
	return isReturn
}

// compareInitializer compares a package initializer segment by segment (initializerSegments), each unit's segment
// counting toward its unit and the frame and imports apart, as no unit's; then each of its anonymous functions, the
// function literals of variables' initializers, paired by their place and counted toward the unit holding them.
// Blocks are not compared by index here, since a variable only one build declares moves every block after it; each
// segment's branches and phis are compared as instructions, their operands by segment.
func (comparison *packageComparison) compareInitializer(hosted *intermediate_representation.Function,
	built *intermediate_representation.Function) {
	tally, judge, pair := comparison.tally, comparison.judge, comparison.pair
	hostSegments, order, hostPlaces := comparison.initializerSegments(pair.host, hosted,
		comparison.hosted.TypesInfo.InitOrder)
	buildSegments, _, buildPlaces := comparison.initializerSegments(pair.build, built,
		comparison.built.TypesInfo.InitOrder)
	pair.host.places, pair.build.places = hostPlaces, buildPlaces
	// The synthetic functions an independent unit's segment reaches, compared once every segment is, since comparing
	// a function names its own values in the places the segments' names are in.
	type reachedFromUnit struct {
		functions [2]*intermediate_representation.Function
		unitIndex int
	}
	var reachedFromUnits []reachedFromUnit
	for _, key := range order {
		hostSegment, buildSegment := hostSegments[key], buildSegments[key]
		if place, isNoUnit := strings.CutPrefix(key, "no unit at "); isNoUnit {
			// The key holds the place as file:offset, and a file's name may hold a colon, so the offset is cut at
			// the last one.
			file := place[:strings.LastIndex(place, ":")]
			comparison.unpaired(hosted.RelativeString(nil)+", the segment "+key, file, 0, false)
			continue
		}
		pair.host.reached, pair.build.reached = nil, nil
		where, hostRendering, buildRendering, isDifferent := "", "", "", false
		if buildSegment == nil {
			where, hostRendering, buildRendering, isDifferent = "the segment", instructionKinds(
				hostSegment.instructions), "none", true
		} else {
			where, hostRendering, buildRendering, isDifferent = compareSegments(pair, hostSegment, buildSegment)
		}
		difference := loweringDifference{function: hosted.RelativeString(nil) + ", " + key, where: where,
			host: hostRendering, build: buildRendering}
		switch {
		case !hostSegment.isUnit:
			tally.packageLevel++
			if isDifferent {
				tally.packageLevelDiffer = append(tally.packageLevelDiffer, loweringFinding{
					unit: comparison.hosted.ID + " " + key, difference: difference})
			}
			continue
		case judge.units[hostSegment.unitIndex].Dependent:
			tally.dependent++
			if isDifferent {
				tally.dependentDiffer++
			}
			continue
		}
		tally.independent++
		tally.segments++
		tally.instructions += len(hostSegment.instructions)
		if judge.judgedAgain[hostSegment.unitIndex] {
			tally.judgedAgain++
		}
		if isDifferent {
			tally.findings = append(tally.findings, loweringFinding{unit: judge.unitLabel(hostSegment.unitIndex),
				difference: difference, calleeDependent: judge.calleeDependent(hostSegment.unitIndex),
				judgedAgain: judge.judgedAgain[hostSegment.unitIndex]})
			continue
		}
		for _, reached := range reachedPairs(pair) {
			reachedFromUnits = append(reachedFromUnits, reachedFromUnit{functions: reached,
				unitIndex: hostSegment.unitIndex})
		}
	}
	for _, reached := range reachedFromUnits {
		comparison.compareFunctions(reached.functions[0], reached.functions[1], reached.unitIndex, true)
	}
	for key, buildSegment := range buildSegments {
		if _, isHosted := hostSegments[key]; !isHosted && !strings.HasPrefix(key, "no unit at ") {
			tally.packageLevelDiffer = append(tally.packageLevelDiffer, loweringFinding{
				unit: comparison.hosted.ID + " " + key, difference: loweringDifference{
					function: hosted.RelativeString(nil), where: "the segment, only the build's",
					host: "none", build: instructionKinds(buildSegment.instructions)}})
		}
	}
	builtAnonymous := map[string]*intermediate_representation.Function{}
	for _, anonymous := range built.AnonymousFunctions {
		builtAnonymous[syntaxPlace(pair.build.fileSet, anonymous)] = anonymous
	}
	for _, anonymous := range hosted.AnonymousFunctions {
		counterpart, isShared := builtAnonymous[syntaxPlace(pair.host.fileSet, anonymous)]
		if !isShared {
			comparison.unpairedFunction(anonymous)
			continue
		}
		unitIndex, isUnit := judge.unitAt(comparison.hosted.ID, pair.host.fileSet, anonymous.Syntax().Pos())
		comparison.compareFunctions(anonymous, counterpart, unitIndex, isUnit)
	}
}

// compareSegments compares two segments of a package initializer instruction by instruction, each side's places
// already naming every value by its segment, and returns where they first part and what each holds.
func compareSegments(pair loweringPair, hostSegment *initializerSegment,
	buildSegment *initializerSegment) (string, string, string, bool) {
	if len(hostSegment.instructions) != len(buildSegment.instructions) {
		return "the segment's instructions", instructionKinds(hostSegment.instructions),
			instructionKinds(buildSegment.instructions), true
	}
	for index, instruction := range hostSegment.instructions {
		hostRendering := pair.host.instruction(instruction)
		buildRendering := pair.build.instruction(buildSegment.instructions[index])
		if hostRendering != buildRendering {
			return fmt.Sprintf("the segment's instruction %d", index), hostRendering, buildRendering, true
		}
	}
	return "", "", "", false
}
