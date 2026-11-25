package parity

// Conversions to and from type parameters, judged over their constraints' normalized terms, whose order decides the
// type a finding names.
//
// A constraint every type satisfies, `any` or one of methods alone, has no terms, and a conversion to or from a value
// of it does not type-check, so it is no fixture; nor is a constraint no type satisfies, `interface{ int; string }`,
// whose values convert to nothing, nor one of more than 100 terms, which the checker refuses, nor a cycle.

type (
	Strings  interface{ ~string }
	Integers interface{ ~int | ~int64 | ~uint8 }
	Either   interface{ Strings | Integers }
	// Narrowed intersects Either with ~string | ~int, leaving ~string and ~int.
	Narrowed interface {
		Either
		~string | ~int
	}
	// Overlapping unions interfaces whose terms overlap, which only interface terms may, so normalizing folds Count
	// and int into ~int.
	Overlapping interface{ countOnly | intOnly | anyInt }
	countOnly   interface{ Count }
	intOnly     interface{ int }
	anyInt      interface{ ~int | ~int16 }
	Nested      interface{ Integers }
	Repeated    interface {
		Integers
		Integers
	}
	WithMethods interface {
		~int
		String() string
	}
)

func typeParameters[
	AllString ~string,
	NamedString Text | Label,
	AliasString LabelText | Label,
	MaybeString ~string | ~int,
	SourceInt ~int | ~int64,
	SourceBytes ~byte | ~rune,
	SourceMixed ~byte | int16 | int64,
	SourceStrings Strings,
	Intersection Narrowed,
	Folded Overlapping,
	Inner Nested,
	Twice Repeated,
	Methodical WithMethods,
	Comparable interface {
		comparable
		~int
	},
](
	number int, letter rune, octet byte, count Count, width Width,
	maybe MaybeString, sourceInt SourceInt, sourceBytes SourceBytes, sourceMixed SourceMixed,
	sourceStrings SourceStrings, intersection Intersection, folded Folded, inner Inner, twice Twice,
	methodical Methodical, comparableValue Comparable,
) {
	// A conversion to AllString, a type parameter whose one term is string, of a value whose type has one term, and
	// a value of Methodical, whose constraint has a String method, are in differences: the port withholds a fix there.
	const untyped = 0
	_ = AllString(letter)
	_ = AllString(octet)
	_ = NamedString(number)
	_ = AliasString(number)
	_ = MaybeString(number)
	_ = MaybeString(untyped)
	_ = MaybeString(count)
	_ = MaybeString(maybe)
	_ = string(maybe)
	_ = Text(maybe)
	_ = string(sourceInt)
	_ = AllString(sourceInt)
	_ = MaybeString(sourceInt)
	_ = string(sourceBytes)
	_ = AllString(sourceBytes)
	_ = string(sourceMixed)
	_ = AllString(sourceMixed)
	_ = string(sourceStrings)
	_ = Intersection(number)
	_ = string(intersection)
	_ = string(folded)
	_ = AllString(folded)
	_ = string(inner)
	_ = string(twice)
	_ = string(comparableValue)
	_ = SourceInt(number)
	_ = Folded(number)
	_ = Comparable(number)
}

// Box holds a type parameter of a generic type, which its methods convert to.
type Box[Element ~string, Index ~int | ~uint] struct {
	element Element
	index   Index
}

func (box Box[Element, Index]) convert(number int) Element {
	_ = string(box.index)
	_ = Element(box.index)
	_ = string(box.element)
	return Element(box.index) + Element(rune(number))
}

func instantiated(box Box[Text, Count]) {
	_ = string(box.index)
	_ = box.convert(1)
}

func closureOverParameter[Value ~int8 | ~uint16](value Value) func() string {
	return func() string {
		return string(value)
	}
}
