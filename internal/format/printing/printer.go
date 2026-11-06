package printing

import (
	"fmt"
	"slices"

	"github.com/system-inc/cohere/internal/format/doc"
)

// PrintFunc is upstream's mainPrint as a printer receives it: print the node at selector, a property name
// of the current node, or the current node itself for "". Upstream's selector may also be a list of names;
// a printer here walks those through Call or CallElement and prints "" inside, which is the same Call
// mainPrint runs. args is the language's own arguments to a child's print; a non-nil args is never
// cached, as upstream caches only a print without them.
type PrintFunc[A any] func(selector string, arguments *A) doc.Doc

// NoArguments is the args of a language whose printer passes none.
type NoArguments struct{}

// CommentContext is upstream's decorated comment, what the ownLine, endOfLine and remaining handlers
// receive when the printer sets AvoidAstMutation, and what attachment builds either way.
type CommentContext[N Node[N], S any, A any] struct {
	Comment       N
	Text          string
	Options       *Options[N, S, A]
	Ast           N
	IsLastComment bool
	Placement     string

	Enclosing, Preceding, Following          N
	HasEnclosing, HasPreceding, HasFollowing bool
}

// CommentHandlers are upstream's printer.handleComments. Each returns true when it attached the
// comment itself, and false to let attachment's default placement run.
type CommentHandlers[N Node[N], S any, A any] struct {
	OwnLine   func(*CommentContext[N, S, A]) bool
	EndOfLine func(*CommentContext[N, S, A]) bool
	Remaining func(*CommentContext[N, S, A]) bool
}

// Embed is upstream's printer.embed result, made synchronous: given the path, a printer either returns
// nil (nothing embedded here) or a function that produces the embedded doc. The function receives
// TextToDoc to format another language's text.
type Embed[N Node[N], S any, A any] func(
	path *AstPath[N, S],
	options *Options[N, S, A],
) func(textToDoc TextToDoc, print PrintFunc[A], path *AstPath[N, S], options *Options[N, S, A]) (doc.Doc, error)

// TextToDoc formats text in another language and returns its doc, upstream's textToDoc. The parser
// name is upstream's (typescript, babel, json, graphql, css, markdown). Supplied by whoever knows every
// language, which is internal/format/native, because the print core cannot import the printers that
// import it.
type TextToDoc func(text string, parser string) (doc.Doc, error)

// Printer is upstream's printer object: what a language supplies to the core. Every field but Print,
// LocStart, LocEnd and VisitorKeys is optional, and a nil one means what upstream's absent one means.
//
// S is the language's per-print state, Options.Settings, and A its arguments to a child's print.
type Printer[N Node[N], S any, A any] struct {
	Print func(path *AstPath[N, S], options *Options[N, S, A], print PrintFunc[A], arguments *A) doc.Doc

	// LocStart and LocEnd are upstream's options.locStart and locEnd: byte offsets, with the
	// language's own overrides.
	LocStart func(node N) int
	LocEnd   func(node N) int

	// VisitorKeys is upstream's getVisitorKeys: the child properties of a node, in upstream's order.
	VisitorKeys func(node N) []string

	// MayHoldEmbed, when set, says whether a tree could hold a node Embed answers for. A tree that cannot
	// skips the embed walk, which visits every node through the path and was 7.6M of the JavaScript
	// printer's allocations on ahra (#fyw36kf). It may answer true for a tree with none; never false for one.
	MayHoldEmbed func(root N) bool

	Embed                Embed[N, S, A]
	EmbedVisitorKeys     func(node N) []string
	Preprocess           func(ast N, options *Options[N, S, A]) N
	HasPrettierIgnore    func(path *AstPath[N, S]) bool
	PrintPrettierIgnored func(path *AstPath[N, S], options *Options[N, S, A], print PrintFunc[A], arguments *A) doc.Doc
	WillPrintOwnComments func(path *AstPath[N, S], options *Options[N, S, A]) bool

	CanAttachComment     func(node N, ancestors []N) bool
	IsBlockComment       func(comment N) bool
	PrintComment         func(path *AstPath[N, S], options *Options[N, S, A]) doc.Doc
	GetCommentChildNodes func(node N, options *Options[N, S, A]) ([]N, bool)
	IsGap                func(gap string, options *Options[N, S, A]) (bool, bool)
	HandleComments       CommentHandlers[N, S, A]

	// AvoidAstMutation is upstream's features.experimental_avoidAstMutation, which decides whether the
	// comment handlers receive a CommentContext. Here they always do; the flag is kept so a printer port
	// can say which upstream mode it is porting.
	AvoidAstMutation bool

	// TemplateQuasis exists because upstream's language-independent attacher special-cases
	// TemplateLiteral and reads its quasis, so comments inside one template expression do not move to
	// another. That is JavaScript knowledge in a generic place; here it is a hook the JavaScript printer
	// supplies and every other language leaves nil.
	TemplateQuasis func(node N) ([]N, bool)
}

