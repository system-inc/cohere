package struct_tag

import (
	"errors"
	"strconv"
	"strings"
)

// The ways a tag can be malformed, each worded as the original's errTag values word it, since the words end the
// finding's message.

// errorTagSyntax is a key that ends before a colon, at the tag's end, a space, a quote or a control character, or a
// colon with nothing after it.
var errorTagSyntax = errors.New("bad syntax for struct tag pair")

// errorTagKeySyntax is an empty key: a pair that starts with its colon, a quote or a control character.
var errorTagKeySyntax = errors.New("bad syntax for struct tag key")

// errorTagValueSyntax is a value that is not a quoted string, is not closed, or does not unquote.
var errorTagValueSyntax = errors.New("bad syntax for struct tag value")

// errorTagValueSpace is a space in a json, xml or asn1 value where the encoding would misread it.
var errorTagValueSpace = errors.New("suspicious space in struct tag value")

// errorTagSpace is a pair that follows the one before it with no space between them.
var errorTagSpace = errors.New("key:\"value\" pairs not separated by spaces")

// spaceCheckedKeys are the keys whose values are judged for a suspicious space. x/tools' checkTagSpaces.
var spaceCheckedKeys = map[string]bool{"json": true, "xml": true, "asn1": true}

// validateStructTag is nil when tag is in the canonical form, a list of `key:"value"` pairs separated by spaces, whose
// values may hold spaces, and otherwise the way it is not. It reads a tag as reflect's StructTag.Lookup does, more
// strictly in two ways: pairs must be separated by a space, and a json, xml or asn1 value must not hold a space
// where its encoding would take it for part of a name or an option.
func validateStructTag(tag string) error {
	// remaining is what of tag is still to read, each pair cut off its front as it is read.
	remaining := tag
	for pairs := 0; remaining != ""; pairs++ {
		// Catches `x:"foo",y:"bar"`, which reflect reads as `x:"foo" ,y:"bar"`, its second key `,y`.
		if pairs > 0 && remaining[0] != ' ' {
			return errorTagSpace
		}
		remaining = strings.TrimLeft(remaining, " ")
		if remaining == "" {
			break
		}
		// The key runs to the colon. A space, a quote or a control character in it is a syntax error. Of the control
		// characters only the single byte ones are caught, [0x00, 0x1f] and 0x7f, since the tag is read by its bytes
		// and not by its runes.
		colon := 0
		for colon < len(remaining) && remaining[colon] > ' ' && remaining[colon] != ':' && remaining[colon] != '"' &&
			remaining[colon] != 0x7f {
			colon++
		}
		if colon == 0 {
			return errorTagKeySyntax
		}
		if colon+1 >= len(remaining) || remaining[colon] != ':' {
			return errorTagSyntax
		}
		if remaining[colon+1] != '"' {
			return errorTagValueSyntax
		}
		key := remaining[:colon]
		remaining = remaining[colon+1:]
		// The value is the quoted string after the colon, a backslash escaping the byte after it.
		closingQuote := 1
		for closingQuote < len(remaining) && remaining[closingQuote] != '"' {
			if remaining[closingQuote] == '\\' {
				closingQuote++
			}
			closingQuote++
		}
		if closingQuote >= len(remaining) {
			return errorTagValueSyntax
		}
		quoted := remaining[:closingQuote+1]
		remaining = remaining[closingQuote+1:]
		value, unquoteError := strconv.Unquote(quoted)
		if unquoteError != nil {
			return errorTagValueSyntax
		}
		if !spaceCheckedKeys[key] {
			continue
		}
		if valueSpaceError := validateValueSpaces(key, value); valueSpaceError != nil {
			return valueSpaceError
		}
	}
	return nil
}

// validateValueSpaces is nil when value, the unquoted value of a json, xml or asn1 key, holds no suspicious space,
// and errorTagValueSpace otherwise. The original's switch on the key inside validateStructTag's loop, where each
// `continue` is a nil here.
func validateValueSpaces(key string, value string) error {
	// options is what of value is judged for any space at all: the options after the name for json and xml, whose
	// names are judged on their own terms, and the whole value for asn1.
	options := value
	switch key {
	case "xml":
		// A space first or last, or more than one, is suspicious.
		if strings.Trim(value, " ") != value || strings.Count(value, " ") > 1 {
			return errorTagValueSpace
		}
		comma := strings.IndexRune(value, ',')
		if comma < 0 {
			return nil
		}
		// A space just before the comma is suspicious.
		if comma > 0 && value[comma-1] == ' ' {
			return errorTagValueSpace
		}
		options = value[comma+1:]
	case "json":
		// json allows a space in a name, so only the options are judged.
		comma := strings.IndexRune(value, ',')
		if comma < 0 {
			return nil
		}
		options = value[comma+1:]
	}
	if strings.IndexByte(options, ' ') >= 0 {
		return errorTagValueSpace
	}
	return nil
}
