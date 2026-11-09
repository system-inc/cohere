package strace_events

import "strings"

// A Text is a string strace printed escaped, a quoted argument's or a decoded descriptor's path: Raw
// as strace spelled it, Unescaped as the bytes it names. Key a path on Unescaped. Raw is a path no
// file has once it holds an escape: strace writes é as \303\251.
//
// Malformed is a Text that holds what strace never writes: an escape it has no such escape for, an
// octal one past a byte, a backslash ending it, or a byte bare that strace always escapes. Unescaped
// then reads what it can and keeps the rest as spelled, so it may be a name no file has. Whether to
// key it or refuse the trace is the consumer's: Loom refuses one, so its cache never keys a path it
// couldn't read.
type Text struct {
	Raw       string
	Unescaped string
	Malformed bool
}

// newQuotedText is a quoted string's text with its escapes decoded.
func newQuotedText(raw string) Text {
	unescaped, wellFormed := Unescape(raw)
	return Text{Raw: raw, Unescaped: unescaped, Malformed: !wellFormed}
}

// newDescriptorText is a decoded descriptor path with its escapes decoded.
func newDescriptorText(raw string) Text {
	unescaped, wellFormed := UnescapeDescriptor(raw)
	return Text{Raw: raw, Unescaped: unescaped, Malformed: !wellFormed}
}

// Unescape is a quoted string strace escaped, without its quotes, as the bytes it names, and whether
// it held only what strace writes. strace escapes a quote and a backslash with a backslash, the
// whitespace \f, \n, \r, \t and \v by letter, and any other byte it won't print as octal: three digits
// when a digit 0 to 7 follows it, otherwise as few as its value needs, so `>` in a decoded path is \76
// and an escape character before `[` is \33. Under -x it writes hexadecimal \xHH instead. Loom's BPF
// tracer writes strace's format with \a and \b besides, which read as their bytes too. Anything else
// after a backslash, and a byte outside printable ASCII or a quote left bare, is malformed: it is kept
// as spelled, and the text reads as not well formed. Go's strconv.Unquote reads only three-digit octal,
// so it refuses \76 and \33[.
func Unescape(text string) (string, bool) {
	return unescape(text, false)
}

// UnescapeDescriptor is Unescape for a path strace decoded a descriptor to, between its < and >,
// where strace escapes the angle brackets too, so a bare one is malformed as well.
func UnescapeDescriptor(text string) (string, bool) {
	return unescape(text, true)
}

// letterEscapes are the escapes strace and Loom's BPF tracer write as a backslash and a letter, each to
// the byte it stands for.
var letterEscapes = map[byte]byte{
	'"': '"', '\\': '\\', 'f': '\f', 'n': '\n', 'r': '\r', 't': '\t', 'v': '\v', 'a': '\a', 'b': '\b',
}

// unescape is Unescape, or UnescapeDescriptor for text strace decoded a descriptor to.
func unescape(text string, inDescriptor bool) (string, bool) {
	backslash := strings.IndexByte(text, '\\')
	if backslash < 0 {
		backslash = len(text)
	}
	wellFormed := true
	for index := 0; index < backslash; index++ {
		wellFormed = wellFormed && !isBare(text[index], inDescriptor)
	}
	if backslash == len(text) {
		return text, wellFormed
	}
	unescaped := make([]byte, 0, len(text))
	unescaped = append(unescaped, text[:backslash]...)
	for index := backslash; index < len(text); index++ {
		character := text[index]
		if character != '\\' {
			wellFormed = wellFormed && !isBare(character, inDescriptor)
			unescaped = append(unescaped, character)
			continue
		}
		if index+1 == len(text) {
			// A backslash ending the text escapes nothing.
			unescaped, wellFormed = append(unescaped, character), false
			continue
		}
		escaped := text[index+1]
		if letter, isLetter := letterEscapes[escaped]; isLetter {
			unescaped = append(unescaped, letter)
			index++
			continue
		}
		switch {
		case isOctalDigit(escaped):
			value, digits := 0, 0
			for digits < 3 && index+1+digits < len(text) && isOctalDigit(text[index+1+digits]) {
				value = value*8 + int(text[index+1+digits]-'0')
				digits++
			}
			if value > 0xff {
				// Three digits past \377 name no byte, so strace never wrote them.
				unescaped, wellFormed = append(unescaped, character), false
				continue
			}
			unescaped = append(unescaped, byte(value))
			index += digits
		case escaped == 'x' && index+3 < len(text) && isHexadecimalDigit(text[index+2]) &&
			isHexadecimalDigit(text[index+3]):
			unescaped = append(unescaped, hexadecimalValue(text[index+2])<<4|hexadecimalValue(text[index+3]))
			index += 3
		default:
			// An escape strace has none of, \q or a \x without two hexadecimal digits.
			unescaped, wellFormed = append(unescaped, character), false
		}
	}
	return string(unescaped), wellFormed
}

