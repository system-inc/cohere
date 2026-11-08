package invalid_struct_tag

import "strconv"

// structTagValues is every value tag gives each key, in the order it gives them, read as reflect's StructTag.Lookup
// reads a tag: keys separated by spaces, each a run of characters that are neither spaces, quotes, colons nor control
// characters, followed by a colon and a quoted value. It stops at the first text that is not such a pair, keeping
// what it read before it, and fails only on a quoted value strconv cannot unquote.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa5008/structtag.go, itself a copy of
// reflect's lookup that gathers every key at once. Like the original, it finds no fault in a value whose closing quote
// is missing; it stops there.
func structTagValues(tag string) (map[string][]string, error) {
	remaining := tag
	values := map[string][]string{}
	for remaining != "" {
		index := 0
		for index < len(remaining) && remaining[index] == ' ' {
			index++
		}
		remaining = remaining[index:]
		if remaining == "" {
			break
		}

		// The key runs to the colon. A space, a quote or a control character ends it in error, reading bytes, not
		// runes, so the control characters of more than one byte are let through, as reflect lets them.
		index = 0
		for index < len(remaining) && remaining[index] > ' ' && remaining[index] != ':' && remaining[index] != '"' &&
			remaining[index] != 0x7f {
			index++
		}
		if index == 0 || index+1 >= len(remaining) || remaining[index] != ':' || remaining[index+1] != '"' {
			break
		}
		key := remaining[:index]
		remaining = remaining[index+1:]

		// The value runs to the next quote no backslash escapes.
		index = 1
		for index < len(remaining) && remaining[index] != '"' {
			if remaining[index] == '\\' {
				index++
			}
			index++
		}
		if index >= len(remaining) {
			break
		}
		quoted := remaining[:index+1]
		remaining = remaining[index+1:]

		value, unquoteError := strconv.Unquote(quoted)
		if unquoteError != nil {
			return nil, unquoteError
		}
		values[key] = append(values[key], value)
	}
	return values, nil
}
