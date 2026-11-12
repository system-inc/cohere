package check

import (
	"go/types"
	"reflect"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
)

// readsWriter makes one package's persisted reads from the host's memo, host, interning each owner, object and fact
// type once.
type readsWriter struct {
	host      *ReadsBuild
	version   string
	record    persistedReads
	owners    map[string]int
	objects   map[types.Object]int
	factTypes map[reflect.Type]int
}

// persistReads is restoring's reads, a package the host checked this run, as an entry persists them, their digests
// taken by host, the host's memo, under version, the renderer's, and the sizes host measures by, and false for reads
// that can never hold or were never recorded, which no record can promise anything of, and for a memo never measured,
// whose digests render every field whole, which no build's measured memo matches. A package with assembly files is
// digested by the memo's whole twin, as the verdict judges it (TypesHold).
func persistReads(restoring Restoring, host *ReadsBuild, version string) (persistedReads, bool) {
	if restoring.Reads == nil {
		return persistedReads{}, false
	}
	if restorable, _ := restoring.Reads.Restorable(); !restorable {
		return persistedReads{}, false
	}
	wordSize, maximumAlignment, measured := host.measuredSizes()
	if !measured {
		return persistedReads{}, false
	}
	writer := &readsWriter{host: host, version: version, owners: map[string]int{}, objects: map[types.Object]int{},
		factTypes: map[reflect.Type]int{}}
	if restoring.Assembly {
		writer.host = host.wholeTwin()
	}
	closureOnly := restoring.Assembly || restoring.Reads.ClosureOnly() != ""
	writer.record.Renderer, writer.record.Assembly, writer.record.ClosureOnly = version, restoring.Assembly, closureOnly
	writer.record.WordSize, writer.record.MaximumAlignment = wordSize, maximumAlignment
	for _, use := range restoring.Reads.Uses() {
		writer.record.Uses = append(writer.record.Uses, writer.use(use, closureOnly))
	}
	for _, selection := range restoring.Reads.Selections() {
		writer.record.Selections = append(writer.record.Selections, writer.selection(selection, closureOnly))
	}
	for _, fact := range restoring.Reads.Facts() {
		persisted := persistedFact{Owner: writer.owner(fact.Package.Path()), FactType: writer.factType(fact.FactType),
			Found: fact.Found}
		if fact.Object != nil {
			persisted.Object = writer.object(fact.Object, false)
		}
		if fact.Found {
			if digest, digestError := factValueDigest(fact.Value); digestError == nil {
				persisted.Value = persistedDigest(version, digest)
			} else {
				persisted.Unencodable = true
			}
		}
		writer.record.Facts = append(writer.record.Facts, persisted)
	}
	for _, lookup := range restoring.Reads.Lookups() {
		writer.record.Lookups = append(writer.record.Lookups, writer.lookup(lookup))
	}
	return writer.record, true
}

// use is one use as the record persists it, a use of the whole, as the verdict judges every use of a package judged
// by closure alone, when closureOnly: its object, its kind, and for a kind but the whole, its digest of what the kind
// reads, unless the object has no objectpath, which the verdict declines before it would render anything.
func (writer *readsWriter) use(use build_reads.ObjectUse, closureOnly bool) persistedUse {
	kind := use.Kind
	if closureOnly {
		kind = build_reads.UseWhole
	}
	persisted := persistedUse{Object: writer.object(use.Object, kind == build_reads.UseWhole), Kind: kind}
	if kind != build_reads.UseWhole && writer.record.Objects[persisted.Object-1].Path != "" {
		persisted.Digest = persistedDigest(writer.version, writer.host.useDigest(use.Object, kind))
	}
	return persisted
}

// selection is one selection as the record persists it: its receiver and member, each with its closure digest when
// closureOnly, and otherwise the digest of the member's own line, which the build's lookup must find again.
func (writer *readsWriter) selection(selection build_reads.Selection, closureOnly bool) persistedSelection {
	persisted := persistedSelection{Receiver: writer.object(selection.Receiver, closureOnly), Name: selection.Name,
		Index: selection.Index, Selected: writer.object(selection.Selected, closureOnly)}
	if !closureOnly {
		persisted.Digest = persistedDigest(writer.version, writer.host.useDigest(selection.Selected,
			build_reads.UseMember))
	}
	return persisted
}

