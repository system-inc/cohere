package doc

// indentations is one Print's indentations, each built once from its base and command. An indentation is
// immutable, and one print's options are fixed, so the same base and command always build the same one;
// building it again for every indent and align the printer met was 3.0M of the JavaScript printer's
// allocations on ahra (#fyw36kf). Per print rather than global, because the options differ between prints.
type indentations map[indentKey]*indentation

// generate is generateIndent, from the print's indentations when this base and command were built before.
func (built indentations) generate(base *indentation, command indentCommand, options Options) *indentation {
	key := indentKey{base: base, command: command}
	if known, present := built[key]; present {
		return known
	}
	generated := generateIndent(base, command, options)
	built[key] = generated
	return generated
}
