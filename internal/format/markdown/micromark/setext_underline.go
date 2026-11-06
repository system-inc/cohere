package micromark

// setextUnderline is micromark-core-commonmark/lib/setext-underline.js.
var setextUnderline = &Construct{
	Name:      "setextUnderline",
	ResolveTo: &Resolver{Resolve: resolveToSetextUnderline},
	Tokenize:  tokenizeSetextUnderline,
}

// resolveToSetextUnderline turns the content before the underline into a heading.
//
// Upstream mutates the events array in place and keeps indices recorded before a splice unadjusted; the
// port does the same. Index 0 stands for undefined in the `!definition` and `if (definition)` tests, as
// it would in JavaScript, where 0 is falsy too.
func resolveToSetextUnderline(events []Event, context *TokenizeContext) []Event {
	// To do: resolve like `markdown-rs`.
	resolvedEvents := events
	index := len(resolvedEvents)
	var content int
	var text int
	var definition int

	// Find the opening of the content.
	// It’ll always exist: we don’t tokenize if it isn’t there.
	for index > 0 {
		index--
		if resolvedEvents[index].Enter {
			if resolvedEvents[index].Token.Type == TypeContent {
				content = index
				break
			}
			if resolvedEvents[index].Token.Type == TypeParagraph {
				text = index
			}
		} else {
			// Exit
			if resolvedEvents[index].Token.Type == TypeContent {
				// Remove the content end (if needed we’ll add it later)
				resolvedEvents = spliceEvents(resolvedEvents, index, 1, nil)
			}
			// After the splice above, events[index] is the event that followed the removed one, as it is
			// upstream.
			if definition == 0 && resolvedEvents[index].Token.Type == TypeDefinition {
				definition = index
			}
		}
	}

	// Upstream spreads the points into fresh objects; Points are values here.
	heading := &Token{
		Type:  TypeSetextHeading,
		Start: resolvedEvents[content].Token.Start,
		End:   resolvedEvents[len(resolvedEvents)-1].Token.End,
	}

	// Change the paragraph to setext heading text.
	resolvedEvents[text].Token.Type = TypeSetextHeadingText

	// If we have definitions in the content, we’ll keep on having content,
	// but we need move it.
	if definition != 0 {
		resolvedEvents = spliceEvents(resolvedEvents, text, 0, []Event{{Enter: true, Token: heading, Context: context}})
		resolvedEvents = spliceEvents(
			resolvedEvents,
			definition+1,
			0,
			[]Event{{Enter: false, Token: resolvedEvents[content].Token, Context: context}},
		)
		resolvedEvents[content].Token.End = resolvedEvents[definition].Token.End
	} else {
		resolvedEvents[content].Token = heading
	}

	// Add the heading exit at the end.
	resolvedEvents = append(resolvedEvents, Event{Enter: false, Token: heading, Context: context})
	return resolvedEvents
}

func tokenizeSetextUnderline(self *Self, effects *Effects, ok State, nok State) State {
	var marker Code

	var start State
	var before State
	var inside State
	var after State

	// At start of heading (setext) underline.
	//
	//	  | aa
	//	> | ==
	//	    ^
	start = func(code Code) State {
		index := len(self.Events)
		var paragraph bool
		// Find an opening.
		for index > 0 {
			index--
			// Skip enter/exit of line ending, line prefix, and content.
			// We can now either have a definition or a paragraph.
			tokenType := self.Events[index].Token.Type
			if tokenType != TypeLineEnding && tokenType != TypeLinePrefix && tokenType != TypeContent {
				paragraph = tokenType == TypeParagraph
				break
			}
		}

		// To do: handle lazy/pierce like `markdown-rs`.
		// To do: parse indent like `markdown-rs`.
		if !self.Parser.Lazy[self.Now().Line] && (self.IsInterrupt() || paragraph) {
			effects.Enter(TypeSetextHeadingLine, nil)
			marker = code
			return before(code)
		}
		return nok(code)
	}

	// After optional whitespace, at `-` or `=`.
	//
	//	  | aa
	//	> | ==
	//	    ^
	before = func(code Code) State {
		effects.Enter(TypeSetextHeadingLineSequence, nil)
		return inside(code)
	}

	// In sequence.
	//
	//	  | aa
	//	> | ==
	//	    ^
	inside = func(code Code) State {
		if code == marker {
			effects.Consume(code)
			return inside
		}
		effects.Exit(TypeSetextHeadingLineSequence)
		if markdownSpace(code) {
			return factorySpace(effects, after, TypeLineSuffix, 0)(code)
		}
		return after(code)
	}

	// After sequence, after optional whitespace.
	//
	//	  | aa
	//	> | ==
	//	      ^
	after = func(code Code) State {
		if code == CodeEof || markdownLineEnding(code) {
			effects.Exit(TypeSetextHeadingLine)
			return ok(code)
		}
		return nok(code)
	}

	return start
}