// lookup is one lookup as the record persists it: a Scope or Closure answer with its digest, an Interface or Method
// lookup's candidates, as lookupHolds gathers the host's, and a Deprecated lookup's subject by its objectpath.
func (writer *readsWriter) lookup(lookup build_reads.Lookup) persistedLookup {
	persisted := persistedLookup{Query: lookup.Query, Name: lookup.Name, Found: lookup.Found, Text: lookup.Text}
	if lookup.Path != "" {
		persisted.Path = writer.owner(lookup.Path)
	}
	switch lookup.Query {
	case build_reads.QueryScope, build_reads.QueryClosure:
		if lookup.Answer != nil {
			persisted.Answer = writer.object(lookup.Answer, true)
		}
	case build_reads.QueryInterface, build_reads.QueryMethod:
		if lookup.Subject == nil || lookup.Subject.Pkg() == nil {
			persisted.NoSubject = true
			return persisted
		}
		hostClosure := gatherClosure(writer.host.indexedClosureOf, lookup.Subject.Pkg().Imports())
		var candidates []*types.TypeName
		for _, indexed := range writer.host.methodsNamed(lookup.Name) {
			if hostClosure.reached[indexed.owner] {
				candidates = append(candidates, indexed.typeNames...)
			}
		}
		sortTypeNames(candidates)
		for _, candidate := range candidates {
			persisted.Candidates = append(persisted.Candidates, writer.object(candidate, true))
		}
		writer.methodJudgment(&persisted, lookup, candidates)
	case build_reads.QueryDeprecated:
		if lookup.Subject != nil {
			persisted.Subject = writer.object(lookup.Subject, false)
			_, hasPath := writer.host.pathOf(lookup.Subject)
			variable, isVariable := lookup.Subject.(*types.Var)
			persisted.InstanceField = !hasPath && isVariable && variable.Origin() != variable
		}
	}
	return persisted
}

// methodJudgment adds to persisted, an Interface or Method lookup with a subject, what the one-bit judgment reads of
// lookup (methodJudgmentOf), from candidates, the host's type names with a method of its name, sorted.
//
// Mutates: persisted gains the answer's own lines, the subject's shape and receiver, and the rule's candidates
func (writer *readsWriter) methodJudgment(
	persisted *persistedLookup,
	lookup build_reads.Lookup,
	candidates []*types.TypeName,
) {
	if lookup.Answer != nil {
		persisted.Answer = writer.object(lookup.Answer, false)
		_, persisted.AnswerIsMethod = lookup.Answer.(*types.Func)
		persisted.AnswerLines = answerLinesOf(lookup.Answer, writer.version)
	}
	if subject, isMethod := lookup.Subject.(*types.Func); isMethod {
		shape := shapeOf(subject.Signature())
		persisted.SubjectIsMethod = true
		persisted.SubjectShape = persistedShape{Parameters: shape.parameters, Results: shape.results,
			Variadic: shape.variadic}
		persisted.ReceiverInterface = receiverIsInterface(subject)
		persisted.EmbedsForeign = receiverEmbedsForeign(subject)
	}
	for _, typeName := range candidates {
		candidate, isCandidate := methodCandidateOf(typeName, lookup.Query, lookup.Name, lookup.Answer,
			writer.version)
		if !isCandidate {
			continue
		}
		persisted.RuleCandidates = append(persisted.RuleCandidates, persistedMethodCandidate{
			TypeName: writer.object(typeName, true), Interface: candidate.isInterface, Generic: candidate.isGeneric,
			Shape: persistedShape{Parameters: candidate.shape.parameters, Results: candidate.shape.results,
				Variadic: candidate.shape.variadic},
			Lines: candidate.lines, HoldsAnswer: candidate.holdsAnswer})
	}
}

// owner is the reference to the package at path in the record's owners, interned the first time it is named.
func (writer *readsWriter) owner(path string) int {
	if reference, known := writer.owners[path]; known {
		return reference
	}
	writer.record.Owners = append(writer.record.Owners, path)
	writer.owners[path] = len(writer.record.Owners)
	return len(writer.record.Owners)
}

// factType is the place of factType in the record's fact types, interned the first time it is named.
func (writer *readsWriter) factType(factType reflect.Type) int {
	if place, known := writer.factTypes[factType]; known {
		return place
	}
	writer.record.FactTypes = append(writer.record.FactTypes, factTypeName(factType))
	writer.factTypes[factType] = len(writer.record.FactTypes) - 1
	return len(writer.record.FactTypes) - 1
}

// object is the reference to object in the record's objects, interned the first time it is named, with its persisted
// closure digest when rendered, the first time a read that compares its rendering names it. An object with no
// objectpath has no digest: the verdict declines it before it would render anything.
func (writer *readsWriter) object(object types.Object, rendered bool) int {
	reference, known := writer.objects[object]
	if !known {
		persisted := persistedObject{}
		if object.Pkg() != nil {
			persisted.Owner = writer.owner(object.Pkg().Path())
		}
		if path, hasPath := writer.host.pathOf(object); hasPath {
			persisted.Path = path
		}
		if object.Name() != string(persisted.Path) {
			persisted.Name = object.Name()
		}
		writer.record.Objects = append(writer.record.Objects, persisted)
		reference = len(writer.record.Objects)
		writer.objects[object] = reference
	}
	stored := &writer.record.Objects[reference-1]
	if rendered && stored.Digest == nil && stored.Path != "" {
		stored.Digest = persistedDigest(writer.version, writer.host.closureDigest(object))
	}
	return reference
}
