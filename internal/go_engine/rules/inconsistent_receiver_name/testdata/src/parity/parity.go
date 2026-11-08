// Package parity holds every way the methods of one type name their receivers that `ST1016` reads, each found and
// each near miss, for the original and the port to judge alike.
package parity

// consistent names its receiver one way, beside receivers left unnamed or named "_", which count for none.
type consistent int

func (value consistent) first()     {}
func (value *consistent) second()   {}
func (consistent) unnamed()         {}
func (_ consistent) underscore()    {}
func (*consistent) unnamedPointer() {}

// mixed names its receiver two ways, across a value and a pointer receiver.
type mixed int

func (value mixed) first()   {}
func (other *mixed) second() {}
func (value mixed) third()   {}

// manyNames counts ten of one name and two of another, which sort as strings, "10x" before "2x".
type manyNames struct{}

func (often manyNames) method01()  {}
func (often manyNames) method02()  {}
func (often manyNames) method03()  {}
func (often manyNames) method04()  {}
func (often manyNames) method05()  {}
func (often manyNames) method06()  {}
func (often manyNames) method07()  {}
func (often manyNames) method08()  {}
func (often manyNames) method09()  {}
func (often manyNames) method10()  {}
func (rarely manyNames) method11() {}
func (rarely manyNames) method12() {}

// threeNames counts three, each once, and a name written outside ASCII, which %q prints as it is.
type threeNames struct{}

func (first threeNames) a()  {}
func (second threeNames) b() {}
func (été threeNames) c()    {}

// unnamedFirst's first method leaves its receiver unnamed, and is where the finding lands all the same.
type unnamedFirst struct{}

func (unnamedFirst) Alpha()       {}
func (value unnamedFirst) beta()  {}
func (other unnamedFirst) gamma() {}

// exportedFirst's method set orders an exported method before an unexported one, whatever their spelling.
type exportedFirst struct{}

func (value exportedFirst) zeta()  {}
func (other exportedFirst) Omega() {}

// A grouped declaration declares each of its types at the top level, each judged.
type (
	firstGrouped  struct{}
	secondGrouped struct{}
)

func (value firstGrouped) one()   {}
func (other firstGrouped) two()   {}
func (value secondGrouped) one()  {}
func (value *secondGrouped) two() {}

// inner's methods are promoted into outer and counted on inner alone, so outer's one name is consistent.
type inner struct{}

func (value inner) promoted()     {}
func (other *inner) promotedToo() {}

type outer struct {
	inner
}

func (receiver outer) own()     {}
func (receiver *outer) ownToo() {}

// generic's methods have receivers that are instances of it, never the type itself, so neither side counts them.
type generic[Element any] struct {
	element Element
}

func (value generic[Element]) first()   {}
func (other *generic[Element]) second() {}

// A method whose receiver is written through an alias has the alias as its receiver's type, never named, so neither
// side counts it, and named's own one name is consistent.
type named struct{}

type aliased = named

func (value named) direct()         {}
func (other aliased) throughAlias() {}

// twiceNamed has two names of its own and a third through its alias, which neither side counts.
type twiceNamed struct{}

type twiceAliased = twiceNamed

func (value twiceNamed) first()          {}
func (other twiceNamed) second()         {}
func (third twiceAliased) throughAlias() {}

// declaredElsewhere's methods are in other.go, counted with the type declared here.
type declaredElsewhere struct{}

// partlyGenerated has one name here and another in generated.go, which neither side counts.
type partlyGenerated struct{}

func (value partlyGenerated) handWritten() {}

// generatedFirst's first method is in generated.go, so the finding lands on its first method written by hand.
type generatedFirst struct{}

func (value generatedFirst) Beta()  {}
func (other generatedFirst) Gamma() {}

// testedType names its receiver one way here and another in parity_test.go, inconsistent only in the test variant.
type testedType struct{}

func (value testedType) inPackage() {}
