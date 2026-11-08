// Package parity holds every branch of structtag's judgment, a clean case beside each, and the shapes a translation
// is most likely to read differently, for the original and the port to run over together.
package parity

import (
	"encoding/xml"
	"fmt"

	"parity/inner"
	. "sibling"
)

// Syntax holds each way a tag can be malformed, and well formed tags beside them.
type Syntax struct {
	PairAtEnd          int "hello"
	PairAtSpace        int "x y:\"z\""
	PairAtQuote        int `x"y:"z"`
	PairAtControl      int "x\x01:\"y\""
	PairAtDelete       int "x\x7f:\"y\""
	PairColonLast      int `x:`
	KeyColonFirst      int `:"x"`
	KeyQuoteFirst      int `"x":"y"`
	KeyControlFirst    int "\x01x:\"y\""
	KeyTabFirst        int "\tx:\"y\""
	KeyDeleteFirst     int "\x7fx:\"y\""
	ValueUnquoted      int `x:y`
	ValueBackquoted    int "x:`y`"
	ValueUnclosed      int `x:"y`
	ValueEscapedClose  int `x:"y\"`
	ValueBadEscape     int `x:"\q"`
	ValueNewline       int "x:\"a\nb\""
	PairsUnspaced      int `x:"y",z:"w"`
	PairsJoined        int `x:"y"z:"w"`
	SecondPairBad      int `x:"y" z`
	SecondValueBad     int `x:"y" z:w`
	TabBetweenPairs    int "x:\"y\"\tz:\"w\""
	MultiByteControl   int "x\u0085y:\"z\""
	LeadingSpaces      int `  x:"y"`
	TrailingSpaces     int `x:"y"  `
	ManySpaces         int `x:"y"    z:"w"`
	Empty              int ``
	OnlySpaces         int `   `
	EscapedQuote       int `x:"a\"b"`
	ColonInValue       int `x:"a:b"`
	NonASCIIKey        int `ключ:"значение"`
	SpacesInOtherValue int `x:"a b  c "`
	First, Second      int "hello"
	_                  int "hello"
	ñame               int "hello"
}

// Spaces holds a space in a json, xml and asn1 value, where it is suspicious and where it is not.
type Spaces struct {
	JSONNameSpace        int `json:"a b"`
	JSONNameSpaceOption  int `json:"c d,omitempty"`
	JSONSpaceBeforeComma int `json:"e ,omitempty"`
	JSONLeadingSpace     int `json:" f"`
	JSONOptionSpace      int `json:"g, omitempty"`
	JSONTrailingSpace    int `json:"h,omitempty "`
	JSONEscapedSpace     int `json:"i,\x20string"`
	JSONOnlyComma        int `json:","`
	XMLLeading           int `xml:" a"`
	XMLTrailing          int `xml:"b "`
	XMLTwoTogether       int `xml:"c  d"`
	XMLTwoApart          int `xml:"e f g"`
	XMLOneSpace          int `xml:"ns h"`
	XMLBeforeComma       int `xml:"i ,attr"`
	XMLAfterComma        int `xml:"j, attr"`
	XMLSpaceThenComma    int `xml:"ns k,attr"`
	XMLCommaFirst        int `xml:",attr"`
	XMLSpaceCommaFirst   int `xml:" ,attr"`
	XMLOnlySpace         int `xml:" "`
	XMLOptionSpaceLast   int `xml:"l,attr "`
	ASN1Space            int `asn1:"optional, explicit"`
	ASN1Clean            int `asn1:"optional,explicit"`
	ASN1Leading          int `asn1:" optional"`
	OtherKeySpace        int `yaml:"a, omitempty"`
	SecondPairSpace      int `yaml:"b" json:"m, omitempty"`
	BadThenSpace         int `json:"n, omitempty" yaml`
	SpaceThenBad         int `yaml "x" json:"o, omitempty"`
}

