package printing

// Options is upstream's options object as the core and printers read it.
type Options[N Node[N]] struct {
	Printer      *Printer[N]
	OriginalText string

	// Settings is the language's own options (formatoptions.Options for every printer today), carried as a
	// value the language type-asserts, so this package does not import the oracle engine's package.
	Settings any

	// EmbeddedLanguageFormatting is upstream's option: "auto" (the default) or "off".
	EmbeddedLanguageFormatting string

	// TextToDoc formats embedded text in another language. Nil means no embedding is possible.
	TextToDoc TextToDoc

	// Comments is every comment in the file, upstream's options[commentsPropertyInOptions].
	Comments []N

	// NodeCount is about how many nodes the tree holds, when the language knows it cheaply, or zero. The
	// print cache, one entry per node printed, starts with room for that many rather than growing by
	// insertion, which was 124 + 79 MB of a cold run on ahra (#wcgw0n4). It never changes the output.
	NodeCount int

	// printedComments is upstream's options[Symbol.for("printedComments")]: comments printed by a
	// printer directly or covered by a prettier-ignore, which ensureAllCommentsPrinted must not flag.
	printedComments map[N]bool
}

// MarkPrinted records a comment as printed outside printComments, upstream's printedComments.add.
func (options *Options[N]) MarkPrinted(comment N) {
	if options.printedComments == nil {
		options.printedComments = map[N]bool{}
	}
	options.printedComments[comment] = true
}
