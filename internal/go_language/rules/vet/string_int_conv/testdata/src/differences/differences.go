// Package differences holds the conversions where the port withholds the original's "Format the number as a
// decimal", each recorded on its line with its reason above it, and beside them the like conversions where the port
// keeps it. Every finding is the original's, at the same place in the same words, and so is every other fix.
package differences

import "fmt"

type (
	Text      string
	Count     int
	Label     = string
	Stringer  interface{ String() string }
	Printable interface {
		~int64
		Stringer
	}
	Counted interface {
		~int
		Count() int
	}
	Misprinted interface {
		~int
		String() int
	}
	Failing interface {
		~int
		Error() string
	}
	Misfailing interface {
		~int
		Error() int
	}
	Formatting interface {
		~uint
		Format(fmt.State, rune)
	}
	Misformatting interface {
		~uint
		Format(string, rune)
	}
	GoStringing interface {
		~int
		GoString() string
	}
)

func targets[AllString ~string, AliasString Label, NamedText Text](number int, count Count) {
	const untyped = 0
	// fmt.Sprint(number) is a string, where the conversion it would replace is an AllString.
	_ = AllString(number) // port withholds: Format the number as a decimal
	// fmt.Sprint(count) is a string, where the conversion it would replace is an AllString.
	_ = AllString(count) // port withholds: Format the number as a decimal
	// fmt.Sprint(untyped) is a string, where the conversion it would replace is an AllString.
	_ = AllString(untyped) // port withholds: Format the number as a decimal
	// AliasString's one term is Label, an alias of string, but fmt.Sprint(number) is a string, not an AliasString.
	_ = AliasString(number) // port withholds: Format the number as a decimal
	// NamedText's one term is Text, so the fix keeps the conversion, NamedText(fmt.Sprint(number)), and its type.
	_ = NamedText(number)
	// A string and a Label, an alias of string, are what fmt.Sprint(number) is, so both keep the fix.
	_ = string(number)
	_ = Label(number)
}

// named is a type parameter named String, which is no reason to withhold anything: the port reads types, not names.
func named[String ~string](number int) {
	// fmt.Sprint(number) is a string, where the conversion it would replace is a String, a type parameter.
	_ = String(number) // port withholds: Format the number as a decimal
}

func sources[
	Methodical interface {
		~int
		String() string
	},
	Embedding Printable,
	Plain interface{ ~int },
	Counting Counted,
	Misprinting Misprinted,
	Erring Failing,
	Miserring Misfailing,
	Formatter Formatting,
	Misformatter Misformatting,
	GoStringer GoStringing,
](
	methodical Methodical, embedding Embedding, plain Plain, counting Counting, misprinting Misprinting,
	erring Erring, miserring Miserring, formatter Formatter, misformatter Misformatter, goStringer GoStringer,
) {
	// fmt.Sprint(methodical) prints what Methodical's String method returns, not the number.
	_ = string(methodical) // port withholds: Format the number as a decimal
	// Text(fmt.Sprint(methodical)) keeps the conversion's type, but prints what the String method returns.
	_ = Text(methodical) // port withholds: Format the number as a decimal
	// fmt.Sprint(embedding) prints what the String method Printable embeds from Stringer returns, not the number.
	_ = string(embedding) // port withholds: Format the number as a decimal
	// A constraint with no methods, one with a method fmt does not call, and one whose String returns an int, which
	// is no fmt.Stringer, leave fmt.Sprint printing the number, so each keeps the fix.
	_ = string(plain)
	_ = string(counting)
	_ = string(misprinting)
	// fmt.Sprint(erring) prints what Failing's Error method returns, as it prints an error, not the number.
	_ = string(erring) // port withholds: Format the number as a decimal
	// fmt.Sprint(formatter) prints whatever Formatting's Format method writes, a fmt.Formatter's, not the number.
	_ = string(formatter) // port withholds: Format the number as a decimal
	// Text(fmt.Sprint(formatter)) keeps the conversion's type, but prints whatever the Format method writes.
	_ = Text(formatter) // port withholds: Format the number as a decimal
	// An Error that returns an int is no error, a Format that takes a string first is no fmt.Formatter, and GoString
	// serves only %#v, never the %v that fmt.Sprint uses, so each leaves the number printed and keeps the fix.
	_ = string(miserring)
	_ = string(misformatter)
	_ = string(goStringer)
}

func both[AllString ~string, Methodical interface {
	~int
	String() string
}](methodical Methodical) {
	// fmt.Sprint(methodical) is a string, not an AllString, and prints what the String method returns.
	_ = AllString(methodical) // port withholds: Format the number as a decimal
}

// Box holds a type parameter of a generic type, which its method converts to.
type Box[Element ~string] struct{ element Element }

func (box Box[Element]) convert(number int) Element {
	// fmt.Sprint(number) is a string, where the conversion it would replace is an Element.
	return Element(number) // port withholds: Format the number as a decimal
}

func instantiated(box Box[Text], count Count) Text {
	// An instance's element is a Text, no type parameter, so the conversion to it keeps the fix.
	return Text(count) + box.convert(1)
}