// PrintAstToDoc is upstream's printAstToDoc, src/main/ast-to-doc.js: attach comments, preprocess,
// resolve embedded languages, then print from the root, wrapping each node's doc in its comments.
//
// Cursor handling is not ported. cohere formats whole files and never tracks a cursor.
//
// Mutates: options the print's comments and printed-comment set live on it, for the printers that read them
func PrintAstToDoc[N Node[N], S any, A any](ast N, comments []N, options *Options[N, S, A]) (doc.Doc, error) {
	options.Comments = comments
	if options.printedComments == nil {
		options.printedComments = map[N]bool{}
	}

	AttachComments(ast, comments, options)
	root := ast
	if options.Printer.Preprocess != nil {
		root = options.Printer.Preprocess(root, options)
	}

	cache := make(map[N]doc.Doc, options.NodeCount)
	path := NewAstPath(root, options.Settings)
	embeds := map[N]doc.Doc{}

	var mainPrint PrintFunc[A]
	mainPrintInternal := func(arguments *A) doc.Doc {
		ensurePrintingNode(path, options)
		value := path.Value()
		if value.IsNil() {
			return doc.Text("")
		}
		node, isNode := value.Node()
		shouldCache := isNode && arguments == nil
		if shouldCache {
			if cached, present := cache[node]; present {
				return cached
			}
		}
		printed := callPluginPrintFunction(path, options, mainPrint, arguments, embeds)
		if shouldCache {
			cache[node] = printed
		}
		return printed
	}
	mainPrint = func(selector string, arguments *A) doc.Doc {
		if selector == "" {
			return mainPrintInternal(arguments)
		}
		return Call(path, func(*AstPath[N, S]) doc.Doc { return mainPrintInternal(arguments) }, selector)
	}

	printEmbeddedLanguages(path, mainPrint, options, embeds)

	printed := callPluginPrintFunction(path, options, mainPrint, nil, embeds)
	if ensureAllCommentsPrintedError := ensureAllCommentsPrinted(options); ensureAllCommentsPrintedError != nil {
		return nil, ensureAllCommentsPrintedError
	}
	return printed, nil
}

// callPluginPrintFunction is upstream's callPluginPrintFunction, without cursor labels.
func callPluginPrintFunction[N Node[N], S any, A any](
	path *AstPath[N, S],
	options *Options[N, S, A],
	print PrintFunc[A],
	arguments *A,
	embeds map[N]doc.Doc,
) doc.Doc {
	node, _ := path.Node()
	printer := options.Printer

	var printed doc.Doc
	switch {
	case printer.HasPrettierIgnore != nil && printer.HasPrettierIgnore(path):
		printed = printIgnored(path, options, print, arguments)
	default:
		if embedded, present := embeds[node]; present {
			printed = embedded
		} else {
			printed = printer.Print(path, options, print, arguments)
		}
	}

	// JSXElement prints its own comments, because it adds parentheses around them; that is what
	// WillPrintOwnComments is for.
	if printer.PrintComment != nil && len(node.CommentData().Comments) > 0 &&
		(printer.WillPrintOwnComments == nil || !printer.WillPrintOwnComments(path, options)) {
		printed = PrintComments(path, printed, options, nil)
	}
	return printed
}

