// Package fact_reach settles which function bodies of a package an extra build may leave to the host's findings once
// facts are counted (#5vbs1np, stage 4 of #9paaen9). The reach at body grain (#w72zxtq) says which bodies a difference
// in types reaches, and proves nothing about facts: a body no type reaches can still hold a finding that turns on a
// callee's fact or answer, as a call discarded is a finding only where its callee is pure. A standing body's findings
// are the host's only if every fact and answer its rules read is equal in the build, so before any rule runs there the
// build compares what the host held with its own, and every body that uses an object whose fact or answer differs
// leaves the standing set, to a fixed point.
//
// The fixed point cuts off at equality and never closes over callers. A body that left is linted by the build, so each
// answer the host recorded about its function is asked in the build and compared: equal, and nothing above it moves;
// different, or one the build cannot answer, and the function's users leave in turn. That is sound only while every
// answer a standing body's rules read was built from answers that each have a record of their own. A kind's judgment
// can read a callee from the kind's own state and leave no record of it, as triviality walks into one: a body that left
// with no record of a kind may have served another's answer of that kind all the same. So such a body reads unknown
// for every kind Settle is handed that it holds no record of, and its function's users leave (#5vbs1np, must 1). A
// kind that records every judgment, as purity, odd slice and nil-ness do (fact_resolver.Kind.RecordsEveryJudgment,
// #z9fze0t), leaves a record wherever one of its answers rests on a body, so its caller hands Settle no such kind. A
// record is read by the kind's own unit: a kind that answers about each of a function's parameters, as odd slice does,
// is recorded for a parameter only by an answer about that one (round 2's N1). And a place two functions share, as a
// cgo package may declare, holds no record of any kind for either, whatever is recorded there.
//
// This package is the fixed point alone, over places, objects and questions it knows nothing more of. Who fills them,
// from stage 1's record of what each declaration uses and from the host's facts, is the engine's pipeline.
package fact_reach

import "maps"

// Place is a function declaration with a body, by the file a reader sees, past any `//line` directive, and the offset
// of its `func` in the file as parsed: stage 1's rule for a unit (build_dependence's newUnit).
type Place struct {
	File   string
	Offset int
}

// Object names an object two builds are compared on, by a key its source makes equal exactly where the object's
// identity is (build_reads.Identity.Equal). An object with no stable identity has no key, and a body that uses one
// never stands.
type Object string

// Reason is why a body left the standing set, or why an object was named before any answer was asked: a word of its
// source's for the census, which this package only carries.
type Reason string

// Named is an object whose fact or answer is known to differ before any answer is asked, or cannot be trusted alike,
// and why: an import's fact the host read, a function's own no-return fact, an answer that hangs on the order its
// package was walked in.
type Named struct {
	Object Object
	Reason Reason
}

// Question is one answer the host recorded that is asked again in the build once the body it is recorded under is
// linted there: Key names it apart from every other, so one recorded under two places is asked once, Kind is its fact
// kind's name, About is the object whose users leave when the build answers otherwise, the function the answer is of,
// or the function declaring the parameter it is of, and Parameter is that parameter's index among the function's,
// -1 for an answer of the function itself.
type Question struct {
	Key       string
	Kind      string
	About     Object
	Parameter int
}

// Kind is a fact kind as the fixed point reads a body's records of it: its name, and whether it answers about each
// of a function's parameters rather than about the function, so that a function holds a record of it only with one
// for each of its parameters.
type Kind struct {
	Name       string
	Parameters bool
}

// recordUnit is what one record is of: a kind's answer about a function, or about its parameter at an index, -1 for
// the function itself.
type recordUnit struct {
	kind      string
	about     Object
	parameter int
}

// Comparison is how the build's answer to a question reads beside the host's.
type Comparison int

const (
	// Unknown is an answer the build could not give, or could not be sure of, which is taken as differing. It is the
	// zero value, so a comparison nobody made never reads as equal.
	Unknown Comparison = iota
	// Equal is the build's answer the same as the host's.
	Equal
	// Differs is the build's answer another than the host's.
	Differs
)

// Source is the host's side of one package in one build, read only.
type Source interface {
	// Users are the bodies whose own uses hold object, in the order the package's files declare them. A body with a
	// use the record could not follow uses every object.
	Users(object Object) []Place
	// Recorded are the answers the host holds that rest on the function at place: each about it or one of its
	// parameters, and each whose value names it as a function the answer's judgment entered.
	Recorded(place Place) []Question
	// Declared are the functions declared at place, by key: one for a function's body, two where a cgo package
	// declares two at one place, and none for a body no identity names.
	Declared(place Place) []Object
	// Parameters is how many parameters the function declared under key takes.
	Parameters(key Object) int
}

// Removal is one body that left the standing set: the object that took it out, and why that object was named.
type Removal struct {
	Place  Place
	Object Object
	Reason Reason
}

