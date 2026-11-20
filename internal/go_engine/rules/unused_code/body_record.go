package unused_code

import "encoding/json"

// bodyRecord is what one function declaration's body adds to its package's graph, as the walk of that body found it
// (#xeafx79): the nodes it makes, the use and own edges it adds, and what of the steps after the walk falls on its
// nodes. An extra build that leaves the body to the host has no body to walk, so it splices the host's record in
// where the walk would have been. A record therefore holds observations only, each object named by where its
// declaring identifier is written or by its index among the body's locals: no types.Object, no token.Pos and no
// index into the graph, none of which another build's graph shares.
type bodyRecord struct {
	// Body is the function declaration the body is of, placed at its `func`, as a standing body is keyed
	// (rule.StandingBodies.Stands).
	Body recordedPlace `json:"body"`
	// Locals is every node the body's walk makes, in the order the walk met them. A local is its index here and never
	// a position: a type switch makes one variable a clause, all at one offset, and an unnamed parameter of a function
	// literal has no declaring identifier.
	Locals []recordedLocal `json:"locals"`
	// Uses is every use edge the walk adds while the body is open, each once, in the order the walk added them. An
	// edge another body adds too is in both records, though the graph holds it once.
	Uses []recordedUse `json:"uses"`
	// Rooted is each local the root uses by a step after the walk, a generated file's nodes and the objects a
	// directive ignores, in the order those steps met them. Both steps read one file's bytes, which a standing body's
	// file shares with the host's.
	Rooted []int `json:"rooted"`
	// Files is each file an object declared outside the body is declared in, where the body uses it or it uses a
	// local, and each file whose name the walk read to judge a write to a package-level variable, whether or not the
	// write became a use. Sorted, each once. A build that does not compile one of them as the host did cannot take
	// the record.
	Files []string `json:"files"`
	// Present is why the body cannot be summarized by a record at all, when any of it is set.
	Present recordedPresence `json:"present"`
}

// recordedPlace is a place in the package's source: the file a reader sees, past any `//line` directive, and the
// offset in the file as parsed, the pair a standing body is keyed by.
type recordedPlace struct {
	File   string `json:"file"`
	Offset int    `json:"offset"`
}

// recordedLocal is one node a body's walk makes: what a finding says of it, and what owns it.
type recordedLocal struct {
	// Place is where the local is declared. Several locals may share one, and a local without a declaring identifier
	// is placed where go/types places its object.
	Place recordedPlace `json:"place"`
	// Kind is the word a finding names the local's kind with (kindOf).
	Kind string `json:"kind"`
	// Name is the local's name as a finding words it (objectOf): a method of an interface under a local named type
	// reads `L.m`.
	Name string `json:"name"`
	// Owners is each node that owns the local, in the order the walk found them. `var a, b struct{ x int }` reads the
	// type once a name, so x has two, and a local the walk only ever uses, a blank result of a function literal, has
	// none.
	Owners []recordedEnd `json:"owners"`
}

// recordedUse is one use edge: By uses Used.
type recordedUse struct {
	By   recordedEnd `json:"by"`
	Used recordedEnd `json:"used"`
}

// recordedEnd is one end of a recorded edge, which Kind tells apart: the body's function, a local by Local, its index
// among the record's locals, an object declared outside the body by Place, where its declaring identifier is, or the
// root. Local and Place are zero for the kinds that do not read them.
type recordedEnd struct {
	Kind  endKind       `json:"kind"`
	Local int           `json:"local"`
	Place recordedPlace `json:"place"`
}

// endKind is which of the four things an end of a recorded edge is.
type endKind string

const (
	// endFunction is the function the body is of.
	endFunction endKind = "function"
	// endLocal is a local of the body.
	endLocal endKind = "local"
	// endDeclared is an object declared outside the body: at package level, a field or a method of a package-level
	// type, or a name of the function's own signature.
	endDeclared endKind = "declared"
	// endRoot is the graph's root.
	endRoot endKind = "root"
)

// recordedPresence is the four reasons a body cannot be summarized by its record. Each feeds a step after the walk
// that compares or reads the build's own types, which no record of positions can replay, so a build keeps such a body
// present and walks it itself.
type recordedPresence struct {
	// UnexportedInterfaceMethod is whether the body holds an interface type literal whose method set, with what it
	// embeds, holds an unexported method. Any named type of the package may come to implement it, and use the methods
	// that do. An interface whose methods are all exported adds no use a type's own exported methods have not.
	UnexportedInterfaceMethod bool `json:"unexportedInterfaceMethod"`
	// LocalMethodSet is whether the body declares a named type whose method set, its own or its pointer's, is not
	// empty. It is read from go/types and not from the syntax: `type L T` and `type L I` take methods with no
	// embedded field in the local declaration.
	LocalMethodSet bool `json:"localMethodSet"`
	// InterfaceInstance is whether the body writes an instantiation of a generic interface type of the package,
	// which entry reads from the package's instances whole.
	InterfaceInstance bool `json:"interfaceInstance"`
	// IgnoredType is whether a directive ignores a type name the body declares. useIgnored then uses that type's
	// methods and fields from go/types, which for a local alias of a package-level type are declared outside the
	// body.
	IgnoredType bool `json:"ignoredType"`
}

// encoded is record in its canonical form: JSON, every field in its declared order and no map among them, so one
// record has one encoding and two records of one body compare byte for byte.
func (record bodyRecord) encoded() ([]byte, error) {
	return json.Marshal(record)
}