// Unexported holds json and xml tags on unexported fields, and the tags that are left alone there.
type Unexported struct {
	jsonTagged        int `json:"j"`
	xmlTagged         int `xml:"x"`
	bothTagged        int `json:"j2" xml:"x2"`
	xmlAfterSkip      int `json:"-" xml:"x3"`
	bothSkipped       int `json:"-" xml:"-"`
	bothEmpty         int `json:"" xml:""`
	optionsOnly       int `json:",omitempty"`
	otherKey          int `yaml:"y"`
	malformed         int `json:"b`
	malformedAndNamed int `json:"c" x`
	_                 int `json:"blank"`
	ñame              int `json:"n"`
	Exported          int `json:"e"`
	Ñame              int `json:"N"`
	twoOf, threeOf    int `xml:"same"`
	leaf              `json:"u"`
	*pointerLeaf      `xml:"p"`
}

// Duplicates holds names repeated across one struct's own fields, and names that only look repeated.
type Duplicates struct {
	First           int `json:"a"`
	Second          int `json:"a"`
	WithOption      int `json:"a,string"`
	unexportedTop   int `json:"a"`
	XMLSameName     int `xml:"a"`
	XMLRepeat       int `xml:"a,omitempty"`
	XMLAttribute    int `xml:"a,attr"`
	XMLAttribute2   int `xml:"a,omitempty,attr"`
	XMLAttributeish int `xml:"b,attribute"`
	XMLPlain        int `xml:"b"`
	XMLAttrName     int `xml:"attr"`
	XMLAttrName2    int `xml:"attr,attr"`
	XMLAttrName3    int `xml:"attr,attr"`
	Ignored         int `json:"-"`
	IgnoredToo      int `json:"-"`
	DashName        int `json:"-,"`
	DashNameToo     int `json:"-,omitempty"`
	OptionsOnly     int `json:",omitempty"`
	OptionsOnlyToo  int `json:",omitempty"`
	Upper           int `json:"A"`
	Shared, Shared2 int `json:"s"`
	Mixed           int `json:"m" xml:"m"`
	MixedToo        int `json:"m" xml:"n"`
	MalformedRepeat int `json:"a" x`
	Unnamed         int
	UnnamedToo      int
	Spaced          int `json:"b c"`
	SpacedToo       int `json:"b c,omitempty"`
}

// XMLNamed holds XMLName's tag, which names the struct's element and claims no xml name, and its json name, which is
// claimed as any other field's.
type XMLNamed struct {
	XMLName   xml.Name `xml:"root" json:"n"`
	Root      int      `xml:"root"`
	JSONRoot  int      `json:"n"`
	RootAgain int      `xml:"root"`
}

// leaf is embedded below, unexported, with exported fields that claim names and an unexported one that does not.
type leaf struct {
	Name    string `json:"name" xml:"name"`
	hidden  string `json:"hidden"`
	Skipped string `json:"-"`
	Plain   string
	Attr    string `xml:"attr,attr"`
}

// Leaf is embedded below, exported.
type Leaf struct {
	Name   string `json:"name" xml:"name"`
	Hidden string `json:"hidden"`
}

// LeafToo claims the same json name as Leaf, and an xml attribute of the name Leaf gives an element.
type LeafToo struct {
	Name string `json:"name" xml:"name,attr"`
}

// pointerLeaf is embedded below through a pointer, which the judgment does not look into.
type pointerLeaf struct {
	Name string `json:"name"`
}

// Middle puts a Leaf one level further down.
type Middle struct {
	LeafToo
}

// MiddleToo puts another Leaf one level further down.
type MiddleToo struct {
	Leaf
}

// Number is embedded below, a defined type that is not a struct.
type Number int

// LeafAlias is embedded below under its alias's name.
type LeafAlias = Leaf

// WithXMLName is embedded below, its XMLName claiming no xml name at the level it is embedded at either.
type WithXMLName struct {
	XMLName xml.Name `xml:"n"`
}

// WithN claims the xml name WithXMLName's XMLName gives its element.
type WithN struct {
	N int `xml:"n"`
}

// Shadowed holds names a deeper field gives and a shallower one repeats, which do not conflict.
type Shadowed struct {
	Leaf
	Name string `json:"name"`
	Middle
}

// SameLevel holds two embedded structs whose fields claim one name at one level.
type SameLevel struct {
	leaf
	Leaf
	LeafToo
}

