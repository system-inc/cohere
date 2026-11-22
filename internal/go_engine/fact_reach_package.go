package go_engine

import (
	"slices"
	"strconv"
	"strings"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
)

// The host's side of the facts' fixed point, stage 4 of #9paaen9 (#5vbs1np): for one package the host checked from
// source, everything a build's pass is handed of the host's run (fact_reach.Host). The bodies that use each object
// come from stage 1's record of what each declaration uses (build_reads.FunctionUses), the answers that rest on each
// body's function, and those about the functions of other packages its bodies and its variables' initializers use,
// from what the host's fact resolver computed (check.ExportedFact). All of that is the same in every build, and is made
// once (factReachPackage, by factReachBuilder); the standing set alone is a build's own (factReachHost). A run makes
// each package's under the body reach census alone, once, as its extra builds start (newFactReachSides), and each
// build hands its passes the side with its own standing set (factReachSides.inBuild).

// factReachSeparator joins the parts of a key. No part can hold it: a NUL byte is in no package's path, no file's
// name, no objectpath, no identifier, no kind's name and no number written out. A file's digest is whatever its
// renderer's caller reads it as, which no run does yet, and must hold none either. So two keys are equal exactly when
// each part of one is the same part of the other.
const factReachSeparator = "\x00"

// factReachOrderBound is why an object whose answers hang on the order the host walked its package in is named
// (fact_reach.Host.OrderBound).
const factReachOrderBound fact_reach.Reason = "order"

// factReachObject is the key of the object identity names, and false for an unknown identity, which names no object
// and so has no key. The key holds every field build_reads.Identity.Equal compares, in one order, each ended by the
// separator none of them can hold, so two keys are equal exactly when their identities are. Never the identity's
// String, which leaves fields out and writes an unknown identity as a word another unknown one would match.
func factReachObject(identity build_reads.Identity) (fact_reach.Object, bool) {
	if identity.Kind == build_reads.IdentityUnknown {
		return "", false
	}
	return fact_reach.Object(strings.Join([]string{string(identity.Kind), identity.PackagePath, string(identity.Path),
		identity.Method, identity.File, strconv.Itoa(identity.Ordinal), identity.Digest}, factReachSeparator)), true
}

// factReachQuestion is the question of the answer of kind about the function function is the key of, or about its
// parameter at index parameter, -1 for the function itself. Its key is the kind, the function's key and a parameter's
// index, joined as an object's key is, so a question the host recorded and one it read of another package are named
// one way, and the same answer met twice is one question.
func factReachQuestion(kind string, function fact_reach.Object, parameter int) fact_reach.Question {
	key := kind + factReachSeparator + string(function)
	if parameter >= 0 {
		key += factReachSeparator + strconv.Itoa(parameter)
	}
	return fact_reach.Question{Key: key, Kind: kind, About: function, Parameter: max(parameter, -1)}
}

// factReachPackage is the host's side of one package for the facts' fixed point, everything of fact_reach.Host that
// is the same in every build. It is made once, on the goroutine that renders its identities, and read only after, so
// every build's pass can share it. What it hands out are places, keys and identities, never an object of the host's,
// which no other build could read.
type factReachPackage struct {
	// places are the package's function bodies, in the order the record declares them.
	places []fact_reach.Place

	// users are, by an object's key, the bodies whose own uses hold the object, in the order the record declares them,
	// and everything the bodies that use every object, each holding a use the record could not follow. A body of
	// everything is in no key's users, so the two never hold one body twice.
	users      map[fact_reach.Object][]fact_reach.Place
	everything []fact_reach.Place

	// initializers are the package's variable initializers, as one asker (factReachInitializers).
	initializers factReachInitializers

	// usedObjects are the objects the bodies' records use, each once, with its identity, in the order of their keys:
	// each key of users, each object a body of everything uses that an identity names, and each the initializers use.
	usedObjects []fact_reach.UsedObject

	// presence is, by a body's place, what the body holds that the presence rows read and no use records
	// (fact_reach.Host.Presence), and no entry for a body that holds none of it.
	presence map[fact_reach.Place]fact_reach.PresenceRecord

	// markers are the method names of the marker interfaces the host's syntax of the package declares, sorted
	// (fact_reach.Host.Markers).
	markers []string

	// functions are the functions a no-return fact is compared on, each once, in the order the record first names
	// them: a function declared with a body where it is declared, then each function its body uses, and each a
	// variable's initializer uses where the initializer is declared.
	functions []fact_reach.Function

	// pushedObjects are the objects only printf's wrapper fact is compared on, each once, in the order the record first
	// names them (fact_reach.Host.PushedObjects).
	pushedObjects []fact_reach.PushedObject

	// recorded are, by a body's place, the questions the host's answers about its function and its parameters make,
	// and imported the questions its answers about the functions of other packages the bodies and the variables'
	// initializers use make. Both are in the order of their keys, since the answers come in a map's order and the fixed
	// point asks, and takes bodies out, in the order it reads them. askings are what a build is asked of each question
	// of either, by the question's key.
	recorded map[fact_reach.Place][]fact_reach.Question
	imported []fact_reach.Question
	askings  map[string]fact_reach.Asking

	// orderBound are the objects whose answers hang on the order the host walked the package in, each named for that.
	orderBound []fact_reach.Named

	// kinds are the fact kinds the host answered about the package's objects, sorted by name, and parameters how many
	// parameters each function declared at a place takes, by its key.
	kinds      []fact_reach.Kind
	parameters map[fact_reach.Object]int

	// shared are the names a reader sees, past any //line directive, that bodies of two parsed files or more are
	// placed in (#e4rd5dw). A place is a name and an offset in the file as parsed, so two bodies of two such files at
	// one offset are one place, which cannot say which of them it is: no body placed in such a name stands
	// (factReachHost.Standing).
	shared map[string]bool

	// declared are, by a body's place, the keys of the functions declared there, which the fixed point names where a
	// body that left holds no record of a kind (fact_reach.Source.Declared), and which the body differential's second
	// leg finds the build's own functions of a standing body by (build_body_differential.go). A cgo package may declare
	// two at one place.
	declared map[fact_reach.Place][]fact_reach.Object

	// unplaced counts what the package's side could not name, each a comparison a build would never make. An answer
	// no body could take: one about the package, a function no declaration of the record declares or one no identity
	// names. A body the file set places in no file, whose answers and uses have nowhere to rest, or a variable's
	// initializer it places in none. An order-bound object no identity names. A function a body starts that no
	// identity names, and a package whose bodies' presence the host never read (factReachBuilder.readPresence). A
	// package that has any is linted whole in every build (factReachHost.Standing), since what cannot be placed cannot
	// be compared. An answer about a variable that is no declared function's parameter is none of these, nor is one its
	// kind marks as its types' alone about a function no declaration declares, and each is left out uncounted
	// (factReachBuilder.answer).
	unplaced int
}

