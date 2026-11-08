package parity

import (
	"bytes"
	"strings"
)

type holder struct {
	text  string
	inner *holder
	data  []byte
}

func produce() string { return "" }

func pair() (string, string) { return "", "" }

const zero = 0

const prefixConstant = "pre"

// Each guard with the unconditional call that does its work on the same two arguments is reported at the whole if
// statement, and the message names the call.
func unconditionalCalls(text string, affix string, replacement string, data []byte, part []byte) {
	if strings.HasPrefix(text, affix) {
		text = strings.TrimPrefix(text, affix)
	}
	if strings.HasSuffix(text, affix) {
		text = strings.TrimSuffix(text, affix)
	}
	if strings.Contains(text, affix) {
		text = strings.Replace(text, affix, replacement, -1)
	}
	if bytes.HasPrefix(data, part) {
		data = bytes.TrimPrefix(data, part)
	}
	if bytes.HasSuffix(data, part) {
		data = bytes.TrimSuffix(data, part)
	}
	if bytes.Contains(data, part) {
		data = bytes.Replace(data, part, nil, 1)
	}
	// Literals compare by kind and spelling.
	if strings.HasPrefix(text, "pre") {
		text = strings.TrimPrefix(text, "pre")
	}
	_, _ = text, data
}

// A call that does another guard's work, or the work on other arguments, or a call of too few arguments, is not
// reported.
func unconditionalNearMisses(text string, other string, affix string, data []byte, part []byte) {
	if strings.HasPrefix(text, affix) {
		text = strings.TrimSuffix(text, affix)
	}
	if strings.HasSuffix(text, affix) {
		text = strings.TrimPrefix(text, affix)
	}
	if strings.Contains(text, affix) {
		text = strings.ReplaceAll(text, affix, "")
	}
	if strings.HasPrefix(text, affix) {
		text = strings.TrimPrefix(other, affix)
	}
	if strings.HasPrefix(text, affix) {
		text = strings.TrimPrefix(affix, text)
	}
	if strings.HasPrefix(text, affix) {
		text = strings.ToLower(text)
	}
	if bytes.HasPrefix(data, part) {
		data = []byte(strings.TrimPrefix(string(data), string(part)))
	}
	// The strings guard and the bytes call, either way about, are not a pair.
	if bytes.HasPrefix(data, part) {
		text = strings.TrimPrefix(text, affix)
	}
	// Literals of another spelling are not the same, even of the same value.
	if strings.HasPrefix(text, "pre") {
		text = strings.TrimPrefix(text, `pre`)
	}
	// A call of a call as an argument is dynamic and never the same.
	if strings.HasPrefix(text, produce()) {
		text = strings.TrimPrefix(text, produce())
	}
	// The pattern takes no parentheses around an argument.
	if strings.HasPrefix(text, affix) {
		text = strings.TrimPrefix((text), affix)
	}
	if strings.HasPrefix(text, affix) {
		text = strings.TrimPrefix(text, (affix))
	}
	if strings.HasPrefix((text), affix) {
		text = text[len(affix):]
	}
	if strings.HasPrefix(text, affix) {
		(text) = text[len(affix):]
	}
	_, _ = text, data
}

