package check

import (
	"bytes"
	"errors"
	"fmt"
	"go/types"
	"reflect"
	"slices"
	"strings"

	"golang.org/x/tools/go/types/objectpath"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/cache"
)

// The verdict on persisted reads (#b2t9xmf). A package the host restored rather than checked has no reads of this run,
// only those its entry persisted (persistReads): each names what it read by its owner's path and objectpath, and holds
// the digest of the host's rendering, made by the binary that wrote it. The build's side is judged by the very code
// that judges a read of this run (find, closureDigest, useDigest, buildAnswer, buildCandidates, methodLookupHolds,
// buildFactHolds, buildDeprecationHolds), its digest mixed with this binary's renderer version, and every reason is the
// one the same read of this run would give, so the two verdicts agree on every package.

// errorUnencodable is a persisted fact whose value did not digest on the host.
var errorUnencodable = errors.New("the host's value did not digest")

// persistedTypesHold is TypesHold of restoring's persisted reads: the same reads in the same order, with the same
// reasons, a package with assembly files judged by the build memo's whole twin.
func persistedTypesHold(
	restoring Restoring,
	closure map[string]*types.Package,
	restored map[*types.Package]bool,
	build *ReadsBuild,
) (bool, string) {
	held := restoring.persisted
	if why := held.typesDeclined(build); why != "" {
		return false, why
	}
	judgedBuild := build
	if held.record.Assembly {
		judgedBuild = build.wholeTwin()
	}
	judged := verdict{build: judgedBuild, closure: closure, restored: restored, methods: &verdictClosures{}}
	for _, use := range held.record.Uses {
		if holds, why := judged.persistedUseHolds(held, use); !holds {
			return false, "T " + why
		}
	}
	for _, selection := range held.record.Selections {
		if holds, why := judged.persistedSelectionHolds(held, selection); !holds {
			return false, "T " + why
		}
	}
	for _, lookup := range held.record.Lookups {
		if lookup.Query == build_reads.QueryDeprecated {
			continue
		}
		if holds, why := judged.persistedLookupHolds(held, lookup, restoring.Imports); !holds {
			return false, "L " + why
		}
	}
	return true, ""
}

// persistedObjectHolds is objectHolds of the host's object reference names in held: in the build at the same place,
// by its owner's path and objectpath (find), and rendered there to the digest the host's rendering persisted.
func (judged verdict) persistedObjectHolds(held *heldReads, reference int) (bool, string) {
	stored := held.object(reference)
	name := held.objectName(reference)
	owner := held.owner(stored.Owner)
	// unsafe's objects are the type checker's own, and the universe's, of no package, the same in every build
	// (counterpart).
	if stored.Owner == 0 || owner == types.Unsafe.Path() {
		return true, ""
	}
	if stored.Path == "" {
		return false, "object " + name + ": object " + name + " has no objectpath"
	}
	found, why := judged.find(owner, stored.Path)
	if found == nil {
		return false, "object " + name + ": " + why
	}
	if !bytes.Equal(stored.Digest, persistedDigest(ReadsRendererVersion, judged.build.closureDigest(found))) {
		return false, "object " + name + " renders differently"
	}
	return true, ""
}

// persistedUseHolds is useHolds of a persisted use (#mjdf7ah): a whole use by the object's closure digest
// (persistedObjectHolds), and every other kind by the digest of what the kind reads of the build's object at the
// host's object's place, against the one the host's persisted. A use of a package judged by closure alone was
// persisted as a use of the whole.
func (judged verdict) persistedUseHolds(held *heldReads, use persistedUse) (bool, string) {
	if use.Kind == build_reads.UseWhole {
		return judged.persistedObjectHolds(held, use.Object)
	}
	stored := held.object(use.Object)
	name := held.objectName(use.Object)
	owner := held.owner(stored.Owner)
	// unsafe's objects are the type checker's own, and the universe's, of no package, the same in every build, and
	// read alike (counterpart).
	if stored.Owner == 0 || owner == types.Unsafe.Path() {
		return true, ""
	}
	if stored.Path == "" {
		return false, "object " + name + ": object " + name + " has no objectpath"
	}
	found, why := judged.find(owner, stored.Path)
	if found == nil {
		return false, "object " + name + ": " + why
	}
	if !bytes.Equal(use.Digest, persistedDigest(ReadsRendererVersion, judged.build.useDigest(found, use.Kind))) {
		return false, "object " + name + ": " + useDiffers[use.Kind]
	}
	return true, ""
}

