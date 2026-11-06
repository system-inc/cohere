package estree

// Value is what a node's property holds: a child, a list of them, a scalar, or one of the few structured
// values the parsers store. It is the closed set the converters and parsers write, so a reader takes a
// property apart with a type switch over it and the compiler refuses a case that can never match. The
// unexported marker keeps the set to the types declared here.
//
// An absent property, or one present with no value (upstream's undefined or null, which Has tells apart),
// is a nil Value.
type Value interface {
	isValue()
}

// Nodes is an array of children. Holes are nil elements, and a nil Nodes is still a present, truthy array.
type Nodes []*Node

// String is a string property: a name, an operator, a raw text, a kind.
type String string

// Boolean is a flag.
type Boolean bool

// Number is a numeric literal's value, and css's numbers.
type Number float64

// Integer is css's offsets, indexes and positions, which its parsers count in whole numbers.
type Integer int

// TemplateValue is a TemplateElement's value: Cooked is nil where the escape is invalid in a tagged
// template, the way upstream's cooked is null.
type TemplateValue struct {
	Cooked *string
	Raw    string
}

// Regex is a regular expression literal's regex property.
type Regex struct {
	Pattern string
	Flags   string
}

// Values is css's mixed list: a value parser's groups, a string among its nodes where a url() argument is
// one the parser cannot handle.
type Values []Value

// Record is css's plain objects, a postcss node's raws and source, a position, a selector's spaces: their
// members by name.
type Record map[string]Value

// Property is one named property of a node, in the order New and Set give them.
type Property struct {
	Key   string
	Value Value
}

func (Nodes) isValue()          {}
func (String) isValue()         {}
func (Boolean) isValue()        {}
func (Number) isValue()         {}
func (Integer) isValue()        {}
func (*TemplateValue) isValue() {}
func (*Regex) isValue()         {}
func (Values) isValue()         {}
func (Record) isValue()         {}
