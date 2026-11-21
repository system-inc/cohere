package unused_code

import "encoding/json"

// bodyRecord is what one function declaration's body adds to its package's graph, as the walk of that body found it
// (#xeafx79): the nodes it makes, the use and own edges it adds, and what of the steps after the walk falls on its
// nodes. An extra build that leaves the body to the host has no body to walk, so it splices the host's record in
// where the walk would have been. A record therefore holds observations only, each object named by where its
// declaring identifier is written or by its index among the body's locals: no types.Object, no token.Pos and no
// index into the graph, none of which another build's graph shares.
type bodyRecord struct {
	// Body is the function declaration the body is of, as a standing body is keyed (rule.StandingBodies.Stands): the
	// file a reader sees its `func` in, past any `//line` directive, and the offset of that `func` in the file as
	// parsed. It is the one place of a record that takes a directive's name. Two bodies of a package can share a key,
	// in two files whose directives name one file, and then neither has a record (bodyRecords).
	Body recordedPlace `json:"body"`
	// Locals is every node the body's walk makes, in the order the walk met them. A local is its index here and never
	// a position: a type switch makes one variable a clause, all at one offset, and an unnamed parameter of a function
	// literal has no declaring identifier.
	Locals []recordedLocal `json:"locals"`
	// Uses is every use edge the walk adds while the body is open, each once, in the order the walk added them. An
	// edge another body adds too is in both records, though the graph holds it once. Either end may be an object
	// declared outside the body: a struct to struct conversion makes the fields of the two structs use each other,
	// a package-level struct's or those of an anonymous struct in a signature.
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

// recordedPlace is a place in the package's source: a file and an offset in that file as parsed. But for a record's
// own key the file is named as it was parsed, never as a `//line` directive names it, so a place is found in a
// build's own files by name and offset alone.
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
	// type, a name of the function's own signature, or a field of an anonymous struct any signature writes.
	endDeclared endKind = "declared"
	// endRoot is the graph's root.
	endRoot endKind = "root"
)

// recordedPresence is the six reasons a body cannot be summarized by its record. Each reads the build's own types
// past what a record of positions can replay, in a step after the walk or in the walk itself, so a build keeps such
// a body present and walks it itself.
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
	// which entry reads from the package's instances whole. The generic interface does not always stand in for its
	// instance: with `holder[T any, P interface{ *T }]` implements binds P and then finds it outside a constraint that
	// names an unbound T, so only the instance holder[int, *int] makes a type's method used.
	InterfaceInstance bool `json:"interfaceInstance"`
	// IgnoredType is whether a directive ignores a type name the body declares, one on its line or one on its whole
	// file. useIgnored then uses that type's methods and fields from go/types, which for a local alias of a
	// package-level type are declared outside the body.
	IgnoredType bool `json:"ignoredType"`
	// AllFieldsUsed is whether the body's walk used every field of a type, and of the structs and arrays it holds
	// (useAllFieldsRecursively), as it does for a conversion to or from unsafe.Pointer and for a local struct with a
	// structs.HostLayout field. Through a named pointer type, `type P *T` and `unsafe.Pointer(p)`, that reads fields
	// of a type nothing in the body names.
	AllFieldsUsed bool `json:"allFieldsUsed"`
	// UnplacedEnd is whether an end of one of the body's edges is an object declared outside the body that its place
	// does not find: a build resolves such an end by asking its own definitions for the object declared at that file
	// and offset, and the host's definitions, asked the same, hand back another object or none. go/types checks
	// the type of `var a, b struct{ x int }` once a variable and so makes x twice at one offset, keeping one for the
	// identifier, and a selection of the other's x would resolve to the wrong field. It is set too for a local of the
	// body declared in a file whose name another file of the package was parsed under, which a build cannot place,
	// and for what no record has a place for at all, which no package's syntax should hold but which keeps the body
	// present rather than stop the host's run: the body, a local or a written variable in no file of the file set, and
	// an object the body's walk sees, an own edge onto it, that the body does not declare.
	UnplacedEnd bool `json:"unplacedEnd"`
}

// encoded is record in its canonical form: JSON, every field in its declared order and no map among them, so one
// record has one encoding and two records of one body compare byte for byte.
func (record bodyRecord) encoded() ([]byte, error) {
	return json.Marshal(record)
}