// persistedSelectionHolds is selectionHolds of a persisted selection: the build's lookup of its name from the build's
// type at the receiver's place finds a member at the host's index path, whose own line digests to the host's member's.
// A selection of a package judged by closure alone is judged by its receiver's and its member's closure digests.
func (judged verdict) persistedSelectionHolds(held *heldReads, selection persistedSelection) (bool, string) {
	if held.record.ClosureOnly {
		if holds, why := judged.persistedObjectHolds(held, selection.Receiver); !holds {
			return false, why
		}
		return judged.persistedObjectHolds(held, selection.Selected)
	}
	receiver := held.object(selection.Receiver)
	receiverName := held.objectName(selection.Receiver)
	name := "selection " + receiverName + "." + selection.Name
	if receiver.Path == "" {
		return false, name + ": object " + receiverName + " has no objectpath"
	}
	found, why := judged.find(held.owner(receiver.Owner), receiver.Path)
	if found == nil {
		return false, name + ": " + why
	}
	// A lookup of an unexported name finds it only in its own package, the build's of the member's path.
	var namedIn *types.Package
	if member := held.object(selection.Selected); member.Owner != 0 {
		namedIn = judged.closure[held.owner(member.Owner)]
	}
	selected, index, _ := types.LookupFieldOrMethod(found.Type(), true, namedIn, selection.Name)
	switch {
	case selected == nil:
		return false, name + ": the build finds no one member of the name"
	case !slices.Equal(index, selection.Index):
		return false, name + fmt.Sprintf(": the build finds it at %v, the host at %v", index, selection.Index)
	case !bytes.Equal(selection.Digest, persistedDigest(ReadsRendererVersion,
		judged.build.useDigest(declaredOrigin(selected), build_reads.UseMember))):
		return false, name + ": the member it selects differs"
	}
	return true, ""
}

// persistedLookupHolds is lookupHolds of a persisted lookup, asked of the build from imports, the judged package's.
func (judged verdict) persistedLookupHolds(held *heldReads, lookup persistedLookup, imports []string) (bool, string) {
	path := held.owner(lookup.Path)
	name := "lookup " + lookup.Query + " " + strings.Trim(path+"."+lookup.Name, ".")
	roots, why := judged.importRoots(imports)
	if why != "" {
		return false, name + ": " + why
	}
	switch lookup.Query {
	case build_reads.QueryScope, build_reads.QueryClosure:
		answer, why := judged.buildAnswer(lookup.Query, path, lookup.Name, roots)
		if why != "" {
			return false, name + ": " + why
		}
		return answerHolds(name, lookup.Found, answer, func(answer types.Object) bool {
			return lookup.Answer != 0 && judged.samePersisted(held, lookup.Answer, answer)
		})
	case build_reads.QueryPackageName:
		return packageNameHolds(name, lookup.Found, roots, lookup.Name)
	case build_reads.QueryInterface, build_reads.QueryMethod:
		if lookup.NoSubject {
			return false, name + ": it has no subject to find the host's closure by"
		}
		judgment := judged.persistedMethodJudgment(held, lookup, roots)
		if holds, why, decided := methodLookupHolds(name, judgment); decided {
			return holds, why
		}
		return judged.persistedCandidatesHold(held, lookup, name, roots)
	default:
		return false, name + ": a lookup of this kind cannot be asked again"
	}
}

// persistedConcreteSubject reports whether a persisted lookup's subject is a function or a concrete type's method, as
// concreteSubject tells one of this run.
func persistedConcreteSubject(lookup persistedLookup) bool {
	return lookup.SubjectIsMethod && !lookup.ReceiverInterface
}

// persistedCandidate is one type name of the build's side of an Interface or Method lookup: a host candidate the
// record persisted, by reference to its objects, for a package the build restored from the host's run, which gives the
// host's own type names, or else one of the build's own.
type persistedCandidate struct {
	host   int
	object *types.TypeName
	path   string
	name   string
}

