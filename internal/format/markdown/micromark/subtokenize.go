package micromark

import "slices"

// subtokenize is micromark-util-subtokenize: tokenize the content of every chunk that has a content type,
// and splice the result in. Returns the events and whether no subtokens were found.
//
// The events sit in a spliceBuffer, as upstream has them: subcontent splices each content's events in as
// the loop sweeps forward, and on a plain slice every splice copied all the events after it, which grew
// with the square of the document.
func subtokenize(eventsArray []Event) ([]Event, bool) {
	events := newSpliceBuffer(eventsArray)
	jumps := map[int]int{}
	index := -1
	more := false

	for index+1 < events.length() {
		index++
		for {
			jump, present := jumps[index]
			if !present {
				break
			}
			index = jump
		}

		event := events.get(index)

		// Add a hook for the GFM tasklist extension, which needs to know if text is in the first content
		// of a list item.
		if index > 0 &&
			event.Token.Type == TypeChunkFlow &&
			events.get(index-1).Token.Type == TypeListItemPrefix {
			subevents := event.Token.tokenizer.Events
			otherIndex := 0

			if otherIndex < len(subevents) && subevents[otherIndex].Token.Type == TypeLineEndingBlank {
				otherIndex += 2
			}

			if otherIndex < len(subevents) && subevents[otherIndex].Token.Type == TypeContent {
				for {
					otherIndex++
					if otherIndex >= len(subevents) {
						break
					}
					if subevents[otherIndex].Token.Type == TypeContent {
						break
					}

					if subevents[otherIndex].Token.Type == TypeChunkText {
						subevents[otherIndex].Token.isInFirstContentOfListItem = true
						otherIndex++
					}
				}
			}
		}

		if event.Enter {
			// Enter.
			if event.Token.ContentType != "" {
				for key, value := range subcontent(events, index) {
					jumps[key] = value
				}
				index = jumps[index]
				more = true
			}
		} else if event.Token.container {
			// Exit.
			otherIndex := index
			lineIndex := -1

			for otherIndex > 0 {
				otherIndex--
				otherEvent := events.get(otherIndex)

				if otherEvent.Token.Type == TypeLineEnding || otherEvent.Token.Type == TypeLineEndingBlank {
					if otherEvent.Enter {
						if lineIndex > 0 {
							events.get(lineIndex).Token.Type = TypeLineEndingBlank
						}

						otherEvent.Token.Type = TypeLineEnding
						lineIndex = otherIndex
					}
				} else if otherEvent.Token.Type == TypeLinePrefix || otherEvent.Token.Type == TypeListItemIndent {
					// Move past.
				} else {
					break
				}
			}

			// Upstream tests `lineIndex` for truthiness, so index 0 counts as none.
			if lineIndex > 0 {
				// Fix position.
				event.Token.End = events.get(lineIndex).Token.Start

				// Switch container exit w/ line endings.
				parameters := append([]Event{event}, events.slice(lineIndex, index)...)
				events.splice(lineIndex, index-lineIndex+1, parameters)
			}
		}
	}

	// Upstream copies the buffer back into the array it was given; here the copy is returned in its place.
	return events.slice(0, events.length()), !more
}

// subcontent tokenizes embedded tokens, splices them in, and returns the gaps to jump over.
func subcontent(events *spliceBuffer, eventIndex int) map[int]int {
	token := events.get(eventIndex).Token
	context := events.get(eventIndex).Context
	startPosition := eventIndex - 1
	var startPositions []int

	tokenizer := token.tokenizer

	if tokenizer == nil {
		start := token.Start
		tokenizer = context.Parser.create(token.ContentType, &start)

		if token.contentTypeTextTrailing {
			tokenizer.contentTypeTextTrailing = true
		}
	}

	var jumps [][2]int
	gaps := map[int]int{}
	var previous *Token
	current := token
	adjust := 0
	start := 0
	breaks := []int{start}

	// Loop forward through the linked tokens to pass them in order to the subtokenizer.
	for current != nil {
		// Find the position of the event for this token.
		for {
			startPosition++
			if events.get(startPosition).Token == current {
				break
			}
		}

		startPositions = append(startPositions, startPosition)

		if current.tokenizer == nil {
			stream := context.SliceStream(current)

			if current.Next == nil {
				stream = append(stream, codeChunk(CodeEof))
			}

			if previous != nil {
				tokenizer.DefineSkip(current.Start)
			}

			if current.isInFirstContentOfListItem {
				tokenizer.gfmTasklistFirstContentOfListItem = true
			}

			tokenizer.Write(stream)

			if current.isInFirstContentOfListItem {
				tokenizer.gfmTasklistFirstContentOfListItem = false
			}
		}

		// Unravel the next token.
		previous = current
		current = current.Next
	}

	// The tokenizer may have replaced its events while resolving, so they are read only once it is done.
	childEvents := tokenizer.Events

	// Now, loop back through all events (and linked tokens), to figure out which parts belong where.
	current = token

	for index := 0; index < len(childEvents); index++ {
		if
		// Find a void token that includes a break.
		!childEvents[index].Enter &&
			childEvents[index-1].Enter &&
			childEvents[index].Token.Type == childEvents[index-1].Token.Type &&
			childEvents[index].Token.Start.Line != childEvents[index].Token.End.Line {
			start = index + 1
			breaks = append(breaks, start)
			// Help GC.
			current.tokenizer = nil
			current.Previous = nil
			current = current.Next
		}
	}

	// Help GC.
	tokenizer.Events = nil

	// If there’s one more token (which is the cases for lines that end in an EOF), that’s perfect: the
	// last point we found starts it. If there isn’t then make sure any remaining content is added to it.
	if current != nil {
		// Help GC.
		current.tokenizer = nil
		current.Previous = nil
	} else {
		breaks = breaks[:len(breaks)-1]
	}

	// Now splice the events from the subtokenizer into the current events, moving back to front so that
	// splice indices aren’t affected.
	for index := len(breaks) - 1; index >= 0; index-- {
		// The buffer copies what it inserts, so a view of the child events serves where upstream slices a copy.
		var slice []Event
		if index+1 < len(breaks) {
			slice = childEvents[breaks[index]:breaks[index+1]]
		} else {
			slice = childEvents[breaks[index]:]
		}
		start := startPositions[len(startPositions)-1]
		startPositions = startPositions[:len(startPositions)-1]
		jumps = append(jumps, [2]int{start, start + len(slice) - 1})
		events.splice(start, 2, slice)
	}

	slices.Reverse(jumps)

	for _, jump := range jumps {
		gaps[adjust+jump[0]] = adjust + jump[1]
		adjust += jump[1] - jump[0] - 1
	}

	return gaps
}
