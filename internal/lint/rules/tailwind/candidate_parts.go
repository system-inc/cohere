package tailwind

// candidateParts is a class split into the pieces that decide whether it can merge.
type candidateParts struct {
	ClassName string
	// Prefix is everything before the root: the variants, in source order.
	Prefix string
	Root   string
	// Value is the text after the root, empty for a class like `border-l`.
	//
	// Decoded rather than verbatim, because it decides equality: `parseCandidate` resolves
	// `bg-(--x)` and `bg-[var(--x)]` to the same value, and they really are the same class, so the merge
	// precondition must see them as equal. SourceValue is the half that gets printed.
	Value string
	// SourceValue is the same value as the author spelled it, for printing back.
	//
	// Two fields rather than one because the decoded form is unwritable. `grid-cols-[1fr_auto]`
	// decodes to `1fr auto`, and a rewrite suggesting `grid-cols-[1fr auto]` names a class with a
	// space in it, which cannot appear in a class attribute at all. Measured on the corpus: 16
	// classes decode to something different from what was written, all of them arbitrary values
	// carrying `_` or the `(--x)` custom-property shorthand.
	//
	// Empty means "same as Value", which is the common case and keeps every caller that builds these
	// by hand correct by default.
	SourceValue string
	Important   bool
}

// mergeKey is everything except the root. Two classes can only merge when this matches.
func (parts candidateParts) mergeKey() string {
	importance := ""
	if parts.Important {
		importance = "!"
	}
	return parts.Prefix + "|" + importance + "|" + parts.Value
}

// printedValue is the value to write into a rebuilt class name.
//
// SourceValue when the decoded form differs from what the author wrote, Value otherwise. See the
// field comment: the decoded form is what decides equality and is not always writable.
func (parts candidateParts) printedValue() string {
	if parts.SourceValue != "" {
		return parts.SourceValue
	}
	return parts.Value
}
