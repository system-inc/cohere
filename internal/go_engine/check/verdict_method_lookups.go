package check

import (
	"go/types"
	"slices"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
)

// The three rules that ask a Method or Interface lookup (consistency_no_any, consistency_no_abbreviated_identifier and
// consistency_no_uppercase_initialism) each read one bit of it: whether a method of another package in the import
// closure fixes the subject method's name. Which method fixes it, and how many could, never reaches a finding. So the
// lookup holds in another build when that bit cannot have flipped there (#qqzg1w8):
//
//   - Answered on the host, the bit stays set while the answer is still found and renders alike by its own lines: one
//     match is enough, whatever else the closure gained or lost.
//   - Unanswered on the host, the bit stays clear unless the build gains, or changes, a candidate the rules could
//     match: a declared method of the name on a named type, or one of an interface's method set, an interface alone for
//     Interface, whose signature has the subject's shape. Every rule's match needs that shape at least: types.Identical
//     for no-any and abbreviated, types.Implements for initialism and abbreviated's concrete subjects, whose interface
//     method must be identical to the subject, and the shape fallbacks themselves.
//
// Two cases keep the whole closure judgment, since the bit there can flip with nothing this reads changing:
//
//   - A Method lookup with a generic candidate of the subject's shape on either side: abbreviated instantiates it with
//     type arguments read off the subject's signature and validates their constraints, which can turn on an argument
//     type's method set in the build.
//   - A subject whose receiver embeds another package's type, directly or through types of its own package that embed
//     one: Implements reads the receiver's method set, which holds the methods that type promotes at any depth, and
//     those can differ by build while the lookup's candidates do not.
//
// A lookup only these rules ask is judged so, so a fourth rule that records one must be read first
// (TestOnlyTheRulesReadForOneBitRecordMethodOrInterfaceLookups).

// methodLookupHolds is the judgment of a Method or Interface lookup by the one bit its rules read, over each side's
// type names with a method of the name, a package the build restored from the host giving the host's. It reports
// false for decided when the lookup keeps the closure judgment instead (see the cases above).
func (judged verdict) methodLookupHolds(
	name string,
	lookup build_reads.Lookup,
	hostTypeNames []*types.TypeName,
	buildTypeNames []*types.TypeName,
) (holds bool, why string, decided bool) {
	subject, isMethod := lookup.Subject.(*types.Func)
	if !isMethod || receiverEmbedsForeign(subject) {
		return false, "", false
	}
	host := methodCandidatesOf(hostTypeNames, lookup.Query, lookup.Name)
	build := methodCandidatesOf(buildTypeNames, lookup.Query, lookup.Name)
	shape := shapeOf(subject.Signature())
	if lookup.Query == build_reads.QueryMethod {
		for _, side := range []map[string]methodCandidate{host, build} {
			for _, candidate := range side {
				if candidate.isGeneric && shapeOf(candidate.method.Signature()) == shape {
					return false, "", false
				}
			}
		}
	}
	if lookup.Found {
		holds, why = judged.answerStillFixes(name, lookup, subject, host, build)
		return holds, why, true
	}
	keys := make([]string, 0, len(build))
	for key := range build {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		candidate := build[key]
		hostCandidate, onHost := host[key]
		if onHost && hostCandidate.sameLines(candidate) || shapeOf(candidate.method.Signature()) != shape {
			continue
		}
		change := "new"
		if onHost {
			change = "changed"
		}
		return false, name + ": " + key + "." + lookup.Name + " is " + change + " in the build, of the subject's " +
			"shape", true
	}
	return true, "", true
}

// answerStillFixes reports whether the answer a rule found on the host still fixes the subject's name in the build.
// The predeclared error is the universe's, the same in every build. An answer of a package the build restored from the
// host is the host's own object there, as the closure judgment gives a restored package's type names: its types were
// read from export data, which leaves out an unexported type nothing exported reaches, while its source and its reads
// are the host's. That rests on the package's own restore having held: the load decides it once its imports are done,
// before any package importing it, the subject's among them, and a decline at check time loads every package above it
// again, never restored (decideHostRestores). Any other answer must be found at its objectpath and render
// alike by its own lines; for an interface's method a concrete subject must implement, so must every interface of the
// host's closure whose method set holds it, since Implements reads the whole interface.
func (judged verdict) answerStillFixes(
	name string,
	lookup build_reads.Lookup,
	subject *types.Func,
	host map[string]methodCandidate,
	build map[string]methodCandidate,
) (bool, string) {
	answer := lookup.Answer
	if answer == nil {
		return false, name + ": it was found on the host with no answer to ask of the build"
	}
	if answer.Pkg() == nil || judged.restored[judged.closure[answer.Pkg().Path()]] {
		return true, ""
	}
	counterpart, missing := judged.counterpart(answer)
	if counterpart == nil {
		return false, name + ": its answer " + objectName(answer) + " is not in the build: " + missing
	}
	switch hostAnswer := answer.(type) {
	case *types.TypeName:
		hostInterface, hostIsInterface := hostAnswer.Type().Underlying().(*types.Interface)
		buildInterface, buildIsInterface := counterpart.Type().Underlying().(*types.Interface)
		if !hostIsInterface || !buildIsInterface || interfaceLine(hostInterface) != interfaceLine(buildInterface) {
			return false, name + ": its answer " + objectName(hostAnswer) + " has other methods in the build"
		}
		return true, ""
	case *types.Func:
		buildMethod, isMethod := counterpart.(*types.Func)
		if !isMethod || signatureLine(hostAnswer) != signatureLine(buildMethod) {
			return false, name + ": its answer " + objectName(hostAnswer) + " has another signature in the build"
		}
		if types.IsInterface(subject.Signature().Recv().Type()) {
			return true, ""
		}
		for key, candidate := range host {
			if candidate.interfaceType == nil || !slices.Contains(slices.Collect(candidate.interfaceType.Methods()),
				hostAnswer) {
				continue
			}
			if buildCandidate, inBuild := build[key]; !inBuild || !buildCandidate.sameLines(candidate) {
				return false, name + ": " + key + ", whose method is its answer, has other methods in the build"
			}
		}
		return true, ""
	}
	return false, name + ": its answer " + objectName(answer) + " is neither a type name nor a method"
}
