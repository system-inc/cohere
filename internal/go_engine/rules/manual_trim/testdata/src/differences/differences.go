package differences

import (
	"bytes"
	"strings"
)

type wrapper struct {
	embedded
}

type embedded struct {
	text string
}

// A type parameter whose type set is byte slices passes to the bytes guard and slices to itself, and the trimming
// call's result assigns back to it, so both forms are reported.
func typeParameter[Data ~[]byte](data Data, part []byte) Data {
	if bytes.HasPrefix(data, part) {
		data = data[len(part):]
	}
	if bytes.HasSuffix(data, part) {
		data = bytes.TrimSuffix(data, part)
	}
	return data
}

// A promoted field and the field through its embedding are two spellings, so not the same.
func promotedField(value wrapper, affix string) {
	if strings.HasPrefix(value.text, affix) {
		value.text = value.embedded.text[len(affix):]
	}
	if strings.HasPrefix(value.embedded.text, affix) {
		value.embedded.text = value.embedded.text[len(affix):]
	}
	_ = value
}

// Literal offsets of any kind compare by the value the type checker gives them.
func literalOffsets(text string) {
	if strings.HasPrefix(text, "pre") {
		text = text[3.0:]
	}
	if strings.HasPrefix(text, "pre") {
		text = text['\x03':]
	}
	if strings.HasSuffix(text, "post") {
		text = text[:len(text)-4.0]
	}
	if strings.HasSuffix(text, "post") {
		text = text[0.0 : len(text)-4]
	}
	_ = text
}

// An initializer turns its statement away before its else is recorded. In the first chain the else if after it has an
// else of its own, so neither is reported; in the second, the else if after the one with an initializer is the last,
// unrecorded, and reported.
func chains(text string, affix string, ready bool) {
	if length := len(text); length == 0 {
		text = affix
	} else if strings.HasPrefix(text, affix) {
		text = text[len(affix):]
	} else if strings.HasSuffix(text, affix) {
		text = text[:len(text)-len(affix)]
	}
	if text == "" {
		text = affix
	} else if found := strings.HasPrefix(text, affix); found {
		text = text[len(affix):]
	} else if strings.HasSuffix(text, affix) {
		text = text[:len(text)-len(affix)]
	}
	// A guard nested in an else if's body is judged on its own.
	if text == "" {
		text = affix
	} else if ready {
		if strings.HasPrefix(text, affix) {
			text = text[len(affix):]
		}
	}
	_ = text
}

// A labeled guard, and guards in clauses and literals, are if statements like any other.
func placements(text string, affix string, choice int, channel chan int) {
labeled:
	if strings.HasPrefix(text, affix) {
		text = text[len(affix):]
	}
	switch choice {
	case 0:
		if strings.HasSuffix(text, affix) {
			text = strings.TrimSuffix(text, affix)
		}
	}
	select {
	case <-channel:
		if strings.Contains(text, affix) {
			text = strings.Replace(text, affix, "", 1)
		}
	default:
	}
	func() {
		if strings.HasPrefix(text, affix) {
			text = text[len(affix):]
		}
	}()
	if choice > 0 {
		goto labeled
	}
}
