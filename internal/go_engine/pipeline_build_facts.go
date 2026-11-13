package go_engine

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/system-inc/cohere/internal/go_engine/build_dependence"
	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/contract"
)

// factExamples is how many differing facts of a kind the record names, enough to see what moves without writing every
// one of thousands.
const factExamples = 3

// buildJudgment is one extra build's judgment on the host's graph (dependenceOf), and what its facts are compared
// through: the files both builds compile, by package ID and path as written, and the packages both have by one ID.
type buildJudgment struct {
	dependence     *build_dependence.Dependence
	shared         map[sharedFile]bool
	sharedPackages map[string]bool
}

// sharedFile is a file of a package, as written.
type sharedFile struct {
	packageIdentifier string
	file              string
}

// factKey is one fact as two builds can match it: its package ID, the object it is about by the file a reader sees
// it in, its offset in the file parsed and its name, and the analyzer and fact type, the file empty and the offset -1
// for a fact about the package itself. An object in a file both builds compile sits at the same offset in each, since
// the file's bytes are the same, so its key is the same in both, as its unit's key is.
type factKey struct {
	packageIdentifier string
	file              string
	offset            int
	name              string
	analyzer          string
	factType          string
}

// factKeyOf is fact's key, and false for a fact about an object with no position, which no other build can match.
func factKeyOf(fact check.ExportedFact) (factKey, bool) {
	key := factKey{packageIdentifier: fact.Package.ID, offset: -1, analyzer: fact.Analyzer, factType: fact.FactType}
	if fact.Object == nil {
		return key, true
	}
	position := fact.Object.Pos()
	tokenFile := fact.Package.Fset.File(position)
	if !position.IsValid() || tokenFile == nil {
		return factKey{}, false
	}
	key.file = tokenFile.Position(position).Filename
	key.offset = tokenFile.Offset(position)
	key.name = fact.Object.Name()
	return key, true
}

// indexHostFacts keeps the host's facts by key, for every extra build's to be compared with, when the run judges
// dependence. A fact whose object no build can match is left out.
func (engine *pipeline) indexHostFacts(facts []check.ExportedFact) {
	if engine.dependence == nil {
		return
	}
	indexed := make(map[factKey]check.ExportedFact, len(facts))
	for _, fact := range facts {
		if key, isKeyed := factKeyOf(fact); isKeyed {
			indexed[key] = fact
		}
	}
	engine.dependence.facts = indexed
}

// compareFacts is the build's facts census (#0ghk7vj): each fact the host or the build exported about an object in a
// file both compile, or about a package both have by one ID, compared by the digest of its value, a fact only one
// build exported counted as different, and each answer the fact resolver computed in both builds, compared by its
// encoding, one only a build asked counted apart; then the units declaring an object whose fact differs, and the
// units the judgment calls independent that call those statically, first-order and followed to a fixed point, beside
// every independent unit that calls a dependent one. Nil when the host's facts were never indexed.
func (engine *pipeline) compareFacts(judged *buildJudgment, facts []check.ExportedFact) *contract.DependenceFacts {
	if engine.dependence.facts == nil {
		return nil
	}
	started := time.Now()
	built := make(map[factKey]check.ExportedFact, len(facts))
	for _, fact := range facts {
		if key, isKeyed := factKeyOf(fact); isKeyed {
			built[key] = fact
		}
	}
	units := judged.dependence.Units()
	kinds := map[[2]string]*contract.FactKindCounts{}
	examples := map[[2]string][]string{}
	differing := map[int]bool{}
	compare := func(key factKey, hosted check.ExportedFact, hostHas bool, builtFact check.ExportedFact,
		buildHas bool) {
		if !judged.sharedPackages[key.packageIdentifier] ||
			key.file != "" && !judged.shared[sharedFile{packageIdentifier: key.packageIdentifier, file: key.file}] {
			return
		}
		kind := [2]string{key.analyzer, key.factType}
		counts := kinds[kind]
		if counts == nil {
			counts = &contract.FactKindCounts{Analyzer: key.analyzer, FactType: key.factType}
			kinds[kind] = counts
		}
		// An answer one build alone holds is a question only it asked, which says nothing of the fact.
		if (hosted.Answer || builtFact.Answer) && (!hostHas || !buildHas) {
			counts.AskedInOne++
			return
		}
		unit, isUnit := -1, false
		if key.file != "" {
			unit, isUnit = judged.dependence.UnitContaining(key.packageIdentifier, key.file, key.offset)
		}
		inWorkspace := isUnit && units[unit].Workspace
		counts.Compared++
		if inWorkspace {
			counts.WorkspaceCompared++
		}
		if hostHas && buildHas && !hosted.Undigestable && !builtFact.Undigestable && hosted.Digest == builtFact.Digest {
			counts.Equal++
			if inWorkspace {
				counts.WorkspaceEqual++
			}
			return
		}
		counts.Different++
		if inWorkspace {
			counts.WorkspaceDifferent++
		}
		examples[kind] = append(examples[kind], factExample(key, hosted, hostHas, builtFact, buildHas))
		if isUnit {
			differing[unit] = true
		}
	}
	for key, hosted := range engine.dependence.facts {
		builtFact, buildHas := built[key]
		compare(key, hosted, true, builtFact, buildHas)
	}
	for key, builtFact := range built {
		if _, hostHas := engine.dependence.facts[key]; !hostHas {
			compare(key, check.ExportedFact{}, false, builtFact, true)
		}
	}
	census := &contract.DependenceFacts{Kinds: []contract.FactKindCounts{}}
	for _, kind := range slices.SortedFunc(maps.Keys(kinds), func(left [2]string, right [2]string) int {
		return cmp.Or(cmp.Compare(left[0], right[0]), cmp.Compare(left[1], right[1]))
	}) {
		counts := *kinds[kind]
		slices.Sort(examples[kind])
		counts.Examples = examples[kind][:min(factExamples, len(examples[kind]))]
		census.Kinds = append(census.Kinds, counts)
	}
	census.Workspace, census.Dependencies = factCallers(units, differing)
	census.Milliseconds = int(time.Since(started).Milliseconds())
	return census
}

