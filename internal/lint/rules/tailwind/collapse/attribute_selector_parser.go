// The attribute selector parser: one left-to-right pass that splits `[data-state="open" i]` into
// its attribute, operator, quote, value and case-sensitivity flag, or rejects it.
//
// Ported from `src/attribute-selector-parser.ts` at the v4.3.3 tag, read at the tag rather than
// from the minified bundle, so a disagreement with the engine is a finding rather than version
// skew.
//
// # Absence is spelled the way upstream spells null
//
// Upstream's result has five fields, four of them nullable. Operator, Quote and Sensitivity are
// never the empty string when present, so the empty string stands for null and a caller can
// concatenate them directly, as upstream's caller does with `quote ?? ""` and a truthiness test on
// sensitivity. Value is different: an unterminated quote yields a present but empty value (see
// below), so it is a pointer, nil exactly where upstream has null.
//
// # Deliberate fidelity to JavaScript semantics
//
// Three behaviors here are consequences of how the upstream loop is written rather than of stated
// intent. They are ported deliberately and each is called out at its branch below:
//
//   - A quoted value closes on the last unescaped matching quote before the `]`, not the first,
//     because upstream's scan records every quote it meets and never breaks. `[a="x"y"]` has the
//     value `x"y`.
//   - A quoted value with no closing quote is not rejected. Upstream slices it as
//     `input.slice(start, start - 1)`, which JavaScript answers with the empty string, and parsing
//     resumes just after the opening quote. `[a="i]` is the attribute `a`, operator `=`, quote `"`,
//     an empty value and the sensitivity `i`.
//   - An escape at the last character of the attribute name steps over the closing `]`, and the
//     reads past the end of the input that follow are `NaN` upstream, which matches nothing. The
//     selector is rejected rather than the read panicking, which is what
//     attributeSelectorCharacterAt is for.
//
// # Bytes rather than UTF-16 code units
//
// Upstream indexes by UTF-16 code unit and this indexes by byte. The two agree on every result:
// every character the parser tests for is ASCII, every UTF-8 byte of a non-ASCII character is
// >= 0x80 and so can match none of them, and the attribute name's `>= 128` test accepts each such
// byte just as upstream accepts each code unit. Every slice boundary therefore falls on an ASCII
// character or the end of the input, in either encoding.
package tailwind

// AttributeSelector is upstream's parsed attribute selector.
//
// A caller rebuilds the selector as upstream does:
// Attribute + Operator + Quote + *Value + Quote, then " " + Sensitivity when Sensitivity is set.
type AttributeSelector struct {
	// Attribute is the name, escapes kept as written, e.g. `data-state` or `data-\:foo`.
	Attribute string
	// Operator is one of `=`, `~=`, `|=`, `^=`, `$=` or `*=`, and empty where upstream has null,
	// which is when the selector is a bare `[name]`.
	Operator string
	// Quote is `"` or `'` for a quoted value, and empty where upstream has null.
	Quote string
	// Value is the value without its quotes, nil exactly where upstream has null, which is when
	// Operator is empty. A pointer because a present value can be empty: see the unterminated
	// quote at the top of this file.
	Value *string
	// Sensitivity is `i` or `s`, lowercased whichever case was written, and empty where upstream
	// has null.
	Sensitivity string
}

// The characters the parser tests for, as the upstream constant set. The letter and digit ranges
// of the attribute name are written inline as ranges instead.
const (
	attributeSelectorAsterisk       = '*'
	attributeSelectorBackslash      = '\\'
	attributeSelectorCaret          = '^'
	attributeSelectorCarriageReturn = '\r'
	attributeSelectorClose          = ']'
	attributeSelectorDash           = '-'
	attributeSelectorDollar         = '$'
	attributeSelectorDoubleQuote    = '"'
	attributeSelectorEquals         = '='
	attributeSelectorLineBreak      = '\n'
	attributeSelectorLowerI         = 'i'
	attributeSelectorLowerS         = 's'
	attributeSelectorNonASCII       = 0x80
	attributeSelectorOpen           = '['
	attributeSelectorPipe           = '|'
	attributeSelectorSingleQuote    = '\''
	attributeSelectorSpace          = ' '
	attributeSelectorTab            = '\t'
	attributeSelectorTilde          = '~'
	attributeSelectorUnderscore     = '_'
	attributeSelectorUpperI         = 'I'
	attributeSelectorUpperS         = 'S'
)

