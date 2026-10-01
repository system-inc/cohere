package micromark

import (
	"slices"
	"unicode/utf16"
)

// createTokenizer is upstream's createTokenizer, micromark/lib/create-tokenizer.js.
//
// from sets the point before the first character; later lines are indented with defineSkip.
func createTokenizer(parser *ParseContext, initialize *InitialConstruct, from *Point) *TokenizeContext {
	context := &TokenizeContext{
		Previous:       CodeEof,
		ContainerState: &ContainerState{},
		Parser:         parser,
		columnStart:    map[int]int{},
		consumed:       true,
		initialize:     initialize,
		point:          Point{bufferIndex: -1, index: 0, Line: 1, Column: 1, Offset: 0},
	}
	if from != nil {
		if from.Line != 0 {
			context.point.Line = from.Line
		}
		if from.Column != 0 {
			context.point.Column = from.Column
		}
		context.point.Offset = from.Offset
	}

	context.effects = &Effects{
		Attempt:   context.constructFactory(context.onSuccessfulConstruct, false),
		Check:     context.constructFactory(context.onSuccessfulCheck, false),
		Consume:   context.consume,
		Enter:     context.enter,
		Exit:      context.exit,
		Interrupt: context.constructFactory(context.onSuccessfulCheck, true),
	}

	context.state = initialize.Tokenize(&Self{TokenizeContext: context}, context.effects)

	if initialize.ResolveAll != nil {
		context.resolveAllConstructs = append(context.resolveAllConstructs, resolvable{initialize: initialize})
	}

	return context
}

// Write feeds chunks to the tokenizer. Upstream's context.write: events come back once the last chunk is
// the end of input, and an empty list before that.
func (context *TokenizeContext) Write(slice []Chunk) []Event {
	context.chunks = pushChunks(context.chunks, slice)

	context.main()

	// Exit if we’re not done, resolve might change stuff.
	if last := context.chunks[len(context.chunks)-1]; last.IsText || last.Code != CodeEof {
		return nil
	}

	context.addResult(resolvable{initialize: context.initialize}, 0)

	// Otherwise, resolve, and exit.
	context.Events = resolveAllEntries(context.resolveAllConstructs, context.Events, context)

	return context.Events
}

// SliceSerialize is the source text of a token.
func (context *TokenizeContext) SliceSerialize(token *Token, expandTabs bool) string {
	return serializeChunks(context.SliceStream(token), expandTabs)
}

// SliceStream is the chunks of a token.
func (context *TokenizeContext) SliceStream(token *Token) []Chunk {
	return sliceChunks(context.chunks, token)
}

// Now is the current point.
func (context *TokenizeContext) Now() Point {
	return context.point
}

// DefineSkip records that lines starting at value's line start at value's column.
func (context *TokenizeContext) DefineSkip(value Point) {
	context.columnStart[value.Line] = value.Column
	context.accountForPotentialSkip()
}

// main is the main loop. `index` and `bufferIndex` in point are modified by consume.
func (context *TokenizeContext) main() {
	for context.point.index < len(context.chunks) {
		chunk := context.chunks[context.point.index]

		// If we’re in a buffer chunk, loop through it.
		if chunk.IsText {
			chunkIndex := context.point.index

			if context.point.bufferIndex < 0 {
				context.point.bufferIndex = 0
			}

			for context.point.index == chunkIndex && context.point.bufferIndex < len(chunk.Text) {
				context.goCode(Code(chunk.Text[context.point.bufferIndex]))
			}
		} else {
			context.goCode(chunk.Code)
		}
	}
}

// goCode deals with one code. Upstream's go.
//
// Upstream's development build asserts that the previous code was consumed. The production build, which
// the oracle runs, does not, and the fork's liquid construct depends on that: its mayClose returns a
// state without consuming, so the same code is fed again to the state it returned. So neither consume
// check is ported; the remaining panics guard invariants whose failure is a porting bug.
func (context *TokenizeContext) goCode(code Code) {
	context.consumed = false
	context.expectedCode = code
	if context.state == nil {
		panic("micromark: expected state")
	}
	context.state = context.state(code)
}