// Users are the bodies whose own uses hold object, in the order the package's files declare them, then the bodies
// that use every object.
func (hosted *factReachPackage) Users(object fact_reach.Object) []fact_reach.Place {
	if len(hosted.everything) == 0 {
		return hosted.users[object]
	}
	return slices.Concat(hosted.users[object], hosted.everything)
}

// Used are the objects the package's bodies use, each once, with its identity, in the order of their keys.
func (hosted *factReachPackage) Used() []fact_reach.UsedObject {
	return hosted.usedObjects
}

// Presence is what the body at place holds that the presence rows read, the zero record for none.
func (hosted *factReachPackage) Presence(place fact_reach.Place) fact_reach.PresenceRecord {
	return hosted.presence[place]
}

// Markers are the method names of the marker interfaces the package declares, sorted.
func (hosted *factReachPackage) Markers() []string {
	return hosted.markers
}

// Recorded are the questions the host's answers about the function at place, and about its parameters, make.
func (hosted *factReachPackage) Recorded(place fact_reach.Place) []fact_reach.Question {
	return hosted.recorded[place]
}

// Places are the package's function bodies, in the order its files declare them.
func (hosted *factReachPackage) Places() []fact_reach.Place {
	return hosted.places
}

// Functions are the functions a body's findings can turn on the no-return fact of, each once, with the host's fact:
// each the package declares with a body and each a body or a variable's initializer uses. A method of an interface is
// none of them, having no body anywhere for a fact to be about, nor is a function no identity names, whose users use
// every object already.
func (hosted *factReachPackage) Functions() []fact_reach.Function {
	return hosted.functions
}

// PushedObjects are the interface methods and the variables of a print or printf function's shape the package's
// bodies and its variables' initializers use, each once, with the host's wrapper fact.
func (hosted *factReachPackage) PushedObjects() []fact_reach.PushedObject {
	return hosted.pushedObjects
}

// Imported are the questions the host's answers about the functions of other packages the package's bodies and its
// variables' initializers use, and about their parameters, make.
func (hosted *factReachPackage) Imported() []fact_reach.Question {
	return hosted.imported
}

// Asking is what a build is asked of the question key names, one of Recorded's or Imported's, and false for a key
// that is neither.
func (hosted *factReachPackage) Asking(key string) (fact_reach.Asking, bool) {
	asking, isAsked := hosted.askings[key]
	return asking, isAsked
}

// OrderBound are the objects whose answers on the host hang on the order it walked the package in.
func (hosted *factReachPackage) OrderBound() []fact_reach.Named {
	return hosted.orderBound
}

// Kinds are the fact kinds the host answered about the package's objects, sorted by name.
func (hosted *factReachPackage) Kinds() []fact_reach.Kind {
	return hosted.kinds
}

// Parameters is how many parameters the function declared under key takes.
func (hosted *factReachPackage) Parameters(key fact_reach.Object) int {
	return hosted.parameters[key]
}

// Declared are the keys of the functions declared at place.
func (hosted *factReachPackage) Declared(place fact_reach.Place) []fact_reach.Object {
	return hosted.declared[place]
}

// inBuild is the package's side in a build whose reach leaves standing to the host, partial saying the build settles
// the package, which it does with standing empty where the package's variable initializers alone are unreached.
func (hosted *factReachPackage) inBuild(standing map[fact_reach.Place]bool, partial bool) fact_reach.Host {
	return factReachHost{factReachPackage: hosted, standing: standing, partial: partial}
}