// persistedCandidatesHold is the Interface and Method half of lookupHolds on a persisted lookup, named name, asked of
// the build's import closure from roots. The host's side is the lookup's candidates, sorted when persisted. The
// build's side is, as for a lookup of this run, the host's candidates of each package the build restored from the
// host, and the build's own type names of every other package of its closure. A restored package of which the host
// had no candidate gives the build's own type names: none, when the host's closure holds the package, since a restored
// package's types are read from the host's export data, which holds fewer type names than the host's check from
// source declared and never another, so the build finds none where the host found none, as the lookup of this run
// finds none by skipping it; and those it has when the host's closure does not hold the package, as the lookup of this
// run takes them.
func (judged verdict) persistedCandidatesHold(
	held *heldReads,
	lookup persistedLookup,
	name string,
	roots []*types.Package,
) (bool, string) {
	buildClosure := judged.buildClosure(roots)
	hostOwners := map[string]bool{}
	var buildSide []persistedCandidate
	for _, reference := range lookup.Candidates {
		stored := held.object(reference)
		owner := held.owner(stored.Owner)
		hostOwners[owner] = true
		if judged.restored[buildClosure.byPath[owner]] {
			buildSide = append(buildSide, persistedCandidate{host: reference, path: owner,
				name: held.bareName(reference)})
		}
	}
	for _, typeName := range countedCandidates(judged.buildCandidates(lookup.Name, roots,
		func(owner *types.Package) bool { return hostOwners[owner.Path()] }), lookup.Name,
		persistedConcreteSubject(lookup)) {
		buildSide = append(buildSide, persistedCandidate{object: typeName, path: typeName.Pkg().Path(),
			name: typeName.Name()})
	}
	// Sorted as sortTypeNames sorts a lookup of this run's, by package path and name.
	slices.SortFunc(buildSide, func(left, right persistedCandidate) int {
		if order := strings.Compare(left.path, right.path); order != 0 {
			return order
		}
		return strings.Compare(left.name, right.name)
	})
	if len(lookup.Candidates) != len(buildSide) {
		return false, name + fmt.Sprintf(": %d type names have the method on the host, %d in the build",
			len(lookup.Candidates), len(buildSide))
	}
	for index, reference := range lookup.Candidates {
		candidate := buildSide[index]
		same := candidate.host == reference
		if candidate.host == 0 {
			same = judged.samePersisted(held, reference, candidate.object)
		}
		if !same {
			return false, name + ": " + held.objectName(reference) + " differs in the build"
		}
	}
	return true, ""
}

// persistedMethodJudgment is what methodLookupHolds reads of a persisted Interface or Method lookup, as
// methodJudgmentOf reads it of a lookup of this run: the host's side is the candidates the record persisted, and the
// build's, as persistedCandidatesHold gathers it, the host's candidates of each package the build restored from the
// host and the build's own of every other package of its closure from roots. The answer is found in the build at its
// owner's path and objectpath (find), the universe's by its name, as counterpart finds one of this run.
func (judged verdict) persistedMethodJudgment(
	held *heldReads,
	lookup persistedLookup,
	roots []*types.Package,
) methodJudgment {
	judgment := methodJudgment{query: lookup.Query, name: lookup.Name, found: lookup.Found,
		subjectIsMethod: lookup.SubjectIsMethod, subjectShape: shapeOfPersisted(lookup.SubjectShape),
		receiverInterface: lookup.ReceiverInterface, embedsForeign: lookup.EmbedsForeign,
		instantiates: lookup.Instantiates, host: map[string]methodCandidate{}, build: map[string]methodCandidate{}}
	buildClosure := judged.buildClosure(roots)
	hostOwners := map[string]bool{}
	for _, reference := range lookup.Candidates {
		hostOwners[held.owner(held.object(reference).Owner)] = true
	}
	for _, persisted := range lookup.RuleCandidates {
		candidate := methodCandidate{isInterface: persisted.Interface, isGeneric: persisted.Generic,
			shape: shapeOfPersisted(persisted.Shape), lines: persisted.Lines, holdsAnswer: persisted.HoldsAnswer}
		key := held.objectName(persisted.TypeName)
		judgment.host[key] = candidate
		if judged.restored[buildClosure.byPath[held.owner(held.object(persisted.TypeName).Owner)]] {
			judgment.build[key] = candidate
		}
	}
	for _, typeName := range countedCandidates(judged.buildCandidates(lookup.Name, roots,
		func(owner *types.Package) bool { return hostOwners[owner.Path()] }), lookup.Name,
		persistedConcreteSubject(lookup)) {
		if candidate, isCandidate := methodCandidateOf(typeName, lookup.Query, lookup.Name, nil,
			ReadsRendererVersion); isCandidate {
			judgment.build[objectName(typeName)] = candidate
		}
	}
	switch {
	case !lookup.Found:
	case lookup.Answer == 0:
		judgment.answer.absent = true
	default:
		stored := held.object(lookup.Answer)
		owner := held.owner(stored.Owner)
		judgment.answer = answerInBuild{name: held.objectName(lookup.Answer), isMethod: lookup.AnswerIsMethod,
			hostLines: lookup.AnswerLines, restored: stored.Owner != 0 && judged.restored[judged.closure[owner]]}
		switch {
		case judgment.answer.restored:
		case stored.Owner == 0:
			judgment.answer.counterpart = types.Universe.Lookup(held.bareName(lookup.Answer))
		case stored.Path == "":
			judgment.answer.missing = "object " + held.objectName(lookup.Answer) + " has no objectpath"
		default:
			judgment.answer.counterpart, judgment.answer.missing = judged.find(owner, stored.Path)
		}
	}
	return judgment
}

// shapeOfPersisted is a persisted shape as the judgment reads one.
func shapeOfPersisted(shape persistedShape) signatureShape {
	return signatureShape{parameters: shape.Parameters, results: shape.Results, variadic: shape.Variadic}
}