// DeepSameLevel holds the same two levels down, reported at the embedding of the struct of the field that repeats.
type DeepSameLevel struct {
	Middle
	MiddleToo
}

// TaggedEmbedding holds embedded structs a tag names, which claim that name and are not looked into, and one whose tag
// gives only options, which is.
type TaggedEmbedding struct {
	Leaf    `json:"leaf"`
	LeafToo `json:",omitempty" xml:"leafToo"`
	Middle  `json:"leaf"`
	Named   string `json:"name"`
}

// IgnoredEmbedding holds an embedded struct the encoding skips.
type IgnoredEmbedding struct {
	Leaf `json:"-"`
	LeafToo
}

// NotLookedInto holds embedded fields whose names are not looked for: a pointer, a type that is not a struct and an
// interface.
type NotLookedInto struct {
	*pointerLeaf
	Number `json:",omitempty"`
	fmt.Stringer
	LeafToo
}

// AliasEmbedding embeds a struct under its alias's name.
type AliasEmbedding struct {
	LeafAlias
	LeafToo
}

// XMLNameEmbedding embeds a struct whose XMLName gives an element name another embedded struct's field gives.
type XMLNameEmbedding struct {
	WithXMLName
	WithN
}

// GenericLeaf is a generic struct whose fields repeat a name.
type GenericLeaf[T any] struct {
	Value T `json:"value"`
	Name  T `json:"name"`
	Again T `json:"value"`
}

// GenericEmbedding embeds an instance of a generic struct beside a struct that claims one of its names.
type GenericEmbedding struct {
	GenericLeaf[int]
	Leaf
}

// GenericOuter embeds a generic struct instantiated with its own type parameter.
type GenericOuter[T any] struct {
	GenericLeaf[T]
	LeafToo
	unexported T `json:"unexported"`
}

// FileOrder embeds a struct declared in another file of this package, after and before one declared in this one.
type FileOrder struct {
	Leaf
	FileLeaf
}

// FileOrderReversed is FileOrder with its fields the other way round.
type FileOrderReversed struct {
	FileLeaf
	Leaf
}

// PackageOrder embeds a struct of a package in a directory below this one's, after and before one of this package.
type PackageOrder struct {
	inner.InnerLeaf
	Leaf
}

// PackageOrderReversed is PackageOrder with its fields the other way round.
type PackageOrderReversed struct {
	Leaf
	inner.InnerLeaf
}

// DeepPackage embeds a struct of another package that embeds one of its own, and a struct of that package that claims
// a name with an unexported field, which is not looked at.
type DeepPackage struct {
	inner.Middle
	MiddleToo
	inner.Hiding
	LeafToo
}

// DotImported embeds a struct of a dot imported package in a sibling directory.
type DotImported struct {
	SiblingLeaf
	Leaf
}

// local declares struct types in a function body, a generic function's and a closure's, and in a conversion.
func local() any {
	type Local struct {
		A int `json:"a"`
		B int `json:"a"`
		C int "hello"
	}
	closure := func() any {
		return struct {
			x int `json:"x"`
			Y int `json:"x"`
		}{}
	}
	converted := struct {
		A int `json:"a, b"`
	}(struct{ A int }{})
	return []any{Local{}, closure(), converted}
}

// generic declares a struct type in a generic function's body.
func generic[T any]() any {
	type Local struct {
		A T `json:"a"`
		B T `json:"a"`
	}
	return Local{}
}

// constrained declares a struct type in a type parameter's constraint.
func constrained[T struct {
	A int `json:"a"`
	B int `json:"a"`
}]() {
}

// Anonymous holds struct types written in place, as a field's type, nested, and in a package variable.
var Anonymous struct {
	Outer struct {
		Inner struct {
			A int `json:"a"`
			B int `json:"a"`
		} `json:"a"`
		Leaf
		LeafToo
	} `json:"a"`
	Other int `json:"a"`
	bad   int "hello"
}

// keep uses what the package declares and does not export, so nothing in it is unused.
var keep = []any{local, generic[int], constrained[struct {
	A int `json:"a"`
	B int `json:"a"`
}]}
