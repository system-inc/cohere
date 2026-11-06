package estree

// StrippedText is a file's text with its comments blanked, StripComments, made the first time it is asked
// for. The postprocess and the printer both strip the file's text of the file's comments: the same string,
// and the same list, merged before either reads it and never moved after, so they share one strip rather
// than make two copies of the file, 37 MB apiece on a cold run on ahra (#wcgw0n4).
type StrippedText struct {
	text     string
	comments []*Node
	stripped string
	made     bool
}

// Text is the stripped text.
func (stripped *StrippedText) Text() string {
	if !stripped.made {
		stripped.stripped = StripComments(stripped.text, stripped.comments)
		stripped.made = true
	}
	return stripped.stripped
}