// A slice that cuts the second argument off the first is reported at the header, the prefix from the start and the
// suffix from the end.
func manualSlices(text string, affix string, data []byte, part []byte, texts []string, at int, value holder,
	pointer *holder, table map[string]string) {
	if strings.HasPrefix(text, affix) {
		text = text[len(affix):]
	}
	if strings.HasSuffix(text, affix) {
		text = text[:len(text)-len(affix)]
	}
	// A low index of a constant zero, in any spelling, is the default.
	if strings.HasSuffix(text, affix) {
		text = text[0 : len(text)-len(affix)]
	}
	if strings.HasSuffix(text, affix) {
		text = text[zero : len(text)-len(affix)]
	}
	if strings.HasSuffix(text, affix) {
		text = text[0x0 : len(text)-len(affix)]
	}
	if bytes.HasPrefix(data, part) {
		data = data[len(part):]
	}
	if bytes.HasSuffix(data, part) {
		data = data[:len(data)-len(part)]
	}
	// A literal offset of the literal's length in bytes, for strings alone.
	if strings.HasPrefix(text, "pre") {
		text = text[3:]
	}
	if strings.HasPrefix(text, "pre") {
		text = text[0x3:]
	}
	if strings.HasPrefix(text, "é") {
		text = text[2:]
	}
	if strings.HasPrefix(text, "\303\251") {
		text = text[2:]
	}
	if strings.HasPrefix(text, `\n`) {
		text = text[2:]
	}
	if strings.HasPrefix(text, "\n") {
		text = text[1:]
	}
	if strings.HasSuffix(text, "post") {
		text = text[:len(text)-4]
	}
	if strings.HasPrefix(text, "pre") {
		text = text[len("pre"):]
	}
	// Selectors and index expressions compare by syntax.
	if strings.HasPrefix(value.text, affix) {
		value.text = value.text[len(affix):]
	}
	if strings.HasPrefix(pointer.inner.text, affix) {
		pointer.inner.text = pointer.inner.text[len(affix):]
	}
	if strings.HasPrefix(texts[at], affix) {
		texts[at] = texts[at][len(affix):]
	}
	if strings.HasPrefix(texts[at+1], texts[at]) {
		texts[at+1] = texts[at+1][len(texts[at]):]
	}
	if strings.HasPrefix(table["key"], affix) {
		table["key"] = table["key"][len(affix):]
	}
	if bytes.HasPrefix(value.data, part) {
		value.data = value.data[len(part):]
	}
	// The builtin len is found through parentheses around its name.
	if strings.HasPrefix(text, affix) {
		text = text[(len)(affix):]
	}
	_, _ = text, data
}

// Slices that cut something else, or keep more, are not reported.
func manualSliceNearMisses(text string, other string, affix string, data []byte, part []byte, texts []string,
	at int, value holder) {
	if strings.HasPrefix(text, affix) {
		text = text[len(affix):len(text)]
	}
	if strings.HasPrefix(text, affix) {
		text = text[:len(affix)]
	}
	if strings.HasPrefix(text, affix) {
		text = text[:]
	}
	if strings.HasPrefix(text, affix) {
		text = other[len(affix):]
	}
	if strings.HasPrefix(text, affix) {
		text = text[len(other):]
	}
	if strings.HasPrefix(text, affix) {
		text = text[(len(affix)):]
	}
	if strings.HasPrefix(text, affix) {
		text = text[len(affix)+0:]
	}
	if strings.HasSuffix(text, affix) {
		text = text[1 : len(text)-len(affix)]
	}
	if strings.HasSuffix(text, affix) {
		text = text[at : len(text)-len(affix)]
	}
	if strings.HasSuffix(text, affix) {
		text = text[:(len(text) - len(affix))]
	}
	if strings.HasSuffix(text, affix) {
		text = text[:len(other)-len(affix)]
	}
	if strings.HasSuffix(text, affix) {
		text = text[:len(text)-len(other)]
	}
	if strings.HasSuffix(text, affix) {
		text = text[len(affix):]
	}
	if strings.HasSuffix(text, affix) {
		text = text[:len(text)]
	}
	if strings.HasSuffix(text, affix) {
		text = text[:len(text)/len(affix)]
	}
	// Contains has no slice that stands for it.
	if strings.Contains(text, affix) {
		text = text[len(affix):]
	}
	if bytes.Contains(data, part) {
		data = data[:len(data)-len(part)]
	}
	// A third index keeps the capacity.
	if bytes.HasPrefix(data, part) {
		data = data[len(part):len(data):len(data)]
	}
	if bytes.HasSuffix(data, part) {
		data = data[: len(data)-len(part) : cap(data)]
	}
	// A literal offset needs a literal affix of its length, and strings.
	if strings.HasPrefix(text, prefixConstant) {
		text = text[3:]
	}
	if strings.HasPrefix(text, "pre") {
		text = text[2:]
	}
	if strings.HasPrefix(text, "é") {
		text = text[1:]
	}
	if strings.HasPrefix(text, "pre") {
		text = text[zero+3:]
	}
	if strings.HasSuffix(text, "post") {
		text = text[:len(text)-3]
	}
	if bytes.HasPrefix(data, []byte("pre")) {
		data = data[3:]
	}
	if bytes.HasPrefix(data, nil) {
		data = data[0:]
	}
	// An index of other syntax, or another field.
	if strings.HasPrefix(texts[at], affix) {
		texts[at] = texts[at+0][len(affix):]
	}
	if strings.HasPrefix(value.text, affix) {
		value.text = value.inner.text[len(affix):]
	}
	_, _ = text, data
}