// printIgnored is upstream's printIgnored, src/main/print-ignored.js: a prettier-ignore'd node prints as
// its original text, and every comment inside it counts as printed.
func printIgnored[N Node[N], S any, A any](
	path *AstPath[N, S],
	options *Options[N, S, A],
	print PrintFunc[A],
	arguments *A,
) doc.Doc {
	node, _ := path.Node()
	start := options.Printer.LocStart(node)
	end := options.Printer.LocEnd(node)
	for _, comment := range options.Comments {
		if options.Printer.LocStart(comment) >= start && options.Printer.LocEnd(comment) <= end {
			options.MarkPrinted(comment)
		}
	}
	if options.Printer.PrintPrettierIgnored != nil {
		return options.Printer.PrintPrettierIgnored(path, options, print, arguments)
	}
	return doc.Text(options.OriginalText[start:end])
}

// ensurePrintingNode is upstream's createPrintPreCheckFunction: print() may only be called on a
// property the parent's visitor keys name. Upstream checks this outside production builds; it is kept
// here because a printer port calling print on a non-node property is a bug worth a loud stop.
func ensurePrintingNode[N Node[N], S any, A any](path *AstPath[N, S], options *Options[N, S, A]) {
	if path.IsRoot() {
		return
	}
	key, hasKey := path.Key()
	parent, hasParent := path.Parent()
	if !hasKey || !hasParent {
		return
	}
	if slices.Contains(options.Printer.VisitorKeys(parent), key) {
		return
	}
	// Invariant: a printer port calls print only on a property its visitor keys name, as Prettier's printers do
	// and its development build checks here, so a stop is a bug in the port, never something in the input.
	panic(fmt.Sprintf("printing: print() called on %q of a %s, which is not one of its visitor keys %v",
		key, parent.Type(), options.Printer.VisitorKeys(parent)))
}

// printEmbeddedLanguages is upstream's printEmbeddedLanguages, src/main/multiparser.js, made
// synchronous.
//
// An embed that fails is dropped and the node prints normally, as upstream's try/catch drops it. That
// is observable: a fenced code block that does not parse prints as written rather than failing the file.
//
// Mutates: path each embed runs with the path at its node, and the stack is back where it was on return
// Mutates: embeds every embed that printed is stored by node, for callPluginPrintFunction to print in its place
func printEmbeddedLanguages[N Node[N], S any, A any](
	path *AstPath[N, S],
	print PrintFunc[A],
	options *Options[N, S, A],
	embeds map[N]doc.Doc,
) {
	if options.EmbeddedLanguageFormatting != "" && options.EmbeddedLanguageFormatting != "auto" {
		return
	}
	printer := options.Printer
	if printer.Embed == nil {
		return
	}
	if printer.MayHoldEmbed != nil {
		if root, isNode := path.Node(); isNode && !printer.MayHoldEmbed(root) {
			return
		}
	}
	visitorKeys := printer.EmbedVisitorKeys
	if visitorKeys == nil {
		visitorKeys = printer.VisitorKeys
	}

	type pending struct {
		print func(TextToDoc, PrintFunc[A], *AstPath[N, S], *Options[N, S, A]) (doc.Doc, error)
		node  N
		stack []Frame[N]
	}
	var calls []pending

	// The callback Call takes is made once, rather than once per key of every node.
	var recurse func(*AstPath[N, S], int)
	recurseInto := func(inner *AstPath[N, S]) struct{} { recurse(inner, 0); return struct{}{} }
	recurse = func(current *AstPath[N, S], _ int) {
		value := current.Value()
		node, isNode := value.Node()
		if value.IsNil() || !isNode || (printer.HasPrettierIgnore != nil && printer.HasPrettierIgnore(current)) {
			return
		}
		for _, key := range visitorKeys(node) {
			if node.Field(key).IsList() {
				current.Each(recurse, key)
			} else {
				Call(current, recurseInto, key)
			}
		}
		if result := printer.Embed(current, options); result != nil {
			calls = append(calls, pending{print: result, node: node, stack: append([]Frame[N](nil), current.stack...)})
		}
	}
	recurse(path, 0)

	original := path.stack
	for _, call := range calls {
		path.stack = call.stack
		textToDoc := options.TextToDoc
		if textToDoc == nil {
			textToDoc = func(string, string) (doc.Doc, error) {
				return nil, fmt.Errorf("printing: no TextToDoc is configured for embedded languages")
			}
		}
		embedded, printError := call.print(textToDoc, print, path, options)
		if printError == nil && embedded != nil {
			embeds[call.node] = embedded
		}
	}
	path.stack = original
}
