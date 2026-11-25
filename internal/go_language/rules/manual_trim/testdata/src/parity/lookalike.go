package parity

import (
	"lookalike"
	standard "strings"
)

// The guards are named by the import path of their package, so the package lookalike, which calls itself strings, is
// none of them, and the standard library's strings under another name is.
func spelledAlike(text string, affix string) {
	if strings.HasPrefix(text, affix) {
		text = strings.TrimPrefix(text, affix)
	}
	if strings.HasPrefix(text, affix) {
		text = text[len(affix):]
	}
	if strings.Contains(text, affix) {
		text = strings.Replace(text, affix, "", -1)
	}
	// The real guard with the lookalike's call, and the lookalike's guard with the real call, are not pairs.
	if standard.HasPrefix(text, affix) {
		text = strings.TrimPrefix(text, affix)
	}
	if strings.HasSuffix(text, affix) {
		text = standard.TrimSuffix(text, affix)
	}
	// The renamed import is reported, its messages naming the package by its path.
	if standard.HasPrefix(text, affix) {
		text = standard.TrimPrefix(text, affix)
	}
	if standard.HasSuffix(text, affix) {
		text = text[:len(text)-len(affix)]
	}
	_ = text
}
