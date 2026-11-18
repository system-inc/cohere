package build_reads

import "strings"

// UseKinds are the kinds of use one part of a declaration made of one object, a bit each.
type UseKinds uint8

const (
	// UsedNamed is an identifier the type checker resolved to the object, by its name: a universe object by its name
	// alone, which a differing file can shadow, and one of a dot import with its package.
	UsedNamed UseKinds = 1 << iota

	// UsedSelected is a selection of the object, from a receiver, by its name, at a path through embedded fields:
	// a field, a method called, a method value or a method expression.
	UsedSelected

	// UsedValueType is the named type of an expression's type the part holds, or the named type a pointer points to,
	// and each such type among its type arguments, since moving a value to an interface, comparing it or copying it
	// reads a method set or a layout another build can change.
	UsedValueType

	// UsedTypeArgument is the named type an instance's type argument is, or points to.
	UsedTypeArgument

	// UsedConstant is a constant read by its value.
	UsedConstant
)

// useKindNames are the kinds' names, by bit, in the order String writes them.
var useKindNames = []struct {
	kind UseKinds
	name string
}{
	{UsedNamed, "Named"},
	{UsedSelected, "Selected"},
	{UsedValueType, "ValueType"},
	{UsedTypeArgument, "TypeArgument"},
	{UsedConstant, "Constant"},
}

// String is each kind's name, joined by `|`.
func (kinds UseKinds) String() string {
	var names []string
	for _, named := range useKindNames {
		if kinds&named.kind != 0 {
			names = append(names, named.name)
		}
	}
	return strings.Join(names, "|")
}