// factExample names one differing fact: its package, its object and both builds' values, or that one build has none.
func factExample(
	key factKey,
	hosted check.ExportedFact,
	hostHas bool,
	builtFact check.ExportedFact,
	buildHas bool,
) string {
	value := func(fact check.ExportedFact, has bool) string {
		switch {
		case !has:
			return "none"
		case fact.Answer:
			return string(fact.Encoded)
		}
		return fmt.Sprintf("%v", fact.Fact)
	}
	object := "the package"
	if key.file != "" {
		object = fmt.Sprintf("%s at %s:%d", key.name, key.file, key.offset)
	}
	return fmt.Sprintf("%s, %s: host %s, build %s", key.packageIdentifier, object, value(hosted, hostHas),
		value(builtFact, buildHas))
}

// factCallers counts, in the workspace and in the dependencies by each unit's own scope, the units differing names
// and what they reach (contract.FactCallers): first the independent units calling one of them, then the fixed point
// that takes each such caller as differing in turn, and every independent unit that calls a dependent one.
func factCallers(units []build_dependence.Unit, differing map[int]bool) (contract.FactCallers, contract.FactCallers) {
	callers := make([][]int, len(units))
	for index, unit := range units {
		for _, callee := range unit.Calls {
			callers[callee] = append(callers[callee], index)
		}
	}
	firstOrder := map[int]bool{}
	for index, unit := range units {
		if unit.Dependent {
			continue
		}
		if slices.ContainsFunc(unit.Calls, func(callee int) bool { return differing[callee] }) {
			firstOrder[index] = true
		}
	}
	bound := map[int]bool{}
	reached := maps.Clone(differing)
	pending := slices.Collect(maps.Keys(differing))
	for len(pending) > 0 {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		for _, caller := range callers[current] {
			if units[caller].Dependent || bound[caller] {
				continue
			}
			bound[caller] = true
			if !reached[caller] {
				reached[caller] = true
				pending = append(pending, caller)
			}
		}
	}
	var workspace contract.FactCallers
	var dependencies contract.FactCallers
	for index, unit := range units {
		scope := &dependencies
		if unit.Workspace {
			scope = &workspace
		}
		if differing[index] {
			scope.DifferingUnits++
			scope.DifferingNodes += unit.Nodes
		}
		if firstOrder[index] {
			scope.FirstOrderUnits++
			scope.FirstOrderNodes += unit.Nodes
		}
		if bound[index] {
			scope.BoundUnits++
			scope.BoundNodes += unit.Nodes
		}
		callsDependent := slices.ContainsFunc(unit.Calls, func(callee int) bool { return units[callee].Dependent })
		if !unit.Dependent && callsDependent {
			scope.CallerUnits++
			scope.CallerNodes += unit.Nodes
		}
	}
	return workspace, dependencies
}
