// Package micromark is micromark 4, the markdown tokenizer Prettier's markdown parser runs, ported to Go.
//
// The fork parses `.md` with mdast-util-from-markdown over micromark, with the GFM, math, wiki-link and
// liquid extensions and an html-text override (src/language-markdown/parse/parse-markdown.js). The printer
// slices the original text by the offsets this tokenizer records, so a CommonMark library that builds a
// similar tree is not a substitute: the tree has to be this tree, position for position.
//
// # A port, not a reimplementation
//
// Each file follows one upstream file, state for state, and names its source. The state machines are
// closures in JavaScript and closures here. Where JavaScript semantics leak into behavior, the Go copies
// the semantics rather than the intent:
//
//   - Text is UTF-16 code units, the way JavaScript strings are. Offsets and columns count code units, the
//     printer slices by them, and the character classes test single code units, so an astral symbol
//     beside an asterisk is two non-punctuation units here exactly as it is upstream.
//   - The end of input is JavaScript's null. Upstream compares it with < and >, where null is 0, and a
//     real NUL never reaches a tokenizer because preprocessing replaces it, so CodeEof is 0.
//   - The interrupt factory hands constructs an object whose prototype is the tokenizer context. Self
//     reproduces that binding: reads are live, and a write lands on the view when there is one.
package micromark

// Code is a character code: a UTF-16 code unit, or one of the negative virtual codes, or CodeEof.
type Code int

// State is one state of a state machine. A nil State is upstream's undefined: the machine is done.
type State func(code Code) State

// Point is a place in the source. Line and column are 1-based, offset is 0-based, all in UTF-16 code
// units. index and bufferIndex locate the point in the chunk list, as upstream's _index and _bufferIndex.
type Point struct {
	Line        int
	Column      int
	Offset      int
	index       int
	bufferIndex int
}

// Chunk is a string of code units, or a single code. Upstream's Chunk is `string | number`.
type Chunk struct {
	Text   []uint16
	Code   Code
	IsText bool
}

func textChunk(text []uint16) Chunk { return Chunk{Text: text, IsText: true} }
func codeChunk(code Code) Chunk     { return Chunk{Code: code} }

// Event is an enter or an exit of a token, in the context that produced it.
type Event struct {
	Enter   bool
	Token   *Token
	Context *TokenizeContext
}

// Resolver rewrites events. It is a pointer so that resolveAll can call each one once, the way upstream
// compares functions by identity: labelStartLink, labelStartImage and labelEnd share one resolveAll.
type Resolver struct {
	Resolve func(events []Event, context *TokenizeContext) []Event
}

// Construct is a piece of syntax.
type Construct struct {
	Name         string
	Tokenize     func(self *Self, effects *Effects, ok State, nok State) State
	Continuation *Construct
	Exit         func(self *Self, effects *Effects)
	Previous     func(self *Self, code Code) bool
	Resolve      *Resolver
	ResolveTo    *Resolver
	ResolveAll   *Resolver
	Partial      bool
	Concrete     bool
	// Add is "after" for a construct that goes after the existing ones when extensions combine.
	Add string
}

// InitialConstruct starts a content type. Only its tokenize and resolveAll are used.
type InitialConstruct struct {
	Tokenize   func(self *Self, effects *Effects) State
	ResolveAll *Resolver
}

// Constructs is what attempt, check and interrupt accept: one construct, a list, or a record.
type Constructs interface{ isConstructs() }

// ConstructList is a list of constructs tried in order.
type ConstructList []*Construct

func (*Construct) isConstructs()    {}
func (ConstructList) isConstructs() {}

// ContainerState is the state a container construct keeps across lines.
//
// Upstream's is an open record; these are every field a construct in our extension set writes.
type ContainerState struct {
	closeFlow         bool
	marker            Code
	size              int
	listType          string
	initialBlankLine  bool
	furtherBlankLines bool
	open              bool
}

// Extension is a syntax extension: per hook, constructs by code.
type Extension struct {
	Document       *ConstructRecord
	ContentInitial *ConstructRecord
	FlowInitial    *ConstructRecord
	Flow           *ConstructRecord
	String         *ConstructRecord
	Text           *ConstructRecord
	InsideSpan     []*Construct
	// AttentionMarkers are codes, upstream's attentionMarkers.null.
	AttentionMarkers []Code
	// Disable are construct names, upstream's disable.null.
	Disable []string
}

// FullConstructs is every hook after combining the defaults with the extensions. Combining yields an
// extension, because upstream's gfm() is itself combineExtensions over five, then combined again, and the
// order that two-level combination produces is observable.
type FullConstructs = Extension

// Content types, upstream's constants.contentType*.
const (
	ContentTypeContent  = "content"
	ContentTypeDocument = "document"
	ContentTypeFlow     = "flow"
	ContentTypeString   = "string"
	ContentTypeText     = "text"
)

// The constants upstream's micromark-util-symbol names, that constructs read.
const (
	attentionSideAfter                       = 2
	attentionSideBefore                      = 1
	atxHeadingOpeningFenceSizeMaximum        = 6
	autolinkDomainSizeMaximum                = 63
	autolinkSchemeSizeMaximum                = 32
	cdataOpeningString                       = "CDATA["
	characterGroupPunctuation                = 2
	characterGroupWhitespace                 = 1
	characterReferenceDecimalSizeMaximum     = 7
	characterReferenceHexadecimalSizeMaximum = 6
	characterReferenceNamedSizeMaximum       = 31
	codeFencedSequenceSizeMinimum            = 3
	hardBreakPrefixSizeMinimum               = 2
	htmlBasic                                = 6
	htmlCdata                                = 5
	htmlComment                              = 2
	htmlComplete                             = 7
	htmlDeclaration                          = 4
	htmlInstruction                          = 3
	htmlRawSizeMaximum                       = 8
	htmlRaw                                  = 1
	linkResourceDestinationBalanceMaximum    = 32
	linkReferenceSizeMaximum                 = 999
	listItemValueSizeMaximum                 = 10
	tabSize                                  = 4
	thematicBreakMarkerCountMinimum          = 3
)