// samePersisted is sameObject of the host's object reference names in held and buildObject, the build's: one object
// in each, of one package path at one objectpath, the build's rendered to the digest the host's persisted. An object of
// no package, or of unsafe, is the type checker's own, which a lookup of this run finds the same object in both.
func (judged verdict) samePersisted(held *heldReads, reference int, buildObject types.Object) bool {
	stored := held.object(reference)
	owner := held.owner(stored.Owner)
	switch {
	case stored.Owner == 0:
		return types.Universe.Lookup(held.bareName(reference)) == buildObject
	case owner == types.Unsafe.Path() && stored.Path != "":
		if own, findError := objectpath.Object(types.Unsafe, stored.Path); findError == nil && own == buildObject {
			return true
		}
	}
	if buildObject.Pkg() == nil || buildObject.Pkg().Path() != owner || stored.Path == "" {
		return false
	}
	buildPath, hasPath := judged.build.pathOf(buildObject)
	if !hasPath || buildPath != stored.Path {
		return false
	}
	return bytes.Equal(stored.Digest, persistedDigest(ReadsRendererVersion, judged.build.closureDigest(buildObject)))
}

// persistedFactsHold is FactsHold of restoring's persisted reads: its fact reads of the types among factTypes, and its
// deprecation lookups when the deprecated fact is among them, in the same order, with the same reasons. A fact read of
// a type no analyzer of the build learns can never be asked, and declines whatever factTypes holds.
func persistedFactsHold(
	restoring Restoring,
	closure map[string]*types.Package,
	build *ReadsBuild,
	fact FactLookup,
	factTypes map[reflect.Type]bool,
) (bool, string) {
	held := restoring.persisted
	if why := held.declined(); why != "" {
		return false, why
	}
	judged := verdict{build: build, closure: closure, fact: fact}
	mixed := func(digest cache.Key) cache.Key { return persistedKey(ReadsRendererVersion, digest) }
	for _, read := range held.record.Facts {
		owner := held.owner(read.Owner)
		named := owner
		if read.Object != 0 {
			named = held.objectName(read.Object)
		}
		factType := held.factTypes[read.FactType]
		if factType == nil {
			return false, "F fact " + held.record.FactTypes[read.FactType] + " of " + named +
				": the build learns no fact of the type"
		}
		if !factTypes[factType] {
			continue
		}
		var path objectpath.Path
		if read.Object != 0 {
			path = held.object(read.Object).Path
			if path == "" {
				return false, "F fact " + factType.String() + " of " + named + ": the object has no objectpath"
			}
		}
		hostValue := func() (cache.Key, error) {
			if read.Unencodable || len(read.Value) != len(cache.Key{}) {
				return cache.Key{}, errorUnencodable
			}
			return cache.Key(read.Value), nil
		}
		if holds, why := judged.buildFactHolds("fact "+factType.String()+" of "+named, owner, path, factType,
			read.Found, hostValue, mixed); !holds {
			return false, "F " + why
		}
	}
	if !factTypes[deprecatedFactType] {
		return true, ""
	}
	for _, lookup := range held.record.Lookups {
		if lookup.Query != build_reads.QueryDeprecated {
			continue
		}
		if holds, why := judged.persistedDeprecationHolds(held, lookup); !holds {
			return false, "D " + why
		}
	}
	return true, ""
}

// persistedDeprecationHolds is deprecationHolds of a persisted Deprecated lookup.
func (judged verdict) persistedDeprecationHolds(held *heldReads, lookup persistedLookup) (bool, string) {
	owner := held.owner(lookup.Path)
	name := "lookup " + lookup.Query + " " + owner
	var path objectpath.Path
	if lookup.Subject != 0 {
		subject := held.object(lookup.Subject)
		// A field of an instance has no deprecation in any build (deprecationHolds).
		if subject.Path == "" && lookup.InstanceField && !lookup.Found {
			return true, ""
		}
		if subject.Path == "" {
			return false, name + ": the subject has no objectpath"
		}
		path = subject.Path
		name += " of " + held.objectName(lookup.Subject)
	}
	return judged.buildDeprecationHolds(name, owner, path, lookup.Found, lookup.Text)
}

// persistedFactTypes are the fact types held's fact reads asked, those the build's analyzers learn, and the deprecated
// fact's when it looked up a deprecation (ReadFactTypes).
func persistedFactTypes(held *heldReads) map[reflect.Type]bool {
	asked := map[reflect.Type]bool{}
	// An unreadable record's fact types are never read: its types' verdict declines it first.
	if held.unreadable != "" {
		return asked
	}
	for _, read := range held.record.Facts {
		if factType := held.factTypes[read.FactType]; factType != nil {
			asked[factType] = true
		}
	}
	for _, lookup := range held.record.Lookups {
		if lookup.Query == build_reads.QueryDeprecated {
			asked[deprecatedFactType] = true
		}
	}
	return asked
}
