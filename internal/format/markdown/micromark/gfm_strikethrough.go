package micromark

import "slices"

// The extension's own token types.
const (
	typeStrikethrough                  = "strikethrough"
	typeStrikethroughSequence          = "strikethroughSequence"
	typeStrikethroughSequenceTemporary = "strikethroughSequenceTemporary"
	typeStrikethroughText              = "strikethroughText"
)

// gfmStrikethroughExtension is micromark-extension-gfm-strikethrough/lib/syntax.js, gfmStrikethrough().
//
// The fork's gfm() is called without options and passes none through, so singleTilde is upstream's
// default, true: one tilde strikes as well as two.
func gfmStrikethroughExtension() *Extension {
	single := true

	// Take events and resolve strikethrough.
	resolveAllStrikethrough := func(events []Event, context *TokenizeContext) []Event {
		resolvedEvents := events
		index := -1

		// Walk through all events.
		for index+1 < len(resolvedEvents) {
			index++

			// Find a token that can close.
			if resolvedEvents[index].Enter &&
				resolvedEvents[index].Token.Type == typeStrikethroughSequenceTemporary &&
				resolvedEvents[index].Token.close {
				open := index

				// Now walk back to find an opener. Upstream's `while (open--)` tests, then decrements.
				for open > 0 {
					open--

					// Find a token that can open the closer.
					if !resolvedEvents[open].Enter &&
						resolvedEvents[open].Token.Type == typeStrikethroughSequenceTemporary &&
						resolvedEvents[open].Token.open &&
						// If the sizes are the same:
						resolvedEvents[index].Token.End.Offset-resolvedEvents[index].Token.Start.Offset ==
							resolvedEvents[open].Token.End.Offset-resolvedEvents[open].Token.Start.Offset {
						resolvedEvents[index].Token.Type = typeStrikethroughSequence
						resolvedEvents[open].Token.Type = typeStrikethroughSequence

						// Points are values here, so upstream's `Object.assign({}, point)` copies are assignments.
						strikethrough := &Token{
							Type:  typeStrikethrough,
							Start: resolvedEvents[open].Token.Start,
							End:   resolvedEvents[index].Token.End,
						}

						text := &Token{
							Type:  typeStrikethroughText,
							Start: resolvedEvents[open].Token.End,
							End:   resolvedEvents[index].Token.Start,
						}

						// Opening.
						nextEvents := []Event{
							{Enter: true, Token: strikethrough, Context: context},
							{Enter: true, Token: resolvedEvents[open].Token, Context: context},
							{Enter: false, Token: resolvedEvents[open].Token, Context: context},
							{Enter: true, Token: text, Context: context},
						}

						insideSpan := context.Parser.Constructs.InsideSpan

						// Upstream tests the array for truthiness, which an empty array passes; combining always
						// leaves at least the defaults here, so nil never happens.
						if insideSpan != nil {
							// Between. Upstream's `events.slice` is a copy, and the inner resolvers splice it in place.
							nextEvents = splice(
								nextEvents,
								len(nextEvents),
								0,
								resolveAll(insideSpan, slices.Clone(resolvedEvents[open+1:index]), context),
							)
						}

						// Closing.
						nextEvents = splice(nextEvents, len(nextEvents), 0, []Event{
							{Enter: false, Token: text, Context: context},
							{Enter: true, Token: resolvedEvents[index].Token, Context: context},
							{Enter: false, Token: resolvedEvents[index].Token, Context: context},
							{Enter: false, Token: strikethrough, Context: context},
						})

						resolvedEvents = spliceEvents(resolvedEvents, open-1, index-open+3, nextEvents)

						index = open + len(nextEvents) - 2
						break
					}
				}
			}
		}

		for _, event := range resolvedEvents {
			if event.Token.Type == typeStrikethroughSequenceTemporary {
				event.Token.Type = TypeData
			}
		}

		return resolvedEvents
	}

	tokenizeStrikethrough := func(self *Self, effects *Effects, ok State, nok State) State {
		previous := self.Previous
		size := 0

		var start State
		var more State

		start = func(code Code) State {
			// Upstream captures `this.events` at tokenize and reads it here, before anything can change it, so
			// the live slice is the same list. It reads the last event unguarded: a `~` before this one was
			// consumed by this tokenizer, so there is always an event.
			if previous == CodeTilde && self.Events[len(self.Events)-1].Token.Type != TypeCharacterEscape {
				return nok(code)
			}

			effects.Enter(typeStrikethroughSequenceTemporary, nil)
			return more(code)
		}

		more = func(code Code) State {
			before := classifyCharacter(previous)

			if code == CodeTilde {
				// If this is the third marker, exit.
				if size > 1 {
					return nok(code)
				}
				effects.Consume(code)
				size++
				return more
			}

			if size < 2 && !single {
				return nok(code)
			}

			token := effects.Exit(typeStrikethroughSequenceTemporary)
			after := classifyCharacter(code)
			// Upstream's `!x` and `Boolean(x)` over the 0, 1 or 2 of classifyCharacter.
			token.open = after == 0 || (after == attentionSideAfter && before != 0)
			token.close = before == 0 || (before == attentionSideAfter && after != 0)
			return ok(code)
		}

		return start
	}

	tokenizer := &Construct{
		Name:       "strikethrough",
		Tokenize:   tokenizeStrikethrough,
		ResolveAll: &Resolver{Resolve: resolveAllStrikethrough},
	}

	return &Extension{
		Text:             &ConstructRecord{ByCode: map[Code][]*Construct{CodeTilde: {tokenizer}}},
		InsideSpan:       []*Construct{tokenizer},
		AttentionMarkers: []Code{CodeTilde},
	}
}
