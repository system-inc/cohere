package micromark

import "slices"

// gfmTableRun is one GFM table: its call's variables, and its states made once per slot.
type gfmTableRun struct {
	gfmTableCall

	startState                            State
	headRowBeforeState                    State
	headRowStartState                     State
	headRowBreakState                     State
	headRowDataState                      State
	headRowEscapeState                    State
	headDelimiterStartState               State
	headDelimiterBeforeState              State
	headDelimiterCellBeforeState          State
	headDelimiterValueBeforeState         State
	headDelimiterLeftAlignmentAfterState  State
	headDelimiterFillerState              State
	headDelimiterRightAlignmentAfterState State
	headDelimiterCellAfterState           State
	headDelimiterNokState                 State
	bodyRowStartState                     State
	bodyRowBreakState                     State
	bodyRowDataState                      State
	bodyRowEscapeState                    State
}

// release zeroes the run's call for its next parse, keeping its bound states.
func (run *gfmTableRun) release() { run.gfmTableCall = gfmTableCall{} }

// Start of a GFM table.
//
// If there is a valid table row or table head before, then we try to parse
// another row.
// Otherwise, we try to parse a head.
func (run *gfmTableRun) start(code Code) State {
	index := len(run.self.Events) - 1
	for index > -1 {
		tokenType := run.self.Events[index].Token.Type
		if tokenType == TypeLineEnding ||
			// Note: markdown-rs uses `whitespace` instead of `linePrefix`
			tokenType == TypeLinePrefix {
			index--
		} else {
			break
		}
	}
	tail := ""
	if index > -1 {
		tail = run.self.Events[index].Token.Type
	}
	isBody := tail == typeTableHead || tail == typeTableRow

	// Don’t allow lazy body rows.
	if isBody && run.self.Parser.Lazy[run.self.Now().Line] {
		return run.nok(code)
	}

	if isBody {
		return run.bodyRowStart(code)
	}
	return run.headRowBefore(code)
}

// Before table head row.
func (run *gfmTableRun) headRowBefore(code Code) State {
	run.effects.Enter(typeTableHead, nil)
	run.effects.Enter(typeTableRow, nil)
	return run.headRowStart(code)
}

// Before table head row, after whitespace.
func (run *gfmTableRun) headRowStart(code Code) State {
	if code == CodeVerticalBar {
		return run.headRowBreak(code)
	}

	// To do: micromark-js should let us parse our own whitespace in extensions,
	// like `markdown-rs`:
	//
	// ```js
	// // 4+ spaces.
	// if (markdownSpace(code)) {
	//   return nok(code)
	// }
	// ```

	run.seen = true
	// Count the first character, that isn’t a pipe, double.
	run.sizeB++
	return run.headRowBreak(code)
}

// At break in table head row.
func (run *gfmTableRun) headRowBreak(code Code) State {
	if code == CodeEof {
		// Note: in `markdown-rs`, we need to reset, in `micromark-js` we don‘t.
		return run.nok(code)
	}

	if markdownLineEnding(code) {
		// If anything other than one pipe (ignoring whitespace) was used, it’s fine.
		if run.sizeB > 1 {
			run.sizeB = 0
			// To do: check if this works.
			// Feel free to interrupt:
			run.self.SetInterrupt(true)
			run.effects.Exit(typeTableRow)
			run.effects.Enter(TypeLineEnding, nil)
			run.effects.Consume(code)
			run.effects.Exit(TypeLineEnding)
			return run.headDelimiterStartState
		}

		// Note: in `markdown-rs`, we need to reset, in `micromark-js` we don‘t.
		return run.nok(code)
	}

	if markdownSpace(code) {
		// To do: check if this is fine.
		// effects.attempt(State::Next(StateName::GfmTableHeadRowBreak), State::Nok)
		// State::Retry(space_or_tab(tokenizer))
		return factorySpace(run.effects, run.headRowBreakState, TypeWhitespace, 0)(code)
	}

	run.sizeB++

	if run.seen {
		run.seen = false
		// Header cell count.
		run.size++
	}

	if code == CodeVerticalBar {
		run.effects.Enter(typeTableCellDivider, nil)
		run.effects.Consume(code)
		run.effects.Exit(typeTableCellDivider)
		// Whether a delimiter was seen.
		run.seen = true
		return run.headRowBreakState
	}

	// Anything else is cell data.
	run.effects.Enter(TypeData, nil)
	return run.headRowData(code)
}