func (context *TokenizeContext) consume(code Code) {
	if code != context.expectedCode {
		panic("micromark: expected given code to equal expected code")
	}

	if markdownLineEnding(code) {
		context.point.Line++
		context.point.Column = 1
		if code == CodeCarriageReturnLineFeed {
			context.point.Offset += 2
		} else {
			context.point.Offset++
		}
		context.accountForPotentialSkip()
	} else if code != CodeVirtualSpace {
		context.point.Column++
		context.point.Offset++
	}

	// Not in a string chunk.
	if context.point.bufferIndex < 0 {
		context.point.index++
	} else {
		context.point.bufferIndex++

		// At end of string chunk.
		if context.point.bufferIndex == len(context.chunks[context.point.index].Text) {
			context.point.bufferIndex = -1
			context.point.index++
		}
	}

	// Expose the previous character.
	context.Previous = code

	// Mark as consumed.
	context.consumed = true
}

func (context *TokenizeContext) enter(tokenType string, token *Token) *Token {
	if token == nil {
		token = &Token{}
	}
	token.Type = tokenType
	token.Start = context.Now()

	context.Events = append(context.Events, Event{Enter: true, Token: token, Context: context})

	context.stack = append(context.stack, token)

	return token
}

func (context *TokenizeContext) exit(tokenType string) *Token {
	if len(context.stack) == 0 {
		panic("micromark: cannot close w/o open tokens")
	}
	token := context.stack[len(context.stack)-1]
	context.stack = context.stack[:len(context.stack)-1]
	token.End = context.Now()

	if tokenType != token.Type {
		panic("micromark: expected exit token to match current token, got " + tokenType + " for " + token.Type)
	}

	context.Events = append(context.Events, Event{Enter: false, Token: token, Context: context})

	return token
}

func (context *TokenizeContext) onSuccessfulConstruct(construct *Construct, info *storeInfo) {
	context.addResult(resolvable{construct: construct}, info.from)
}

func (context *TokenizeContext) onSuccessfulCheck(_ *Construct, info *storeInfo) {
	info.restore()
}

// constructFactory is upstream's constructFactory: attempt, check and interrupt differ in what happens on
// success and in whether constructs see `interrupt: true`.
func (context *TokenizeContext) constructFactory(onReturn func(*Construct, *storeInfo), interrupt bool) func(Constructs, State, State) State {
	return func(constructs Constructs, returnState State, bogusState State) State {
		var listOfConstructs []*Construct
		var constructIndex int
		var currentConstruct *Construct
		var info *storeInfo

		var handleConstruct func(construct *Construct) State
		var ok, nok State

		handleListOfConstructs := func(list []*Construct) State {
			listOfConstructs = list
			constructIndex = 0

			if len(list) == 0 {
				if bogusState == nil {
					panic("micromark: expected `bogusState` to be given")
				}
				return bogusState
			}

			return handleConstruct(list[constructIndex])
		}

		handleMapOfConstructs := func(record *ConstructRecord) State {
			return func(code Code) State {
				var list []*Construct
				if code != CodeEof {
					list = append(list, record.ByCode[code]...)
					list = append(list, record.Null...)
				}
				return handleListOfConstructs(list)(code)
			}
		}

		handleConstruct = func(construct *Construct) State {
			return func(code Code) State {
				info = context.store()
				currentConstruct = construct

				if !construct.Partial {
					context.CurrentConstruct = construct
				}

				if construct.Name != "" && slices.Contains(context.Parser.Constructs.Disable, construct.Name) {
					return nok(code)
				}

				self := &Self{TokenizeContext: context}
				if interrupt {
					self.isView = true
					self.viewInterrupt = true
				}
				return construct.Tokenize(self, context.effects, ok, nok)(code)
			}
		}

		ok = func(code Code) State {
			if code != context.expectedCode {
				panic("micromark: expected code")
			}
			context.consumed = true
			onReturn(currentConstruct, info)
			return returnState
		}

		nok = func(code Code) State {
			if code != context.expectedCode {
				panic("micromark: expected code")
			}
			context.consumed = true
			info.restore()

			constructIndex++
			if constructIndex < len(listOfConstructs) {
				return handleConstruct(listOfConstructs[constructIndex])
			}

			return bogusState
		}

		switch typed := constructs.(type) {
		case ConstructList:
			return handleListOfConstructs(typed)
		case *Construct:
			return handleListOfConstructs([]*Construct{typed})
		case *ConstructRecord:
			return handleMapOfConstructs(typed)
		default:
			panic("micromark: unknown constructs")
		}
	}
}