// isBare is whether a byte left unescaped is one strace always escapes: outside printable ASCII, a
// quote, or in a decoded descriptor path an angle bracket.
func isBare(character byte, inDescriptor bool) bool {
	return character < ' ' || character > '~' || character == '"' ||
		inDescriptor && (character == '<' || character == '>')
}

// QuotedArgument reads the quoted string arguments starts with, `"x.txt", O_RDONLY`, and returns it
// with what follows its closing quote. A string strace cut short at -s's limit is followed by `...`,
// which stays in what follows. arguments starting with no quote, or holding no closing one, is none.
func QuotedArgument(arguments string) (Text, string, bool) {
	if !strings.HasPrefix(arguments, `"`) {
		return Text{}, "", false
	}
	for index := 1; index < len(arguments); index++ {
		switch arguments[index] {
		case '\\':
			index++
		case '"':
			return newQuotedText(arguments[1:index]), arguments[index+1:], true
		}
	}
	return Text{}, "", false
}

// A Descriptor is a descriptor argument: Name as strace printed it, `AT_FDCWD`, `5` or `-100`, and
// the path strace decoded it to (--decode-fds=path), `AT_FDCWD</work>` naming the process's working
// directory. Path is empty when strace decoded none.
type Descriptor struct {
	Name string
	Path Text
}

// DescriptorArgument reads the descriptor arguments starts with, `AT_FDCWD</work>, "x"` or
// `3</work/dir>, 0x55d0`, and returns it with what follows its comma and the whitespace after. A
// descriptor that is the last argument, as fchdir's, ends arguments and is followed by "".
func DescriptorArgument(arguments string) (Descriptor, string, bool) {
	length := 0
	if strings.HasPrefix(arguments, "AT_FDCWD") {
		length = len("AT_FDCWD")
	} else {
		sign := 0
		if strings.HasPrefix(arguments, "-") {
			sign = 1
		}
		digits := leading(arguments[sign:], isDigit)
		if digits == 0 {
			return Descriptor{}, "", false
		}
		length = sign + digits
	}
	descriptor := Descriptor{Name: arguments[:length]}
	// strace escapes a `>` inside a decoded path (\76), so the first one closes it.
	if strings.HasPrefix(arguments[length:], "<") {
		if closing := strings.IndexByte(arguments[length+1:], '>'); closing >= 0 {
			descriptor.Path, length = newDescriptorText(arguments[length+1:length+1+closing]), length+2+closing
		}
	}
	if length == len(arguments) {
		return descriptor, "", true
	}
	if arguments[length] != ',' {
		return Descriptor{}, "", false
	}
	length++
	return descriptor, arguments[length+leading(arguments[length:], isSpace):], true
}

// NextArgument is what follows an argument's separating comma, given what followed the argument: the
// next argument, without the whitespace around it.
func NextArgument(rest string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(rest), ","))
}

func isOctalDigit(character byte) bool {
	return '0' <= character && character <= '7'
}

func isHexadecimalDigit(character byte) bool {
	return isDigit(character) || 'a' <= character && character <= 'f' || 'A' <= character && character <= 'F'
}

// hexadecimalValue is a hexadecimal digit's value, for a byte isHexadecimalDigit holds.
func hexadecimalValue(character byte) byte {
	switch {
	case isDigit(character):
		return character - '0'
	case 'a' <= character && character <= 'f':
		return character - 'a' + 10
	}
	return character - 'A' + 10
}