// In table head row data.
func (run *gfmTableRun) headRowData(code Code) State {
	if code == CodeEof || code == CodeVerticalBar || markdownLineEndingOrSpace(code) {
		run.effects.Exit(TypeData)
		return run.headRowBreak(code)
	}

	run.effects.Consume(code)
	if code == CodeBackslash {
		return run.headRowEscapeState
	}
	return run.headRowDataState
}

// In table head row escape.
func (run *gfmTableRun) headRowEscape(code Code) State {
	if code == CodeBackslash || code == CodeVerticalBar {
		run.effects.Consume(code)
		return run.headRowDataState
	}

	return run.headRowData(code)
}

// Before delimiter row.
func (run *gfmTableRun) headDelimiterStart(code Code) State {
	// Reset `interrupt`.
	run.self.SetInterrupt(false)

	// Note: in `markdown-rs`, we need to handle piercing here too.
	if run.self.Parser.Lazy[run.self.Now().Line] {
		return run.nok(code)
	}

	run.effects.Enter(typeTableDelimiterRow, nil)
	// Track if we’ve seen a `:` or `|`.
	run.seen = false

	if markdownSpace(code) {
		// `undefined` as the maximum is no limit, which factorySpace spells 0.
		limit := tabSize
		if slices.Contains(run.self.Parser.Constructs.Disable, "codeIndented") {
			limit = 0
		}
		return factorySpace(run.effects, run.headDelimiterBeforeState, TypeLinePrefix, limit)(code)
	}

	return run.headDelimiterBefore(code)
}

// Before delimiter row, after optional whitespace.
//
// Reused when a `|` is found later, to parse another cell.
func (run *gfmTableRun) headDelimiterBefore(code Code) State {
	if code == CodeDash || code == CodeColon {
		return run.headDelimiterValueBefore(code)
	}

	if code == CodeVerticalBar {
		run.seen = true
		// If we start with a pipe, we open a cell marker.
		run.effects.Enter(typeTableCellDivider, nil)
		run.effects.Consume(code)
		run.effects.Exit(typeTableCellDivider)
		return run.headDelimiterCellBeforeState
	}

	// More whitespace / empty row not allowed at start.
	return run.headDelimiterNok(code)
}

// After `|`, before delimiter cell.
func (run *gfmTableRun) headDelimiterCellBefore(code Code) State {
	if markdownSpace(code) {
		return factorySpace(run.effects, run.headDelimiterValueBeforeState, TypeWhitespace, 0)(code)
	}

	return run.headDelimiterValueBefore(code)
}

// Before delimiter cell value.
func (run *gfmTableRun) headDelimiterValueBefore(code Code) State {
	// Align: left.
	if code == CodeColon {
		run.sizeB++
		run.seen = true

		run.effects.Enter(typeTableDelimiterMarker, nil)
		run.effects.Consume(code)
		run.effects.Exit(typeTableDelimiterMarker)
		return run.headDelimiterLeftAlignmentAfterState
	}

	// Align: none.
	if code == CodeDash {
		run.sizeB++
		// To do: seems weird that this *isn’t* left aligned, but that state is used?
		return run.headDelimiterLeftAlignmentAfter(code)
	}

	if code == CodeEof || markdownLineEnding(code) {
		return run.headDelimiterCellAfter(code)
	}

	return run.headDelimiterNok(code)
}

// After delimiter cell left alignment marker.
func (run *gfmTableRun) headDelimiterLeftAlignmentAfter(code Code) State {
	if code == CodeDash {
		run.effects.Enter(typeTableDelimiterFiller, nil)
		return run.headDelimiterFiller(code)
	}

	// Anything else is not ok after the left-align colon.
	return run.headDelimiterNok(code)
}