func (context *TokenizeContext) addResult(entry resolvable, from int) {
	construct := entry.construct
	resolveAll := entry.resolveAll()
	if resolveAll != nil && !slices.Contains(context.resolveAllConstructs, entry) {
		context.resolveAllConstructs = append(context.resolveAllConstructs, entry)
	}

	if construct != nil && construct.Resolve != nil {
		resolved := construct.Resolve.Resolve(slices.Clone(context.Events[from:]), context)
		context.Events = spliceEvents(context.Events, from, len(context.Events)-from, resolved)
	}

	if construct != nil && construct.ResolveTo != nil {
		context.Events = construct.ResolveTo.Resolve(context.Events, context)
	}
}

// storeInfo is upstream's Info: where events were when an attempt started, and how to go back.
type storeInfo struct {
	from    int
	restore func()
}

func (context *TokenizeContext) store() *storeInfo {
	startPoint := context.Now()
	startPrevious := context.Previous
	startCurrentConstruct := context.CurrentConstruct
	startEventsIndex := len(context.Events)
	startStack := slices.Clone(context.stack)

	return &storeInfo{from: startEventsIndex, restore: func() {
		context.point = startPoint
		context.Previous = startPrevious
		context.CurrentConstruct = startCurrentConstruct
		context.Events = context.Events[:startEventsIndex]
		context.stack = startStack
		context.accountForPotentialSkip()
	}}
}

// accountForPotentialSkip moves the current point a bit forward in the line when it’s on a column skip.
func (context *TokenizeContext) accountForPotentialSkip() {
	if column, defined := context.columnStart[context.point.Line]; defined && context.point.Column < 2 {
		context.point.Column = column
		context.point.Offset += column - 1
	}
}

// sliceChunks gets the chunks from a slice of chunks in the range of a token.
func sliceChunks(chunks []Chunk, token *Token) []Chunk {
	startIndex := token.Start.index
	startBufferIndex := token.Start.bufferIndex
	endIndex := token.End.index
	endBufferIndex := token.End.bufferIndex
	var view []Chunk

	if startIndex == endIndex {
		view = []Chunk{textChunk(chunks[startIndex].Text[startBufferIndex:endBufferIndex])}
	} else {
		view = slices.Clone(chunks[startIndex:endIndex])

		if startBufferIndex > -1 {
			head := view[0]
			if head.IsText {
				view[0] = textChunk(head.Text[startBufferIndex:])
			} else {
				view = view[1:]
			}
		}

		if endBufferIndex > 0 {
			view = append(view, textChunk(chunks[endIndex].Text[:endBufferIndex]))
		}
	}

	return view
}

// serializeChunks gets the string value of a slice of chunks.
func serializeChunks(chunks []Chunk, expandTabs bool) string {
	var result []uint16
	atTab := false

	for _, chunk := range chunks {
		if chunk.IsText {
			result = append(result, chunk.Text...)
		} else {
			switch chunk.Code {
			case CodeCarriageReturn:
				result = append(result, '\r')
			case CodeLineFeed:
				result = append(result, '\n')
			case CodeCarriageReturnLineFeed:
				result = append(result, '\r', '\n')
			case CodeHorizontalTab:
				if expandTabs {
					result = append(result, ' ')
				} else {
					result = append(result, '\t')
				}
			case CodeVirtualSpace:
				if !expandTabs && atTab {
					continue
				}
				result = append(result, ' ')
			default:
				// Currently only replacement character.
				result = append(result, uint16(chunk.Code))
			}
		}

		atTab = !chunk.IsText && chunk.Code == CodeHorizontalTab
	}

	return string(utf16.Decode(result))
}
