// Package parity holds every receiver shape `ST1006` reads, each found and each near miss, for the original and the
// port to judge alike.
package parity

// valueType has methods on a value and on a pointer receiver, each name the check reads and the names it leaves.
type valueType int

func (self valueType) valueSelf()       {}
func (this valueType) valueThis()       {}
func (_ valueType) valueUnderscore()    {}
func (self *valueType) pointerSelf()    {}
func (this *valueType) pointerThis()    {}
func (_ *valueType) pointerUnderscore() {}
func (value valueType) named()          {}
func (valueType) unnamed()              {}
func (*valueType) unnamedPointer()      {}
func (myself valueType) nearSelf()      {}
func (thisOne valueType) nearThis()     {}
func (__ valueType) doubleUnderscore()  {}
func (Self valueType) capitalSelf()     {}

// A grouped declaration declares each of its types at the top level, each judged.
type (
	firstGrouped  struct{}
	secondGrouped struct{}
)

func (self firstGrouped) grouped() {}
func (_ *secondGrouped) grouped()  {}

// inner's methods are promoted into outer and judged on inner alone, once.
type inner struct{}

func (self inner) promoted()         {}
func (this *inner) promotedPointer() {}
func (self inner) overridden()       {}

type outer struct {
	inner
}

func (this outer) overridden() {}

// pointerOuter embeds a pointer, whose methods it promotes and does not declare.
type pointerOuter struct {
	*inner
}

func (receiver pointerOuter) own() {}

// derived takes none of base's methods, so only its own are judged.
type base struct{}

func (self base) baseMethod() {}

type derived base

func (_ derived) derivedMethod() {}

// An interface's methods have no receiver name, and an embedded one promotes nothing either side judges.
type speaker interface {
	speak()
}

type speakerHolder struct {
	speaker
}

func (self speakerHolder) hold() {}

// generic's methods have receivers that are instances of it, never the type itself, so neither side reports them.
type generic[Element any] struct {
	element Element
}

func (self generic[Element]) genericSelf()     {}
func (_ *generic[Element]) genericUnderscore() {}

// aliased names named, and a method whose receiver is written through the alias has the alias as its receiver's
// type, never named, so neither side reports it. direct's receiver names named itself, and both report it.
type named struct{}

type aliased = named

func (self aliased) throughAlias()         {}
func (this *aliased) throughAliasPointer() {}
func (_ named) direct()                    {}

// A type named "_" is no member, and holds nothing either side could judge.
type _ struct{}

// A type declared in a function is no member of the package, and can carry no method.
func local() {
	type localType struct{}
	_ = localType{}
}

// declaredElsewhere's method is in other.go, judged on the type declared here.
type declaredElsewhere struct{}