// In delimiter cell filler.
func (run *gfmTableRun) headDelimiterFiller(code Code) State {
	if code == CodeDash {
		run.effects.Consume(code)
		return run.headDelimiterFillerState
	}

	// Align is `center` if it was `left`, `right` otherwise.
	if code == CodeColon {
		run.seen = true
		run.effects.Exit(typeTableDelimiterFiller)
		run.effects.Enter(typeTableDelimiterMarker, nil)
		run.effects.Consume(code)
		run.effects.Exit(typeTableDelimiterMarker)
		return run.headDelimiterRightAlignmentAfterState
	}

	run.effects.Exit(typeTableDelimiterFiller)
	return run.headDelimiterRightAlignmentAfter(code)
}

// After delimiter cell right alignment marker.
func (run *gfmTableRun) headDelimiterRightAlignmentAfter(code Code) State {
	if markdownSpace(code) {
		return factorySpace(run.effects, run.headDelimiterCellAfterState, TypeWhitespace, 0)(code)
	}

	return run.headDelimiterCellAfter(code)
}

// After delimiter cell.
func (run *gfmTableRun) headDelimiterCellAfter(code Code) State {
	if code == CodeVerticalBar {
		return run.headDelimiterBefore(code)
	}

	if code == CodeEof || markdownLineEnding(code) {
		// Exit when:
		// * there was no `:` or `|` at all (it’s a thematic break or setext
		//   underline instead)
		// * the header cell count is not the delimiter cell count
		if !run.seen || run.size != run.sizeB {
			return run.headDelimiterNok(code)
		}

		// Note: in markdown-rs`, a reset is needed here.
		run.effects.Exit(typeTableDelimiterRow)
		run.effects.Exit(typeTableHead)
		// To do: in `markdown-rs`, resolvers need to be registered manually.
		// effects.register_resolver(ResolveName::GfmTable)
		return run.ok(code)
	}

	return run.headDelimiterNok(code)
}

// In delimiter row, at a disallowed byte.
func (run *gfmTableRun) headDelimiterNok(code Code) State {
	// Note: in `markdown-rs`, we need to reset, in `micromark-js` we don‘t.
	return run.nok(code)
}

// Before table body row.
func (run *gfmTableRun) bodyRowStart(code Code) State {
	// Note: in `markdown-rs` we need to manually take care of a prefix,
	// but in `micromark-js` that is done for us, so if we’re here, we’re
	// never at whitespace.
	run.effects.Enter(typeTableRow, nil)
	return run.bodyRowBreak(code)
}

// At break in table body row.
func (run *gfmTableRun) bodyRowBreak(code Code) State {
	if code == CodeVerticalBar {
		run.effects.Enter(typeTableCellDivider, nil)
		run.effects.Consume(code)
		run.effects.Exit(typeTableCellDivider)
		return run.bodyRowBreakState
	}

	if code == CodeEof || markdownLineEnding(code) {
		run.effects.Exit(typeTableRow)
		return run.ok(code)
	}

	if markdownSpace(code) {
		return factorySpace(run.effects, run.bodyRowBreakState, TypeWhitespace, 0)(code)
	}

	// Anything else is cell content.
	run.effects.Enter(TypeData, nil)
	return run.bodyRowData(code)
}

// In table body row data.
func (run *gfmTableRun) bodyRowData(code Code) State {
	if code == CodeEof || code == CodeVerticalBar || markdownLineEndingOrSpace(code) {
		run.effects.Exit(TypeData)
		return run.bodyRowBreak(code)
	}

	run.effects.Consume(code)
	if code == CodeBackslash {
		return run.bodyRowEscapeState
	}
	return run.bodyRowDataState
}

// In table body row escape.
func (run *gfmTableRun) bodyRowEscape(code Code) State {
	if code == CodeBackslash || code == CodeVerticalBar {
		run.effects.Consume(code)
		return run.bodyRowDataState
	}

	return run.bodyRowData(code)
}