// Settlement is what Settle found: the bodies that still stand, those that left the standing set, in the order they
// left, how many questions the build was asked, how many of them read different or unknown, by kind, and how many
// functions of a body that left held no record of a kind, by kind, each read unknown for it.
type Settlement struct {
	Standing   map[Place]bool
	Removed    []Removal
	Asked      int
	Differing  map[string]int
	Unknown    map[string]int
	Unrecorded map[string]int
}

// DiffersReason is why a body left when an answer of kind, asked in the build, read otherwise than the host's. It and
// UnknownReason are exported so that whoever names an object for an answer it asked before the walk, as the build's
// pass names one for an answer the host read from another package, gives the reason Settle gives.
func DiffersReason(kind string) Reason { return Reason("answer " + kind) }

// UnknownReason is why a body left when an answer of kind, asked in the build, could not be read.
func UnknownReason(kind string) Reason { return Reason("unknown " + kind) }

// UnrecordedReason is why a body left when a function it uses, declared at a body the build lints, holds no record of
// kind, so its answer of kind reads unknown.
func UnrecordedReason(kind string) Reason { return Reason("unrecorded " + kind) }

// Settle takes out of standing every body the facts reach, and reports what still stands and what it took (see the
// package). standing is read and never written: what still stands is the settlement's own map. reached are the bodies
// of files both builds compile that the build lints already, whose recorded answers are asked first; named are the
// objects known to differ before any answer is asked. kinds are the fact kinds a body's findings can turn on, each of
// which a body that left reads unknown for where it holds no record of it, by the kind's unit. compare asks the
// build one question and reads its answer beside the host's; it is called once for each question, in the order the
// bodies leave.
func Settle(
	standing map[Place]bool,
	reached []Place,
	named []Named,
	kinds []Kind,
	source Source,
	compare func(Question) Comparison,
) *Settlement {
	settlement := &Settlement{Standing: maps.Clone(standing), Differing: map[string]int{}, Unknown: map[string]int{},
		Unrecorded: map[string]int{}}
	if settlement.Standing == nil {
		// A clone of no map is none, and what still stands is the settlement's own to answer of either way.
		settlement.Standing = map[Place]bool{}
	}
	asked := map[string]bool{}
	left := append([]Place{}, reached...)
	differing := append([]Named{}, named...)
	// Both lists only grow, and only so far, so the walk ends: a body enters as it leaves the standing set, which it
	// does once, and an object as its source named it, as a question about it fails, or as its body holds no record of
	// a kind, each read once, since a body's questions and kinds are read as it enters.
	nextPlace, nextObject := 0, 0
	for nextPlace < len(left) || nextObject < len(differing) {
		for ; nextObject < len(differing); nextObject++ {
			object := differing[nextObject]
			for _, user := range source.Users(object.Object) {
				if !settlement.Standing[user] {
					continue
				}
				delete(settlement.Standing, user)
				settlement.Removed = append(settlement.Removed,
					Removal{Place: user, Object: object.Object, Reason: object.Reason})
				left = append(left, user)
			}
		}
		for ; nextPlace < len(left); nextPlace++ {
			recorded := map[recordUnit]bool{}
			for _, question := range source.Recorded(left[nextPlace]) {
				recorded[recordUnit{kind: question.Kind, about: question.About, parameter: question.Parameter}] = true
				if asked[question.Key] {
					continue
				}
				asked[question.Key] = true
				settlement.Asked++
				switch compare(question) {
				case Equal:
				case Differs:
					settlement.Differing[question.Kind]++
					differing = append(differing, Named{Object: question.About, Reason: DiffersReason(question.Kind)})
				default:
					settlement.Unknown[question.Kind]++
					differing = append(differing, Named{Object: question.About, Reason: UnknownReason(question.Kind)})
				}
			}
			// After the recorded questions, so a function whose recorded answer differs is named for that first.
			declared := source.Declared(left[nextPlace])
			for _, kind := range kinds {
				for _, object := range declared {
					if len(declared) == 1 && isRecorded(recorded, kind, object, source) {
						continue
					}
					settlement.Unrecorded[kind.Name]++
					differing = append(differing, Named{Object: object, Reason: UnrecordedReason(kind.Name)})
				}
			}
		}
	}
	return settlement
}

// isRecorded is whether recorded holds kind's record of the function declared under object, by the kind's unit: one
// answer about the function, or one about each of its parameters.
func isRecorded(recorded map[recordUnit]bool, kind Kind, object Object, source Source) bool {
	if !kind.Parameters {
		return recorded[recordUnit{kind: kind.Name, about: object, parameter: -1}]
	}
	for index := range source.Parameters(object) {
		if !recorded[recordUnit{kind: kind.Name, about: object, parameter: index}] {
			return false
		}
	}
	return true
}
