package fact_reach

import "github.com/system-inc/cohere/internal/go_engine/build_reads"

// Host is everything of the host's run that one package's fixed point reads in one build, handed to the build's pass
// on that package: the Source, the standing set the reach leaves, and what the build is asked before and during the
// walk. Every name in it is the host's, a key or an identity, and the build finds its own object for each: the build
// never names an object to the host, since a name it made of its own types could miss the host's and take no body
// out. It is read by one pass, on one goroutine.
type Host interface {
	Source

	// Standing is the bodies the reach leaves to the host in this build as the walk starts, the caller's own copy to
	// take bodies out of, and false for a package the build lints whole, where nothing is asked.
	Standing() (map[Place]bool, bool)

	// Places are the package's function bodies in the files the host compiles, in the order they are declared. Those
	// of them not standing as the walk starts, in a file the build compiles too, are the bodies it lints already.
	Places() []Place

	// Functions are the functions whose no-return fact a body's findings can turn on: each the package declares with
	// a body and each function a body uses, its own package's or another's, once, with the host's fact.
	Functions() []Function

	// Imported are the answers the host's analysis of the package read from other packages, each asked of the build
	// before any body leaves: one that differs names the object it is about, whose users leave.
	Imported() []Question

	// Asking is what the build is asked of the question key names, one of Recorded's or Imported's, and false for a
	// key that is neither.
	Asking(key string) (Asking, bool)

	// OrderBound are the objects whose answers on the host hang on the order it walked the package in, a build's
	// order being another (#h0k644s, the order rule): their users leave before anything is asked.
	OrderBound() []Named

	// Kinds are the fact kinds the host answered about the package's objects, sorted by name, each answering about
	// parameters where the host's answers of it are: each a kind a body that leaves reads unknown for where it holds no
	// record of it, beside every kind the build registers. A kind the host never answered about the package served no
	// body of it, whatever its judgments read.
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
