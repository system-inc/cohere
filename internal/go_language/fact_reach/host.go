package fact_reach

import "github.com/system-inc/cohere/internal/go_language/build_reads"

// Host is everything of the host's run that one package's fixed point reads in one build, handed to the build's pass
// on that package: the Source, the standing set the reach leaves, and what the build is asked before and during the
// walk. Every name in it is the host's, a key or an identity, and the build finds its own object for each: the build
// never names an object to the host, since a name it made of its own types could miss the host's and take no body
// out. It is read by one pass, on one goroutine.
type Host interface {
	Source

	// Standing is the bodies the reach leaves to the host in this build as the walk starts, the caller's own copy to
	// take bodies out of, and false for a package the build lints whole, where nothing is asked. It may be empty and
	// true: the build lints all of such a package too, and settles it for an asker that is no body.
	Standing() (map[Place]bool, bool)

	// Places are the package's function bodies in the files the host compiles, in the order they are declared. Those
	// of them not standing as the walk starts, in a file the build compiles too, are the bodies it lints already.
	Places() []Place

	// Functions are the functions whose no-return fact a body's findings can turn on: each the package declares with
	// a body and each function a body or a variable's initializer uses, its own package's or another's, once, with the
	// host's fact.
	Functions() []Function

	// PushedObjects are the objects no function a body declares or names is, whose printf wrapper fact a body's
	// findings can turn on, each a body or a variable's initializer uses once, with the host's fact (PushedObject).
	PushedObjects() []PushedObject

	// Imported are the answers the host's analysis of the package read from other packages, each asked of the build
	// before any body leaves: one that differs names the object it is about, whose users leave.
	Imported() []Question

	// Asking is what the build is asked of the question key names, one of Recorded's or Imported's, and false for a
	// key that is neither.
	Asking(key string) (Asking, bool)

	// OrderBound are the objects whose answers on the host hang on the order it walked the package in, a build's
	// order being another (#h0k644s, the order rule): their users leave before anything is asked.
	OrderBound() []Named

	// Used are the objects the package's bodies' records use, each once, by its key, with the identity the build finds
	// its own object by, in the order of their keys. A rule that reads the bodies naming an object needs those bodies
	// present, so the build judges its own object of each identity it can find and names the key where the rule
	// would read them (standing_presence); an object the build has no own of is never judged.
	Used() []UsedObject

	// Presence is what the body at place holds that a package-wide rule reads and stage 1's record does not, read off
	// the host's own syntax, so the build can keep present what a standing body would need once it is stripped: the
	// zero record for a body that holds none of it, or for a place that is no body's.
	Presence(place Place) PresenceRecord

	// Markers are the method names of every marker interface the host's syntax of the package declares, a one-method
	// unexported interface with nothing embedded, at any level, sorted: a marker only a file the host compiles
	// declares decides the host's verdict on every empty method of its name.
	Markers() []string

	// Kinds are the fact kinds the host answered about the package's objects, sorted by name, each answering about
	// parameters where the host's answers of it are: each a kind a body that leaves reads unknown for where it holds no
	// record of it. A kind the host never answered about the package served no body of it, whatever its judgments read.
	Kinds() []Kind
}

// Function is one function the build's facts are compared on: its key, the identity the build finds its own object
// by, whether the host held the no-return fact of it, and the host's other pushed facts of it (#5vbs1np, must 2), by
// the name of the analyzer that learns them, each in the fact's words, empty for no fact. An analyzer the host never
// ran on the package has no entry, and nothing of it is compared.
type Function struct {
	Object   Object
	Identity build_reads.Identity
	NoReturn bool
	Pushed   map[string]string
}

// PushedObject is one object the build compares a pushed fact of and nothing else (#zqx7gke): a method of an
// interface, or a package-level variable or a field, whose type has the shape of a print or printf function. printf
// finds an interface method a wrapper where a method implementing it is one, which an assignment anywhere in the
// package shows, and a variable one where a function literal assigned to it forwards to one, its fact on the variable.
// Neither has a body or a no-return fact of its own, so neither is a Function, and Pushed holds printf's fact alone,
// in the fact's words, empty for no fact, and no entry where the host never ran printf on the package.
//
// Local is whether the object is a variable or a field of another package than the one whose bodies use it (seats'
// review-1 on #zqx7gke, must 2). printf finds a function literal this package assigns to such a variable a wrapper as
// it finds one assigned to its own, and its calls here read that kind, but it exports a fact only on its own
// package's objects, so the kind is this package's alone and in no build's facts: two builds whose files assign it
// differently, or whose literal forwards to callees of differing kinds, hold the same facts of it. So a build names
// each such object "local printf wrapper", in every build and whatever its facts read, and takes its users out. That
// covers every kind printf holds that no fact carries, by rules/vet/printf's code:
//   - propagate (wrapper_search.go:229-237) is the one place a kind is set that is neither a fact's nor a name's
//     (wrapper_kinds.go:28-52 keeps only those), and it exports a fact wherever the candidate is the package's own.
//   - A candidate of another package comes only from gather's function literal branch, through assignedVariable
//     (wrapper_search.go:139-167): an imported variable, `pkg.Logf = func...` (:162-164), or a field the selection
//     reaches, `x.Logf = func...` (:156-160), a field of another package's type, one promoted through an embedded
//     one among them, which the record names by the selected field itself.
//   - A method of another package's interface, gathered from an interface of the package's own syntax that embeds
//     it, is never given a kind: a candidate with no body is forwarded only from the implementations
//     methodImplementations finds (wrapper_search.go:86-94), which it finds only for the package's own interface
//     methods (:315).
//   - A local variable is the package's own, and its fact is exported as any other.
type PushedObject struct {
	Object   Object
	Identity build_reads.Identity
	Pushed   map[string]string
	Local    bool
}

// UsedObject is one object the package's bodies use: its key, the one Users is asked by, and the identity that names
// it in any build.
type UsedObject struct {
	Object   Object
	Identity build_reads.Identity
}

// PresenceRecord is what one body holds that the presence rows read (standing_presence) and no use of stage 1's
// record says: GoStarts are the keys of the package's functions the body starts with go where testinggoroutine hears
// the statement, and RunStarts those it hands by name to (*testing.T).Run in a _test.go file, each once, in the order
// the body names them; OwnAny is whether the body writes a candidate of consistency-no-any's of its own, an any or an
// interface{} in a local's, a function literal's or a local type's declaration, which a use of interface{} never
// records; and LocalMarkers are the method names of the one-method unexported interfaces the body declares, sorted.
type PresenceRecord struct {
	GoStarts     []Object
	RunStarts    []Object
	OwnAny       bool
	LocalMarkers []string
}

// Asking is one question as the build is asked it: the fact kind's name, the function the answer is of, or that
// declares the parameter it is of, by the identity that names it in any build, the parameter's index among the
// function's, -1 for an answer of the function itself, and the host's answer as its resolver encoded it, which the
// build's is equal to byte for byte or differs from.
type Asking struct {
	Kind      string
	Function  build_reads.Identity
	Parameter int
	Encoded   []byte
}