// The body must be one plain assignment to the guard's first argument, with no initializer and no else.
func shapesOfTheStatement(text string, other string, affix string) {
	if strings.HasPrefix(text, affix) {
		text += text[len(affix):]
	}
	if strings.HasPrefix(text, affix) {
		text := text[len(affix):]
		_ = text
	}
	if strings.HasPrefix(text, affix) {
		text, other = text[len(affix):], other
	}
	if strings.HasPrefix(text, affix) {
		other = text[len(affix):]
	}
	if strings.HasPrefix(text, affix) {
		text = text[len(affix):]
		text = text[len(affix):]
	}
	if strings.HasPrefix(text, affix) {
	}
	if trimmed := affix; strings.HasPrefix(text, trimmed) {
		text = text[len(trimmed):]
	}
	if strings.HasPrefix(text, affix) {
		text = text[len(affix):]
	} else {
		text = ""
	}
	// An empty else is still an else.
	if strings.HasPrefix(text, affix) {
		text = text[len(affix):]
	} else {
	}
	// An else if is one branch of a chain, and is left alone, at every depth.
	if text == "" {
		text = other
	} else if strings.HasPrefix(text, affix) {
		text = text[len(affix):]
	}
	if text == "" {
		text = other
	} else if text == affix {
		text = other
	} else if strings.HasPrefix(text, affix) {
		text = strings.TrimPrefix(text, affix)
	}
	// An if with an initializer is turned away before its else is recorded, so the else if after it is judged on
	// its own, and reported.
	if length := len(text); length == 0 {
		text = other
	} else if strings.HasPrefix(text, affix) {
		text = text[len(affix):]
	}
	// A guard nested in a branch is judged on its own.
	if text != "" {
		if strings.HasPrefix(text, affix) {
			text = text[len(affix):]
		}
	} else {
		if strings.HasSuffix(text, affix) {
			text = strings.TrimSuffix(text, affix)
		}
	}
	trim := func(text string) string {
		if strings.HasPrefix(text, affix) {
			text = text[len(affix):]
		}
		return text
	}
	_, _ = trim, other
}

// The condition must be a call of the guard itself, as written.
func shapesOfTheCondition(text string, affix string) {
	if !strings.HasPrefix(text, affix) {
		text = text[len(affix):]
	}
	if strings.HasPrefix(text, affix) && text != "" {
		text = text[len(affix):]
	}
	hasPrefix := strings.HasPrefix
	if hasPrefix(text, affix) {
		text = text[len(affix):]
	}
	if strings.EqualFold(text, affix) {
		text = text[len(affix):]
	}
	if strings.HasPrefix(pair()) {
		text = text[len(affix):]
	}
	// A len of the function's own is not the builtin.
	len := func(string) int { return 0 }
	if strings.HasPrefix(text, affix) {
		text = text[len(affix):]
	}
	_ = text
}
