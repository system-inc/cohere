// Package uses uses the library's deprecated objects and package every way the check reads a use, and the standard
// library's deprecations under Go 1.22, where the port and staticcheck's SA1019 still agree finding for finding. The
// differential test runs both over each and compares.
package uses

import (
	"crypto/x509/pkix"
	"go/ast"
	"io/ioutil" // Found by both: deprecated since Go 1.19, which the module targets.
	"math/rand"
	"strings"

	"example.com/parity/library"
	"example.com/parity/old"         // Found by both: a deprecated package.
	_ "example.com/parity/old"       // Found by both: a deprecated package imported for its effects.
	renamed "example.com/parity/old" // Found by both: a deprecated package under another name.
)

// Found by both: a package-level variable before any function, given a deprecated function's result.
var early = library.OldFunction()

func uses(record library.Record, pointer *library.Record, embedding library.Embedding, box library.Box[int],
	value library.Interface) {
	// Found by both, one each: a function, a variable, a constant, a type, a field, a method value and a call.
	_ = library.OldFunction()
	_ = library.OldVariable
	_ = library.OldConstant
	var _ library.OldType
	_ = record.OldField
	_ = record.OldMethod
	record.OldMethod()
	// Found by both: a field and a method through a pointer, and promoted through an embedding.
	_ = pointer.OldField
	pointer.OldMethod()
	_ = embedding.OldField
	embedding.OldMethod()
	// Found by both: a generic type's method, deprecated on its origin, and an interface's method.
	box.OldMethod()
	value.OldMethod()
	// Found by both: a method expression, reported over the method's name alone.
	_ = library.Record.OldMethod
	// Found by both: a struct literal's key, reported over the key.
	_ = library.Record{OldField: 1, NewField: 2}
	_ = &library.Record{NewField: 2, OldField: 1}
	// Found by neither: what is not deprecated.
	_ = library.NewFunction()
	_ = library.NewVariable
	_ = library.NewConstant
	_ = record.NewField
	record.NewMethod()
	box.NewMethod()
	value.NewMethod()
	_ = old.Value
	_ = renamed.Value
	// Found by neither: a literal whose type is elided, a positional literal, and a map's keys.
	_ = []library.Record{{OldField: 1}}
	_ = library.Record{1, 2}
	_ = map[string]int{"OldField": 1}
	// Found by both: the standard library's deprecations the module's version has reached, of each kind of message.
	_ = strings.Title("x")
	rand.Seed(1)
	_ = rand.Read
	var _ *ast.Object
	// Found by neither: a member of a deprecated package that is not deprecated itself, and a deprecation of the
	// standard library staticcheck's knowledge does not date.
	_, _ = ioutil.ReadAll(nil)
	var _ pkix.CertificateList
}

// Deprecated: a deprecated function may use deprecated objects.
func deprecatedUser() {
	_ = library.OldFunction()
	_ = func() int { return library.OldVariable }
}

// Found by neither: a package-level variable after a deprecated function in its file is judged as if inside it, the
// original's false negative.
var afterDeprecatedFunction = library.OldFunction()

func notDeprecated() {
	// Found by both: the function declaration is no longer the deprecated one.
	_ = library.OldVariable
}
