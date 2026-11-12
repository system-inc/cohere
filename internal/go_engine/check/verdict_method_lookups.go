package check

import (
	"bytes"
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
// (TestOnlyTheRulesReadForOneBitRecordMethodOrInterfaceLookups). The judgment is one, methodLookupHolds, over what
// either verdict holds of a lookup (methodJudgment): the reads of this run (methodJudgmentOf) and those an entry
// persisted (persistedMethodJudgment), so the two verdicts agree on every lookup.

// methodJudgment is what methodLookupHolds reads of a Method or Interface lookup, from the reads of this run or from
// those an entry persisted: its query and name, whether the host's rule found an answer, the subject's method shape,
// whether its receiver is an interface and whether it embeds another package's type at any depth, and each side's
// candidates by their package's path and name, a package the build restored from the host giving the host's. answer
// is the host's answer as the build finds it, when the host found one.
type methodJudgment struct {
	query             string
	name              string
	found             bool
	subjectIsMethod   bool
	subjectShape      signatureShape
	receiverInterface bool
	embedsForeign     bool
	host              map[string]methodCandidate
	build             map[string]methodCandidate
	answer            answerInBuild
}

// answerInBuild is a lookup's answer as the build finds it: absent when the host found one but recorded none to ask,
// its name, as a reason names it, whether it is a method, the digest of its own lines on the host (answerLinesOf),
// whether it is the host's own object in the build, of a package the build restored from the host, and otherwise the
// build's counterpart, nil with why there is none.
type answerInBuild struct {
	absent      bool
	name        string
	isMethod    bool
	hostLines   []byte
	restored    bool
	counterpart types.Object
	missing     string
}

// methodLookupHolds is the judgment of a Method or Interface lookup, named name, by the one bit its rules read. It
// reports false for decided when the lookup keeps the closure judgment instead (see the cases above).
func methodLookupHolds(name string, judgment methodJudgment) (holds bool, why string, decided bool) {
	if !judgment.subjectIsMethod || judgment.embedsForeign {
		return false, "", false
	}
	if judgment.query == build_reads.QueryMethod {
		for _, side := range []map[string]methodCandidate{judgment.host, judgment.build} {
			for _, candidate := range side {
				if candidate.isGeneric && candidate.shape == judgment.subjectShape {
					return false, "", false
				}
			}
		}
	}
	if judgment.found {
		holds, why = answerStillFixes(name, judgment)
		return holds, why, true
	}
	keys := make([]string, 0, len(judgment.build))
	for key := range judgment.build {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		candidate := judgment.build[key]
		hostCandidate, onHost := judgment.host[key]
		if onHost && hostCandidate.sameLines(candidate) || candidate.shape != judgment.subjectShape {
			continue
		}
		change := "new"
		if onHost {
			change = "changed"
		}
		return false, name + ": " + key + "." + judgment.name + " is " + change + " in the build, of the subject's " +
			"shape", true
	}
	return true, "", true
}

// answerStillFixes reports whether the answer a rule found on the host still fixes the subject's name in the build. An
// answer of a package the build restored from the host is the host's own object there, as the closure judgment gives a
// restored package's type names: its types were read from export data, which leaves out an unexported type nothing
// exported reaches, while its source and its reads are the host's. That rests on the package's own restore having
// held: the load decides it once its imports are done, before any package importing it, the subject's among them, and
// a decline at check time loads every package above it again, never restored (decideHostRestores). Any other answer
// must have a counterpart in the build, at its objectpath or, for the predeclared error, itself (counterpart), and
// render alike by its own lines; for an interface's method a concrete subject must implement, so must every interface
// of the host's closure whose method set holds it, since Implements reads the whole interface.
func answerStillFixes(name string, judgment methodJudgment) (bool, string) {
	answer := judgment.answer
	switch {
	case answer.absent:
		return false, name + ": it was found on the host with no answer to ask of the build"
	case answer.restored:
		return true, ""
	}
	if answer.counterpart == nil {
		return false, name + ": its answer " + answer.name + " is not in the build: " + answer.missing
	}
	buildLines := answerLinesOf(answer.counterpart, ReadsRendererVersion)
	_, buildIsMethod := answer.counterpart.(*types.Func)
	switch {
	case !answer.isMethod && (buildIsMethod || buildLines == nil || !bytes.Equal(buildLines, answer.hostLines)):
		return false, name + ": its answer " + answer.name + " has other methods in the build"
	case answer.isMethod && (!buildIsMethod || !bytes.Equal(buildLines, answer.hostLines)):
		return false, name + ": its answer " + answer.name + " has another signature in the build"
	}
	if !answer.isMethod || judgment.receiverInterface {
		return true, ""
	}
	keys := make([]string, 0, len(judgment.host))
	for key := range judgment.host {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		candidate := judgment.host[key]
		if !candidate.holdsAnswer {
			continue
		}
		if buildCandidate, inBuild := judgment.build[key]; !inBuild || !buildCandidate.sameLines(candidate) {
			return false, name + ": " + key + ", whose method is its answer, has other methods in the build"
		}
	}
	return true, ""
}

// methodJudgmentOf is what methodLookupHolds reads of lookup, a lookup of this run, over each side's type names with a
// method of its name, a package the build restored from the host giving the host's.
func (judged verdict) methodJudgmentOf(
	lookup build_reads.Lookup,
	hostTypeNames []*types.TypeName,
	buildTypeNames []*types.TypeName,
) methodJudgment {
	judgment := methodJudgment{query: lookup.Query, name: lookup.Name, found: lookup.Found,
		host:  methodCandidatesOf(hostTypeNames, lookup.Query, lookup.Name, lookup.Answer),
		build: methodCandidatesOf(buildTypeNames, lookup.Query, lookup.Name, nil)}
	subject, isMethod := lookup.Subject.(*types.Func)
	if !isMethod {
		return judgment
	}
	judgment.subjectIsMethod, judgment.subjectShape = true, shapeOf(subject.Signature())
	judgment.receiverInterface = receiverIsInterface(subject)
	judgment.embedsForeign = receiverEmbedsForeign(subject)
	switch answer := lookup.Answer; {
	case !lookup.Found:
	case answer == nil:
		judgment.answer.absent = true
	default:
		_, isMethod := answer.(*types.Func)
		judgment.answer = answerInBuild{name: objectName(answer), isMethod: isMethod,
			hostLines: answerLinesOf(answer, ReadsRendererVersion),
			restored:  answer.Pkg() != nil && judged.restored[judged.closure[answer.Pkg().Path()]]}
		if !judgment.answer.restored {
			judgment.answer.counterpart, judgment.answer.missing = judged.counterpart(answer)
		}
	}
	return judgment
}
