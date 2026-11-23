package go_engine

import (
	"cmp"
	"fmt"
	"go/token"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/system-inc/go-types/types"
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
// the file's bytes are the same, so its key is the same in both, as its unit's key is. An object of a package named by
// records (namedByRecords) is keyed by its declaring unit's file and start in place of its own position, beside its
// name (#01chv64): the host's, found by its local name as the records name it, held in local until a build's census
// finds its unit (byDeclaringUnit), and the build's by its position, the unit holding it, byUnit then set. A fact the
// host holds by a method's or a field's path alone has no name, and is left out (pairByUnit).
type factKey struct {
	packageIdentifier string
	file              string
	offset            int
	name              string
	local             string
	byUnit            bool
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

// heldByStandardLibrary reports whether the standard library's graph holds the package of identifier, loaded or by a
// record, whose answers a build's census finds through its units (byDeclaringUnit.answerOf).
func (engine *pipeline) heldByStandardLibrary(identifier string) bool {
	standardLibrary := engine.dependence.standardLibrary
	if standardLibrary == nil || standardLibrary.graph == nil {
		return false
	}
	_, isHeld := standardLibrary.files[identifier]
	return isHeld
}

// namedByRecords reports whether the host restored the package of identifier and the standard library's graph holds
// it by a record the findings store keeps (#6a3kjvz) rather than loaded: its objects have no counterparts there, so
// its facts are keyed by their declaring units on both sides (byDeclaringUnit).
func (engine *pipeline) namedByRecords(identifier string) bool {
	standardLibrary := engine.dependence.standardLibrary
	if !engine.dependence.restored[identifier] || standardLibrary == nil || standardLibrary.graph == nil ||
		standardLibrary.loaded[identifier] {
		return false
	}
	_, isHeld := standardLibrary.files[identifier]
	return isHeld
}

// factKeyByName is the host's fact's key with its object named as a record names it (build_dependence.LocalName),
// beside the object's own name, or the path a fact with no object carries standing for it: a package-level object's
// path is its name alone, and any other path, a method's or a field's, holds a dot and no name, and is keyed with none,
// for the census to leave out (pairByUnit). It is false when nothing names the object.
func factKeyByName(fact check.ExportedFact, encoder *objectpath.Encoder) (factKey, bool) {
	key := factKey{packageIdentifier: fact.Package.ID, offset: -1, analyzer: fact.Analyzer, factType: fact.FactType}
	switch {
	case fact.Object != nil:
		key.local, key.name = build_dependence.LocalName(fact.Object, encoder.For), fact.Object.Name()
	case fact.ObjectPath != "":
		key.local = string(fact.ObjectPath)
		if !strings.Contains(key.local, ".") {
			key.name = key.local
		}
	}
	return key, key.local != ""
}

// indexHostFacts keeps the host's facts by key, for every extra build's to be compared with, when the run judges
// dependence. A fact whose object no build can match is left out. A package the host restored from the system cache's
// table ran no analyzer, but its restored action holds the table's facts and the resolver its answers, about objects
// read from export data, whose positions are no offsets in its files: each is keyed by its counterpart in the standard
// library's graph, loaded from source, so it meets the build's fact about the same object (#ss4t5g6). One with no
// counterpart is left out, as is every fact of a package the host restored that the graph does not hold. A fact the
// table holds about an object export data does not hold has its path alone (check.ExportedFact.ObjectPath), by which
// the graph finds its object. Where the graph holds the package by a record, with no types, there is no counterpart,
// and each fact is keyed by its object's name as the records name it (factKeyByName), counted unkeyed where it has none
// (#01chv64): an objectpath names a method by its place in its type's method set, which another build's files can
// change, so the name finds the declaring unit in the host's own graph, and each build's census meets it there. The
// table's answers of each restored package, restoredAnswers, are kept as they are, for compareFacts to read by
// objectpath.
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
	var encoder objectpath.Encoder
	engine.dependence.factsUnnamed = 0
	for _, fact := range facts {
		key, isKeyed := factKeyOf(fact)
		if engine.namedByRecords(fact.Package.ID) && (fact.Object != nil || fact.ObjectPath != "") {
			key, isKeyed = factKeyByName(fact, &encoder)
			if !isKeyed {
				engine.dependence.factsUnnamed++
			}
		} else if engine.dependence.restored[fact.Package.ID] && (fact.Object != nil || fact.ObjectPath != "") {
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
// every independent unit that calls a dependent one, over both graphs' units, the run's call index (callIndex) read by
// the build's verdicts. A fact of a package named by records is keyed by its object's declaring unit on both sides,
// and left out, counted by why, where pairByUnit cannot pair it exactly, as is a build's answer restoredAnswer cannot
// place. A fact of a package the build restored rather than checked from source is left out on both sides, the build
// having no syntax of it, and counted (contract.DependenceFacts.WithoutSyntax, #wzre5eg): each one the census would
// have compared, and each the build's entry holds by a path alone. A fact about an object in a file the two do not
// both compile, of a package both have by one ID, is no comparison's either, the other build having no such object,
// and is counted on the side that holds it (contract.DependenceFacts.CompiledInOne, #w5dbvdm). Nil when the host's
// facts were never indexed.
func (engine *pipeline) compareFacts(judged *buildJudgment, facts []check.ExportedFact) *contract.DependenceFacts {
	if engine.dependence.facts == nil {
		return nil
	}
	started := time.Now()
	var encoder objectpath.Encoder
	parts := contract.UnkeyedFacts{Unnamed: engine.dependence.factsUnnamed}
	declaring := &byDeclaringUnit{standardLibrary: judged.standardLibrary}
	hostFacts := make(map[factKey]check.ExportedFact, len(engine.dependence.facts))
	hostByName := map[factKey]check.ExportedFact{}
	for key, hosted := range engine.dependence.facts {
		if key.local != "" {
			hostByName[key] = hosted
			continue
		}
		hostFacts[key] = hosted
	}
	built := make(map[factKey]check.ExportedFact, len(facts))
	builtByPosition := map[factKey]check.ExportedFact{}
	withoutSyntax := 0
	// A fact about an object in a file the two builds do not both compile, of a package both have by one ID, which
	// the build checked from source or restored: the build's is passed by here, as it is keyed, and the host's as the
	// host's are walked, each counted where it is passed by.
	compiledInOne := 0
	inOnesFile := func(key factKey) bool {
		return key.file != "" &&
			(judged.sharedPackages[key.packageIdentifier] || judged.withoutSyntax[key.packageIdentifier]) &&
			!judged.shared[sharedFile{packageIdentifier: key.packageIdentifier, file: key.file}]
	}
	for _, fact := range facts {
		// A fact the build's own entry holds by a path alone, of a package it restored, names no object for a key to
		// place, and every such fact of a kind would meet under its package's own key: each is counted here, apart.
		if fact.Object == nil && fact.ObjectPath != "" && judged.withoutSyntax[fact.Package.ID] {
			withoutSyntax++
			continue
		}
		key, isKeyed := factKeyOf(fact)
		if engine.namedByRecords(fact.Package.ID) && fact.Object != nil {
			if !isKeyed {
				parts.Unplaced++
				continue
			}
			// A fact in a file only the build compiles is no comparison's, as compare finds of a positioned one.
			if !judged.shared[sharedFile{packageIdentifier: key.packageIdentifier, file: key.file}] {
				if inOnesFile(key) {
					compiledInOne++
				}
				continue
			}
			// As for a restored package below: an object no objectpath names has no side in the host's.
			if _, pathError := encoder.For(fact.Object); pathError != nil {
				parts.Closures++
				continue
			}
			builtByPosition[key] = fact
			continue
		}
		if !isKeyed {
			continue
		}
		if inOnesFile(key) {
			compiledInOne++
			continue
		}
		// A fact about an object no objectpath names, a function's local closure say, in a package the host restored
		// from the system cache's table, is one no entry can hold, so the host has no side of it to compare with: it is
		// left out, where a host that checked the package from source would have compared it.
		if engine.dependence.restored[fact.Package.ID] && fact.Object != nil {
			if _, pathError := encoder.For(fact.Object); pathError != nil {
				continue
			}
		}
		built[key] = fact
	}
	hostByUnit, builtByUnit, paired := pairByUnit(hostByName, builtByPosition, declaring.hosted, declaring.built)
	maps.Copy(hostFacts, hostByUnit)
	maps.Copy(built, builtByUnit)
	parts.Unplaced += paired.Unplaced
	parts.PathOnly += paired.PathOnly
	parts.Collisions += paired.Collisions
	calls := engine.callIndex()
	kinds := map[[2]string]*contract.FactKindCounts{}
	examples := map[[2]string][]string{}
	differing := map[int]bool{}
	// A fact is compared only of a package both builds have by one ID, in a file both compile.
	comparable := func(key factKey) bool {
		return judged.sharedPackages[key.packageIdentifier] &&
			(key.file == "" || judged.shared[sharedFile{packageIdentifier: key.packageIdentifier, file: key.file}])
	}
	// A fact the census would compare, of a package the build restored: it has no syntax there, so the fact is left
	// out, and counted on the side that holds it.
	leftWithoutSyntax := func(key factKey) bool {
		return judged.withoutSyntax[key.packageIdentifier] &&
			(key.file == "" || judged.shared[sharedFile{packageIdentifier: key.packageIdentifier, file: key.file}])
	}
	compare := func(key factKey, hosted check.ExportedFact, hostHas bool, builtFact check.ExportedFact,
		buildHas bool) {
		if !comparable(key) {
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
			unit, isUnit = calls.UnitContaining(key.packageIdentifier, key.file, key.offset)
		}
		inWorkspace := isUnit && calls.Workspace(unit)
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
	for key, hosted := range hostFacts {
		if leftWithoutSyntax(key) {
			withoutSyntax++
			continue
		}
		if inOnesFile(key) {
			compiledInOne++
			continue
		}
		builtFact, buildHas := built[key]
		compare(key, hosted, true, builtFact, buildHas)
	}
	for key, builtFact := range built {
		if leftWithoutSyntax(key) {
			withoutSyntax++
			continue
		}
		if _, hostHas := hostFacts[key]; !hostHas && comparable(key) {
			hosted, hostHas, isPlaced := engine.restoredAnswer(builtFact, &encoder, declaring)
			if !isPlaced {
				parts.Answers++
				continue
			}
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
	census.Workspace, census.Dependencies = factCallers(calls, judged.dependence, judged.standardLibrary, differing)
	census.Milliseconds = int(time.Since(started).Milliseconds())
	census.Unkeyed, census.UnkeyedParts = unkeyedTotal(parts), parts
	census.WithoutSyntax, census.CompiledInOne = withoutSyntax, compiledInOne
	return census
}

// restoredAnswer is the host's answer of builtFact's kind about its object when the host restored the object's package
// and computed nothing there (#ss4t5g6): the answer its store holds by the object's objectpath, or no fact's answer
// where the store holds the package computed whole in the kind, and false where it holds neither, a question the host
// would have had to ask, as one only the build asked is. The host's own run asked only what its rules reached, and the
// table answers far more, so an answer the build never asked is never compared. In a package the standard library's
// graph holds, the answer is found through the unit declaring the function, never by the build's own objectpath,
// which can name another method where the build's files change a method set (byDeclaringUnit.answerOf, #h660y7d),
// and the third result is false where the function cannot be placed there, whose host side is unknown.
func (engine *pipeline) restoredAnswer(
	builtFact check.ExportedFact,
	encoder *objectpath.Encoder,
	declaring *byDeclaringUnit,
) (check.ExportedFact, bool, bool) {
	answers := engine.dependence.restoredAnswers[builtFact.Package.ID]
	if !builtFact.Answer || builtFact.Object == nil || answers == nil {
		return check.ExportedFact{}, false, true
	}
	var encoded []byte
	isHeld := false
	if engine.heldByStandardLibrary(builtFact.Package.ID) {
		var isPlaced bool
		encoded, isHeld, isPlaced = declaring.answerOf(answers, builtFact)
		if !isPlaced {
			return check.ExportedFact{}, false, false
		}
	} else {
		path, pathError := encoder.For(builtFact.Object)
		if pathError != nil {
			return check.ExportedFact{}, false, true
		}
		encoded, isHeld = answers.Values[builtFact.Analyzer][path]
	}
	if !isHeld {
		if !answers.Complete[builtFact.Analyzer] {
			return check.ExportedFact{}, false, true
		}
		encoded, isHeld = fact_resolver.ZeroAnswer(builtFact.Analyzer)
		if !isHeld {
			return check.ExportedFact{}, false, true
		}
	}
	return check.ExportedFact{Package: builtFact.Package, Analyzer: builtFact.Analyzer, FactType: builtFact.FactType,
		Object: builtFact.Object, Digest: cache.DigestOf(encoded), Answer: true, Encoded: encoded}, true, true
}

// factExample names one differing fact: its package, its object and both builds' values, or that one build has none.
// The object is named by its own position, the build's where the fact is keyed by its declaring unit and the build has
// it, and otherwise by the unit's, said so, since the host's fact of a package named by records has no position.
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
	switch {
	case key.byUnit && buildHas && builtFact.Object != nil:
		if positioned, isPositioned := factKeyOf(builtFact); isPositioned {
			object = fmt.Sprintf("%s at %s:%d", positioned.name, positioned.file, positioned.offset)
			break
		}
		object = fmt.Sprintf("%s in the unit at %s:%d", key.name, key.file, key.offset)
	case key.byUnit:
		object = fmt.Sprintf("%s in the unit at %s:%d", key.name, key.file, key.offset)
	case key.file != "":
		object = fmt.Sprintf("%s at %s:%d", key.name, key.file, key.offset)
	}
	return fmt.Sprintf("%s, %s: host %s, build %s", key.packageIdentifier, object, value(hosted, hostHas),
		value(builtFact, buildHas))
}

// factCallers counts, in the workspace and in the dependencies by each unit's own scope, the units differing names
// and what they reach (contract.FactCallers): first the independent units calling one of them, then the fixed point
// that takes each such caller as differing in turn, and every independent unit that calls a dependent one. Each unit's
// verdict is the build's, host its judgment of the host's graph and standardLibrary of the standard library's, nil
// when calls holds no such graph; everything else is the run's, which every build shares (#f9br7sg).
func factCallers(
	calls *build_dependence.CallIndex,
	host *build_dependence.Dependence,
	standardLibrary *build_dependence.Dependence,
	differing map[int]bool,
) (contract.FactCallers, contract.FactCallers) {
	firstOrder := map[int]bool{}
	for unit := range calls.UnitCount() {
		if calls.Dependent(host, standardLibrary, unit) {
			continue
		}
		if slices.ContainsFunc(calls.Calls(unit), func(callee int32) bool { return differing[int(callee)] }) {
			firstOrder[unit] = true
		}
	}
	bound := map[int]bool{}
	reached := maps.Clone(differing)
	pending := slices.Collect(maps.Keys(differing))
	for len(pending) > 0 {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		for _, callerIndex := range calls.Callers(current) {
			caller := int(callerIndex)
			if calls.Dependent(host, standardLibrary, caller) || bound[caller] {
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
	for unit := range calls.UnitCount() {
		scope := &dependencies
		if calls.Workspace(unit) {
			scope = &workspace
		}
		nodes := calls.Nodes(unit)
		if differing[unit] {
			scope.DifferingUnits++
			scope.DifferingNodes += nodes
		}
		if firstOrder[unit] {
			scope.FirstOrderUnits++
			scope.FirstOrderNodes += nodes
		}
		if bound[unit] {
			scope.BoundUnits++
			scope.BoundNodes += nodes
		}
		callsDependent := slices.ContainsFunc(calls.Calls(unit), func(callee int32) bool {
			return calls.Dependent(host, standardLibrary, int(callee))
		})
		if !calls.Dependent(host, standardLibrary, unit) && callsDependent {
			scope.CallerUnits++
			scope.CallerNodes += nodes
		}
	}
	return workspace, dependencies
}
