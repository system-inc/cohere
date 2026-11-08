package invalid_struct_tag

import (
	"fmt"
	"go/ast"
	"go/types"
	"io"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"honnef.co/go/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// stringOptionMessage is what a `string` option on a field of a type it does not serve is told.
const stringOptionMessage = "invalid appearance of `string` tag option; it is only intended for fields of numeric " +
	"types or pointers to those"

// checkJsonTagSyntax judges tag, the `json` value of field's tag, by the grammar of encoding/json's version 2: a
// name, then options separated by commas, each an identifier or a single-quoted string, `case` and `format` taking a
// value after a colon, every option one the package knows, given once, `format` last.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa5008/jsonv2.go's validateJSONTag, whose
// branches and messages it keeps one for one, reporting each fault at the tag and reading on, so one tag can be told
// several things.
func checkJsonTagSyntax(context *rule.Context, field *ast.Field, tag string) {
	if tag == "-" {
		return
	}

	// An unexported field is never encoded, so any tag but "-" on it is a mistake. An embedded field of an unexported
	// type can still forward exported fields, so it is judged like an exported one (golang/go#24153, #32772).
	if len(field.Names) != 0 && !field.Names[0].IsExported() {
		if tag != "" {
			staticcheck_report.Report(context, field.Tag,
				fmt.Sprintf("unexported struct field cannot have non-ignored `json:%q` tag", tag))
		}
		return
	}

	remaining := tag
	if len(remaining) > 0 && !strings.HasPrefix(remaining, ",") {
		// Almost any name is accepted, as version 1 accepts it, short of a comma, a backslash or a quote.
		nameLength := len(remaining) - len(strings.TrimLeftFunc(remaining, func(character rune) bool {
			return !strings.ContainsRune(",\\'\"`", character)
		}))
		name := remaining[:nameLength]

		// A name followed by anything but a comma is malformed, or single-quoted, which consumeTagOption reads.
		if !strings.HasPrefix(remaining[nameLength:], ",") && len(name) != len(remaining) {
			var optionError error
			name, nameLength, optionError = consumeTagOption(remaining)
			if optionError != nil {
				staticcheck_report.Report(context, field.Tag, fmt.Sprintf("malformed `json` tag: %v", optionError))
			}
		}
		if !utf8.ValidString(name) {
			staticcheck_report.Report(context, field.Tag, fmt.Sprintf("invalid UTF-8 in JSON object name %q", name))
			// Each invalid byte becomes utf8.RuneError, as the encoder would write it.
			name = string([]rune(name))
		}
		if name == "-" && remaining[0] == '-' {
			staticcheck_report.Report(context, field.Tag,
				fmt.Sprintf("should encoding/json ignore this field or name it \"-\"? Either use `json:\"-\"` to "+
					"ignore the field or use `json:\"'-'%s` to specify %q as the name",
					strings.TrimPrefix(strconv.Quote(tag), `"-`), name))
		}
		remaining = remaining[nameLength:]
	}

	wasFormat := false
	seen := map[string]bool{}
	for len(remaining) > 0 {
		if remaining[0] != ',' {
			// The byte itself is quoted, as the original quotes it, not the rune it begins.
			staticcheck_report.Report(context, field.Tag,
				fmt.Sprintf("malformed `json` tag: invalid character %q before next option (expecting ',')",
					remaining[0]))
		} else {
			remaining = remaining[len(","):]
			if len(remaining) == 0 {
				staticcheck_report.Report(context, field.Tag, "malformed `json` tag: invalid trailing ',' character")
				break
			}
		}

		option, optionLength, optionError := consumeTagOption(remaining)
		if optionError != nil {
			staticcheck_report.Report(context, field.Tag, fmt.Sprintf("malformed `json` tag: %v", optionError))
		}
		rawOption := remaining[:optionLength]
		remaining = remaining[optionLength:]
		switch {
		case wasFormat:
			staticcheck_report.Report(context, field.Tag, "`format` tag option was not specified last")
		case strings.HasPrefix(rawOption, "'") && strings.TrimFunc(option, isLetterOrDigit) == "":
			staticcheck_report.Report(context, field.Tag,
				fmt.Sprintf("unnecessarily quoted appearance of `%s` tag option; specify `%s` instead", rawOption,
					option))
		}
		switch option {
		case "case":
			remaining = caseValueChecked(context, field, remaining)
		case "embed", "inline", "unknown", "omitzero", "omitempty":
		case "string":
			if !checkStringOption(context, field) {
				// The original moves to the next option here, so this `string` is never counted as seen.
				continue
			}
		case "format":
			remaining, wasFormat = formatValueChecked(context, field, remaining, wasFormat)
		default:
			// A key that looks like a known option, omitEmpty or omit_empty, is told the spelling it resembles.
			normalized := strings.ReplaceAll(strings.ToLower(option), "_", "")
			switch normalized {
			case "case", "inline", "unknown", "omitzero", "omitempty", "string", "format":
				staticcheck_report.Report(context, field.Tag,
					fmt.Sprintf("invalid appearance of `%s` tag option; specify `%s` instead", option, normalized))
			default:
				staticcheck_report.Report(context, field.Tag,
					fmt.Sprintf("invalid appearance of unknown `%s` tag option", option))
			}
		}

		if seen[option] {
			staticcheck_report.Report(context, field.Tag,
				fmt.Sprintf("duplicate appearance of `%s` tag option", rawOption))
		}
		seen[option] = true
	}

	if seen["inline"] && seen["unknown"] {
		staticcheck_report.Report(context, field.Tag, "field cannot have both `inline` and `unknown` specified")
	}
}

// caseValueChecked judges the value of a `case` option, tag being what follows the option's name, and is what is left
// of tag after it: tag itself when no colon follows, and what follows the colon when the value is malformed.
func caseValueChecked(context *rule.Context, field *ast.Field, tag string) string {
	if !strings.HasPrefix(tag, ":") {
		staticcheck_report.Report(context, field.Tag,
			"missing value for `case` tag option; specify `case:ignore` or `case:strict` instead")
		return tag
	}
	valueText := tag[len(":"):]
	value, valueLength, valueError := consumeTagOption(valueText)
	if valueError != nil {
		staticcheck_report.Report(context, field.Tag, fmt.Sprintf("malformed value for `case` tag option: %v",
			valueError))
		return valueText
	}
	rawValue := valueText[:valueLength]
	if strings.HasPrefix(rawValue, "'") {
		staticcheck_report.Report(context, field.Tag,
			fmt.Sprintf("unnecessarily quoted appearance of `case:%s` tag option; specify `case:%s` instead",
				rawValue, value))
	}
	if value != "ignore" && value != "strict" {
		staticcheck_report.Report(context, field.Tag,
			fmt.Sprintf("invalid appearance of unknown `case:%s` tag value", rawValue))
	}
	return valueText[valueLength:]
}

// formatValueChecked judges the value of a `format` option, tag being what follows the option's name, and is what is
// left of tag after it and whether a `format` has now been read: wasFormat unchanged when no colon follows or the
// value is malformed, and true once one is read whole.
func formatValueChecked(context *rule.Context, field *ast.Field, tag string, wasFormat bool) (string, bool) {
	if !strings.HasPrefix(tag, ":") {
		staticcheck_report.Report(context, field.Tag, "missing value for `format` tag option")
		return tag, wasFormat
	}
	valueText := tag[len(":"):]
	_, valueLength, valueError := consumeTagOption(valueText)
	if valueError != nil {
		staticcheck_report.Report(context, field.Tag, fmt.Sprintf("malformed value for `format` tag option: %v",
			valueError))
		return valueText, wasFormat
	}
	return valueText[valueLength:], true
}

// checkStringOption judges a `string` option on field: every type field's type can be, through a pointer at most,
// must be a boolean, an integer, a float or a string, and each that is not is told so once. It is false when field's
// type has no terms to judge, a type parameter constrained by any, which is told so too.
func checkStringOption(context *rule.Context, field *ast.Field) bool {
	typeSet := typeutil.NewTypeSet(context.TypesInfo.TypeOf(field.Type))
	if len(typeSet.Terms) == 0 {
		staticcheck_report.Report(context, field.Tag, stringOptionMessage)
		return false
	}
	for _, term := range typeSet.Terms {
		pointed := typeutil.Dereference(term.Type().Underlying())
		for _, pointedTerm := range typeutil.NewTypeSet(pointed).Terms {
			basic, isBasic := pointedTerm.Type().Underlying().(*types.Basic)
			// Booleans and strings are accepted because version 1 encodes them, by accident, and the message names
			// neither, since version 2 does not.
			if !isBasic || basic.Info()&(types.IsBoolean|types.IsInteger|types.IsFloat|types.IsString) == 0 {
				staticcheck_report.Report(context, field.Tag, stringOptionMessage)
			}
		}
	}
	return true
}

// consumeTagOption reads the option tag begins with, an identifier or a single-quoted string, and is the option, how
// many bytes of tag it took, and what was wrong with it, if anything: then the option is all of tag up to its next
// comma.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa5008/jsonv2.go's consumeTagOption,
// whose errors it returns word for word.
func consumeTagOption(tag string) (string, int, error) {
	// Options are separated by commas, as version 1 separates them.
	end := strings.IndexByte(tag, ',')
	if end < 0 {
		end = len(tag)
	}

	switch first, _ := utf8.DecodeRuneInString(tag); {
	case first == '_' || unicode.IsLetter(first):
		length := len(tag) - len(strings.TrimLeftFunc(tag, isLetterOrDigit))
		return tag[:length], length, nil
	case first == '\'':
		// A single-quoted string is a double-quoted Go string with single quotes at its ends, since neither a backtick
		// nor a double quote can stand in a struct tag as it is. It is rewritten as a double-quoted one and unquoted.
		isEscaping := false
		quoted := []byte{'"'}
		length := len(`'`)
		for len(tag) > length {
			character, characterLength := utf8.DecodeRuneInString(tag[length:])
			switch {
			case isEscaping:
				if character == '\'' {
					// An escaped single quote needs no escape between double quotes.
					quoted = quoted[:len(quoted)-1]
				}
				isEscaping = false
			case character == '\\':
				isEscaping = true
			case character == '"':
				quoted = append(quoted, '\\')
			case character == '\'':
				quoted = append(quoted, '"')
				length += len(`'`)
				unquoted, unquoteError := strconv.Unquote(string(quoted))
				if unquoteError != nil {
					return tag[:end], end, fmt.Errorf("invalid single-quoted string: %s", tag[:length])
				}
				return unquoted, length, nil
			}
			quoted = append(quoted, tag[length:][:characterLength]...)
			length += characterLength
		}
		// The error quotes at most ten bytes of the string, and the ellipsis says it was cut.
		length = min(length, 10)
		//cohere:ignore consistency-require-error-clause keeps upstream's text word for word, its ellipsis included
		return tag[:end], end, fmt.Errorf("single-quoted string not terminated: %s...", tag[:length])
	case len(tag) == 0:
		return tag[:end], end, io.ErrUnexpectedEOF
	default:
		return tag[:end], end, fmt.Errorf(
			"invalid character %q at start of option (expecting Unicode letter or single quote)", first)
	}
}

// isLetterOrDigit is whether character may stand in an identifier option: an underscore, a letter or a number.
func isLetterOrDigit(character rune) bool {
	return character == '_' || unicode.IsLetter(character) || unicode.IsNumber(character)
}
