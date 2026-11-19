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
// answer a standing body's rules read was built from answers that each have a record of their own, which each fact kind
// holds for itself.
//
// This package is the fixed point alone, over places, objects and questions it knows nothing more of. Who fills them,
// from stage 1's record of what each declaration uses and from the host's facts, is the engine's pipeline.
package fact_reach

import "maps"

// Place is a function declaration with a body, by the file it was parsed from and the offset of its `func`, as
// build_reads.Declaration places one.
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
// kind's name, and About is the object whose users leave when the build answers otherwise, the function the answer is
// of, or the function declaring the parameter it is of.
type Question struct {
	Key   string
	Kind  string
	About Object
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
}

// Removal is one body that left the standing set: the object that took it out, and why that object was named.
type Removal struct {
	Place  Place
	Object Object
	Reason Reason
}

// Settlement is what Settle found: the bodies that still stand, those that left the standing set, in the order they
// left, how many questions the build was asked, and how many of them read different or unknown, by kind.
type Settlement struct {
	Standing  map[Place]bool
	Removed   []Removal
	Asked     int
	Differing map[string]int
	Unknown   map[string]int
}

// DiffersReason is why a body left when an answer of kind, asked in the build, read otherwise than the host's. It and
// UnknownReason are exported so that whoever names an object for an answer it asked before the walk, as the build's
// pass names one for an answer the host read from another package, gives the reason Settle gives.
func DiffersReason(kind string) Reason { return Reason("answer " + kind) }

// UnknownReason is why a body left when an answer of kind, asked in the build, could not be read.
func UnknownReason(kind string) Reason { return Reason("unknown " + kind) }

// Settle takes out of standing every body the facts reach, and reports what still stands and what it took (see the
// package). standing is read and never written: what still stands is the settlement's own map. reached are the bodies
// of files both builds compile that the build lints already, whose recorded answers are asked first; named are the
// objects known to differ before any answer is asked. compare asks the build one question and reads its answer beside
// the host's; it is called once for each question, in the order the bodies leave.
func Settle(
	standing map[Place]bool,
	reached []Place,
	named []Named,
	source Source,
	compare func(Question) Comparison,
) *Settlement {
	settlement := &Settlement{Standing: maps.Clone(standing), Differing: map[string]int{}, Unknown: map[string]int{}}
	if settlement.Standing == nil {
		// A clone of no map is none, and what still stands is the settlement's own to answer of either way.
		settlement.Standing = map[Place]bool{}
	}
	asked := map[string]bool{}
	left := append([]Place{}, reached...)
	differing := append([]Named{}, named...)
	// Both lists only grow, each body and each object entering once, so the walk ends: a body enters as it leaves the
	// standing set, which it does once, and an object as its one question fails or as its source named it.
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
			for _, question := range source.Recorded(left[nextPlace]) {
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
		}
	}
	return settlement
}