// attributeSelectorPastEnd is what attributeSelectorCharacterAt answers past the end of the input,
// standing in for the `NaN` JavaScript's `charCodeAt` returns there. It equals no byte, so every
// comparison against it fails, as every comparison against `NaN` does.
const attributeSelectorPastEnd = -1

// attributeSelectorCharacterAt is upstream's `input.charCodeAt(index)`, including its answer past
// the end of the input.
//
// Upstream reads past the end in one place, after an escape steps over the closing `]` (see the
// attribute name loop), and its whitespace skips are unbounded because the `]` normally stops
// them. Go would panic where JavaScript reads `NaN`, so every read goes through here.
func attributeSelectorCharacterAt(input string, index int) int {
	if index < 0 || index >= len(input) {
		return attributeSelectorPastEnd
	}
	return int(input[index])
}

// ParseAttributeSelector is upstream's `parse`: it reads `[name]` or `[name op value flag]`, with
// whitespace allowed around each part, and reports false for anything else.
func ParseAttributeSelector(input string) (AttributeSelector, bool) {
	// Must start with `[` and end with `]`.
	if attributeSelectorCharacterAt(input, 0) != attributeSelectorOpen ||
		attributeSelectorCharacterAt(input, len(input)-1) != attributeSelectorClose {
		return AttributeSelector{}, false
	}

	index := 1
	start := index
	end := len(input) - 1

	// Skip whitespace, e.g.: [   data-foo]
	//                         ^^^
	for isAttributeSelectorWhitespace(attributeSelectorCharacterAt(input, index)) {
		index++
	}

	// Attribute name, e.g.: [data-foo]
	//                        ^^^^^^^^
	start = index
	for ; index < end; index++ {
		character := attributeSelectorCharacterAt(input, index)
		// An escape takes the next character into the name unexamined. When the escape is the last
		// character before the `]`, this steps over the `]` and leaves index one past the end, so
		// the operator read below sees nothing and rejects the selector.
		if character == attributeSelectorBackslash {
			index++
			continue
		}
		if character >= 'A' && character <= 'Z' {
			continue
		}
		if character >= 'a' && character <= 'z' {
			continue
		}
		if character >= '0' && character <= '9' {
			continue
		}
		if character == attributeSelectorDash || character == attributeSelectorUnderscore {
			continue
		}
		// Non-ASCII code points are valid in a CSS ident, e.g.: `[data-état]`.
		if character >= attributeSelectorNonASCII {
			continue
		}
		break
	}

	// Must have at least one character in the attribute name.
	if start == index {
		return AttributeSelector{}, false
	}
	attribute := input[start:index]

	// Skip whitespace, e.g.: [data-foo   =value]
	//                                 ^^^
	for isAttributeSelectorWhitespace(attributeSelectorCharacterAt(input, index)) {
		index++
	}

	// At the end, e.g.: `[data-foo]`.
	if index == end {
		return AttributeSelector{Attribute: attribute}, true
	}

	// Operator, e.g.: [data-foo*=value]
	//                          ^^
	operator := ""
	character := attributeSelectorCharacterAt(input, index)
	switch {
	case character == attributeSelectorEquals:
		operator = "="
		index++
	case (character == attributeSelectorTilde ||
		character == attributeSelectorPipe ||
		character == attributeSelectorCaret ||
		character == attributeSelectorDollar ||
		character == attributeSelectorAsterisk) &&
		attributeSelectorCharacterAt(input, index+1) == attributeSelectorEquals:
		operator = input[index:index+1] + "="
		index += 2
	default:
		// Invalid operator, and also the past-the-end read after an escaped `]`.
		return AttributeSelector{}, false
	}

	// Skip whitespace, e.g.: [data-foo*=   value]
	//                                   ^^^
	for isAttributeSelectorWhitespace(attributeSelectorCharacterAt(input, index)) {
		index++
	}

	// An operator with no value is invalid, e.g.: `[data-foo*=]`.
	if index == end {
		return AttributeSelector{}, false
	}

	// Value, e.g.: [data-foo*=value]
	//                         ^^^^^
	value := ""

	// Quoted value, e.g.: [data-foo*="value"]
	//                                ^^^^^^^
	quote := ""
	character = attributeSelectorCharacterAt(input, index)
	if character == attributeSelectorSingleQuote || character == attributeSelectorDoubleQuote {
		quote = input[index : index+1]
		index++

		start = index
		for scan := index; scan < end; scan++ {
			current := attributeSelectorCharacterAt(input, scan)
			if current == character {
				// A matching quote. The scan does not stop here, so a later unescaped quote
				// replaces this one as the close, and `[a="x"y"]` has the value `x"y`.
				index = scan + 1
			} else if current == attributeSelectorBackslash {
				// Skip the escaped character.
				scan++
			}
		}

		// With no closing quote, index is still start, and upstream's
		// `input.slice(start, start - 1)` is the empty string rather than an error. The value is
		// present and empty, and parsing resumes just after the opening quote, so `[a="i]` reads
		// the `i` as the sensitivity flag.
		if index-1 >= start {
			value = input[start : index-1]
		}
	} else {
		// Unquoted value, e.g.: [data-foo*=value]
		//                                  ^^^^^
		// Keep going until whitespace or the end.
		start = index
		for index < end && !isAttributeSelectorWhitespace(attributeSelectorCharacterAt(input, index)) {
			index++
		}
		value = input[start:index]
	}

	// Skip whitespace, e.g.: [data-foo*=value   ]
	//                                        ^^^
	for isAttributeSelectorWhitespace(attributeSelectorCharacterAt(input, index)) {
		index++
	}

	// At the end, e.g.: `[data-foo=value]`.
	if index == end {
		return AttributeSelector{Attribute: attribute, Operator: operator, Quote: quote, Value: &value}, true
	}

	// Sensitivity, e.g.: [data-foo=value i]
	//                                    ^
	// No whitespace is required before it after a quoted value, so `[data-foo="value"i]` has one.
	// After an unquoted value the value runs to the next whitespace, so `[data-foo=valuei]` does
	// not.
	sensitivity := ""
	switch attributeSelectorCharacterAt(input, index) {
	case attributeSelectorLowerI, attributeSelectorUpperI:
		sensitivity = "i"
		index++
	case attributeSelectorLowerS, attributeSelectorUpperS:
		sensitivity = "s"
		index++
	default:
		// Invalid sensitivity.
		return AttributeSelector{}, false
	}

	// Skip whitespace, e.g.: [data-foo=value i   ]
	//                                         ^^^
	for isAttributeSelectorWhitespace(attributeSelectorCharacterAt(input, index)) {
		index++
	}

	// Anything left after the sensitivity is invalid, e.g.: [data-foo=value iX]
	//                                                                        ^
	if index != end {
		return AttributeSelector{}, false
	}

	return AttributeSelector{
		Attribute:   attribute,
		Operator:    operator,
		Quote:       quote,
		Value:       &value,
		Sensitivity: sensitivity,
	}, true
}

// isAttributeSelectorWhitespace is upstream's `isAsciiWhitespace`: space, tab, line feed and
// carriage return. Form feed is not among them, though CSS counts it as whitespace.
func isAttributeSelectorWhitespace(character int) bool {
	switch character {
	case attributeSelectorSpace, attributeSelectorTab, attributeSelectorLineBreak, attributeSelectorCarriageReturn:
		return true
	}
	return false
}
