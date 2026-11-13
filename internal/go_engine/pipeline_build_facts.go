package go_engine

import (
	"cmp"
	"fmt"
	"go/token"
	"go/types"
	"maps"
	"slices"
	"time"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/objectpath"

	"github.com/system-inc/cohere/internal/go_engine/build_dependence"
	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
)

// factExamples is how many differing facts of a kind the record names, enough to see what moves without writing every
// one of thousands.
const factExamples = 3

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
	return factKeyAt(fact, fact.Object, fact.Package.Fset)
}

// factKeyAt is fact's key with its object named by object, whose position is in fileSet, and false when object has no
// position there.
func factKeyAt(fact check.ExportedFact, object types.Object, fileSet *token.FileSet) (factKey, bool) {
	key := factKey{packageIdentifier: fact.Package.ID, offset: -1, analyzer: fact.Analyzer, factType: fact.FactType}
	if object == nil {
		return key, true
	}
	position := object.Pos()
	tokenFile := fileSet.File(position)
	if !position.IsValid() || tokenFile == nil {
		return factKey{}, false
	}
	key.file = tokenFile.Position(position).Filename
	key.offset = tokenFile.Offset(position)
	key.name = object.Name()
	return key, true
}

// indexHostFacts keeps the host's facts by key, for every extra build's to be compared with, when the run judges
// dependence. A fact whose object no build can match is left out. A package the host restored from the binary's table
// ran no analyzer, but its restored action holds the table's facts and the resolver its answers, about objects read
// from export data, whose positions are no offsets in its files: each is keyed by its counterpart in the standard
// library's graph, loaded from source, so it meets the build's fact about the same object (#ss4t5g6). One with no
// counterpart is left out, as is every fact of a package the host restored that the graph does not hold. A fact the
// table holds about an object export data does not hold has its path alone (check.ExportedFact.ObjectPath), by which
// the graph finds its object. The table's answers of each restored package, restoredAnswers, are kept as they are,
// for compareFacts to read by objectpath.
func (engine *pipeline) indexHostFacts(
	facts []check.ExportedFact,
	restoredAnswers map[*packages.Package]*fact_resolver.Answers,
) {
	if engine.dependence == nil {
		return
	}
	engine.dependence.restoredAnswers = map[string]*fact_resolver.Answers{}
	for restoredPackage, answers := range restoredAnswers {
		engine.dependence.restoredAnswers[restoredPackage.ID] = answers
	}
	standardLibrary := engine.dependence.standardLibrary
	var counterparts *build_dependence.Counterparts
	if standardLibrary != nil && standardLibrary.graph != nil {
		counterparts = build_dependence.NewCounterparts(standardLibrary.graph, nil)
	}
	indexed := make(map[factKey]check.ExportedFact, len(facts))
	for _, fact := range facts {
		key, isKeyed := factKeyOf(fact)
		if engine.dependence.restored[fact.Package.ID] && (fact.Object != nil || fact.ObjectPath != "") {
			isKeyed = false
			var counterpart types.Object
			isFound := false
			switch {
			case counterparts == nil:
			case fact.Object != nil:
				counterpart, isFound = counterparts.Counterpart(fact.Object)
			default:
				counterpart, isFound = counterparts.ObjectAt(fact.Package.PkgPath, fact.ObjectPath)
			}
			if isFound {
				key, isKeyed = factKeyAt(fact, counterpart, standardLibrary.fileSet)
			}
		} else if fact.ObjectPath != "" {
			// A restored package's fact named by its path alone, of a package the host did not restore, is an extra
			// build's own restore, which no census key can name.
			isKeyed = false
		}
		if isKeyed {
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
// every independent unit that calls a dependent one, over both graphs' units (census). Nil when the host's facts were
// never indexed.
func (engine *pipeline) compareFacts(judged *buildJudgment, facts []check.ExportedFact) *contract.DependenceFacts {
	if engine.dependence.facts == nil {
		return nil
	}
	started := time.Now()
	var encoder objectpath.Encoder
	built := make(map[factKey]check.ExportedFact, len(facts))
	for _, fact := range facts {
		key, isKeyed := factKeyOf(fact)
		if !isKeyed {
			continue
		}
		// A fact about an object no objectpath names, a function's local closure say, in a package the host restored
		// from the binary's table, is one no entry can hold, so the host has no side of it to compare with: it is left
		// out, where a host that checked the package from source would have compared it.
		if engine.dependence.restored[fact.Package.ID] && fact.Object != nil {
			if _, pathError := encoder.For(fact.Object); pathError != nil {
				continue
			}
		}
		built[key] = fact
	}
	units, unitContaining := judged.census()
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
			unit, isUnit = unitContaining(key.packageIdentifier, key.file, key.offset)
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
			hosted, hostHas := engine.restoredAnswer(builtFact, &encoder)
			compare(key, hosted, hostHas, builtFact, true)
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

// restoredAnswer is the host's answer of builtFact's kind about its object when the host restored the object's package
// and computed nothing there (#ss4t5g6): the answer its store holds by the object's objectpath, or no fact's answer
// where the store holds the package computed whole in the kind, and false where it holds neither, a question the host
// would have had to ask, as one only the build asked is. The host's own run asked only what its rules reached, and the
// table answers far more, so an answer the build never asked is never compared.
func (engine *pipeline) restoredAnswer(
	builtFact check.ExportedFact,
	encoder *objectpath.Encoder,
) (check.ExportedFact, bool) {
	answers := engine.dependence.restoredAnswers[builtFact.Package.ID]
	if !builtFact.Answer || builtFact.Object == nil || answers == nil {
		return check.ExportedFact{}, false
	}
	path, pathError := encoder.For(builtFact.Object)
	if pathError != nil {
		return check.ExportedFact{}, false
	}
	encoded, isHeld := answers.Values[builtFact.Analyzer][path]
	if !isHeld {
		if !answers.Complete[builtFact.Analyzer] {
			return check.ExportedFact{}, false
		}
		encoded, isHeld = fact_resolver.ZeroAnswer(builtFact.Analyzer)
		if !isHeld {
			return check.ExportedFact{}, false
		}
	}
	return check.ExportedFact{Package: builtFact.Package, Analyzer: builtFact.Analyzer, FactType: builtFact.FactType,
		Object: builtFact.Object, Digest: cache.DigestOf(encoded), Answer: true, Encoded: encoded}, true
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
