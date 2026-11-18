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

	// UsedValueType is each named type an expression's type the part holds reaches: the type itself when it is named,
	// or each named type its structure holds, and every type argument's at any depth (functionUsesRecorder.addNamed),
	// since moving a value to an interface, comparing it or copying it reads a method set or a layout another build can
	// change.
	UsedValueType

	// UsedTypeArgument is each named type an instance's type argument reaches, as UsedValueType's are reached.
	UsedTypeArgument

	// UsedConstant is a constant read by its value.
	UsedConstant

	// usedKindsEnd is one past the highest kind, kept last, so a PartUse can assert its kind bits hold every kind.
	usedKindsEnd
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
