package micromark

import "slices"

// gfmTable is micromark-extension-gfm-table/lib/syntax.js, with the alignment that lib/infer.js computes.
//
// The tokenizer emits rows, cell dividers and data; resolveGfmTable then wraps them in table, body, cell and
// cell content tokens through an edit map (gfm_table_edit_map.go), turns each body and head cell's data
// into one chunkText, and hangs each table's column alignment on its enter token as Align, which
// mdast-util-gfm-table reads.
var gfmTable = &Construct{
	Name:       "table",
	Tokenize:   tokenizeGfmTable,
	ResolveAll: &Resolver{Resolve: resolveGfmTable},
}

// The token types the table extension adds.
const (
	typeTable                = "table"
	typeTableBody            = "tableBody"
	typeTableCellDivider     = "tableCellDivider"
	typeTableContent         = "tableContent"
	typeTableData            = "tableData"
	typeTableDelimiter       = "tableDelimiter"
	typeTableDelimiterFiller = "tableDelimiterFiller"
	typeTableDelimiterMarker = "tableDelimiterMarker"
	typeTableDelimiterRow    = "tableDelimiterRow"
	typeTableHead            = "tableHead"
	typeTableHeader          = "tableHeader"
	typeTableRow             = "tableRow"
)

// gfmTableRowKind is upstream's RowKind: where we are, `1` for head row, `2` for delimiter row, `3` for body
// row, `0` for none.
type gfmTableRowKind int

// gfmTableRange is upstream's Range: cell info. The four are the index of the previous cell's end, the
// cell's start, its first data and its last data. Upstream's ranges are arrays, but no array is mutated
// while another name holds it, so values are equivalent.
type gfmTableRange [4]int

// gfmTableExtension is gfmTable(): the table construct, tried at every code in flow.
func gfmTableExtension() *Extension {
	return &Extension{Flow: &ConstructRecord{Null: []*Construct{gfmTable}}}
}

func tokenizeGfmTable(self *Self, effects *Effects, ok State, nok State) State {
	size := 0
	sizeB := 0
	// Upstream's `boolean | undefined`, only ever tested for truthiness.
	seen := false

	var start, headRowBefore, headRowStart, headRowBreak, headRowData, headRowEscape State
	var headDelimiterStart, headDelimiterBefore, headDelimiterCellBefore, headDelimiterValueBefore State
	var headDelimiterLeftAlignmentAfter, headDelimiterFiller, headDelimiterRightAlignmentAfter State
	var headDelimiterCellAfter, headDelimiterNok, bodyRowStart, bodyRowBreak, bodyRowData, bodyRowEscape State

	// Start of a GFM table.
	//
	// If there is a valid table row or table head before, then we try to parse
	// another row.
	// Otherwise, we try to parse a head.
	start = func(code Code) State {
		index := len(self.Events) - 1
		for index > -1 {
			tokenType := self.Events[index].Token.Type
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
			tail = self.Events[index].Token.Type
		}
		isBody := tail == typeTableHead || tail == typeTableRow

		// Don’t allow lazy body rows.
		if isBody && self.Parser.Lazy[self.Now().Line] {
			return nok(code)
		}

		if isBody {
			return bodyRowStart(code)
		}
		return headRowBefore(code)
	}

	// Before table head row.
	headRowBefore = func(code Code) State {
		effects.Enter(typeTableHead, nil)
		effects.Enter(typeTableRow, nil)
		return headRowStart(code)
	}

	// Before table head row, after whitespace.
	headRowStart = func(code Code) State {
		if code == CodeVerticalBar {
			return headRowBreak(code)
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

		seen = true
		// Count the first character, that isn’t a pipe, double.
		sizeB++
		return headRowBreak(code)
	}

	// At break in table head row.
	headRowBreak = func(code Code) State {
		if code == CodeEof {
			// Note: in `markdown-rs`, we need to reset, in `micromark-js` we don‘t.
			return nok(code)
		}

		if markdownLineEnding(code) {
			// If anything other than one pipe (ignoring whitespace) was used, it’s fine.
			if sizeB > 1 {
				sizeB = 0
				// To do: check if this works.
				// Feel free to interrupt:
				self.SetInterrupt(true)
				effects.Exit(typeTableRow)
				effects.Enter(TypeLineEnding, nil)
				effects.Consume(code)
				effects.Exit(TypeLineEnding)
				return headDelimiterStart
			}

			// Note: in `markdown-rs`, we need to reset, in `micromark-js` we don‘t.
			return nok(code)
		}

		if markdownSpace(code) {
			// To do: check if this is fine.
			// effects.attempt(State::Next(StateName::GfmTableHeadRowBreak), State::Nok)
			// State::Retry(space_or_tab(tokenizer))
			return factorySpace(effects, headRowBreak, TypeWhitespace, 0)(code)
		}

		sizeB++

		if seen {
			seen = false
			// Header cell count.
			size++
		}

		if code == CodeVerticalBar {
			effects.Enter(typeTableCellDivider, nil)
			effects.Consume(code)
			effects.Exit(typeTableCellDivider)
			// Whether a delimiter was seen.
			seen = true
			return headRowBreak
		}

		// Anything else is cell data.
		effects.Enter(TypeData, nil)
		return headRowData(code)
	}

	// In table head row data.
	headRowData = func(code Code) State {
		if code == CodeEof || code == CodeVerticalBar || markdownLineEndingOrSpace(code) {
			effects.Exit(TypeData)
			return headRowBreak(code)
		}

		effects.Consume(code)
		if code == CodeBackslash {
			return headRowEscape
		}
		return headRowData
	}

	// In table head row escape.
	headRowEscape = func(code Code) State {
		if code == CodeBackslash || code == CodeVerticalBar {
			effects.Consume(code)
			return headRowData
		}

		return headRowData(code)
	}

	// Before delimiter row.
	headDelimiterStart = func(code Code) State {
		// Reset `interrupt`.
		self.SetInterrupt(false)

		// Note: in `markdown-rs`, we need to handle piercing here too.
		if self.Parser.Lazy[self.Now().Line] {
			return nok(code)
		}

		effects.Enter(typeTableDelimiterRow, nil)
		// Track if we’ve seen a `:` or `|`.
		seen = false

		if markdownSpace(code) {
			// `undefined` as the maximum is no limit, which factorySpace spells 0.
			limit := tabSize
			if slices.Contains(self.Parser.Constructs.Disable, "codeIndented") {
				limit = 0
			}
			return factorySpace(effects, headDelimiterBefore, TypeLinePrefix, limit)(code)
		}

		return headDelimiterBefore(code)
	}

	// Before delimiter row, after optional whitespace.
	//
	// Reused when a `|` is found later, to parse another cell.
	headDelimiterBefore = func(code Code) State {
		if code == CodeDash || code == CodeColon {
			return headDelimiterValueBefore(code)
		}

		if code == CodeVerticalBar {
			seen = true
			// If we start with a pipe, we open a cell marker.
			effects.Enter(typeTableCellDivider, nil)
			effects.Consume(code)
			effects.Exit(typeTableCellDivider)
			return headDelimiterCellBefore
		}

		// More whitespace / empty row not allowed at start.
		return headDelimiterNok(code)
	}

	// After `|`, before delimiter cell.
	headDelimiterCellBefore = func(code Code) State {
		if markdownSpace(code) {
			return factorySpace(effects, headDelimiterValueBefore, TypeWhitespace, 0)(code)
		}

		return headDelimiterValueBefore(code)
	}

	// Before delimiter cell value.
	headDelimiterValueBefore = func(code Code) State {
		// Align: left.
		if code == CodeColon {
			sizeB++
			seen = true

			effects.Enter(typeTableDelimiterMarker, nil)
			effects.Consume(code)
			effects.Exit(typeTableDelimiterMarker)
			return headDelimiterLeftAlignmentAfter
		}

		// Align: none.
		if code == CodeDash {
			sizeB++
			// To do: seems weird that this *isn’t* left aligned, but that state is used?
			return headDelimiterLeftAlignmentAfter(code)
		}

		if code == CodeEof || markdownLineEnding(code) {
			return headDelimiterCellAfter(code)
		}

		return headDelimiterNok(code)
	}

	// After delimiter cell left alignment marker.
	headDelimiterLeftAlignmentAfter = func(code Code) State {
		if code == CodeDash {
			effects.Enter(typeTableDelimiterFiller, nil)
			return headDelimiterFiller(code)
		}

		// Anything else is not ok after the left-align colon.
		return headDelimiterNok(code)
	}

	// In delimiter cell filler.
	headDelimiterFiller = func(code Code) State {
		if code == CodeDash {
			effects.Consume(code)
			return headDelimiterFiller
		}

		// Align is `center` if it was `left`, `right` otherwise.
		if code == CodeColon {
			seen = true
			effects.Exit(typeTableDelimiterFiller)
			effects.Enter(typeTableDelimiterMarker, nil)
			effects.Consume(code)
			effects.Exit(typeTableDelimiterMarker)
			return headDelimiterRightAlignmentAfter
		}

		effects.Exit(typeTableDelimiterFiller)
		return headDelimiterRightAlignmentAfter(code)
	}

	// After delimiter cell right alignment marker.
	headDelimiterRightAlignmentAfter = func(code Code) State {
		if markdownSpace(code) {
			return factorySpace(effects, headDelimiterCellAfter, TypeWhitespace, 0)(code)
		}

		return headDelimiterCellAfter(code)
	}

	// After delimiter cell.
	headDelimiterCellAfter = func(code Code) State {
		if code == CodeVerticalBar {
			return headDelimiterBefore(code)
		}

		if code == CodeEof || markdownLineEnding(code) {
			// Exit when:
			// * there was no `:` or `|` at all (it’s a thematic break or setext
			//   underline instead)
			// * the header cell count is not the delimiter cell count
			if !seen || size != sizeB {
				return headDelimiterNok(code)
			}

			// Note: in markdown-rs`, a reset is needed here.
			effects.Exit(typeTableDelimiterRow)
			effects.Exit(typeTableHead)
			// To do: in `markdown-rs`, resolvers need to be registered manually.
			// effects.register_resolver(ResolveName::GfmTable)
			return ok(code)
		}

		return headDelimiterNok(code)
	}

	// In delimiter row, at a disallowed byte.
	headDelimiterNok = func(code Code) State {
		// Note: in `markdown-rs`, we need to reset, in `micromark-js` we don‘t.
		return nok(code)
	}

	// Before table body row.
	bodyRowStart = func(code Code) State {
		// Note: in `markdown-rs` we need to manually take care of a prefix,
		// but in `micromark-js` that is done for us, so if we’re here, we’re
		// never at whitespace.
		effects.Enter(typeTableRow, nil)
		return bodyRowBreak(code)
	}

	// At break in table body row.
	bodyRowBreak = func(code Code) State {
		if code == CodeVerticalBar {
			effects.Enter(typeTableCellDivider, nil)
			effects.Consume(code)
			effects.Exit(typeTableCellDivider)
			return bodyRowBreak
		}

		if code == CodeEof || markdownLineEnding(code) {
			effects.Exit(typeTableRow)
			return ok(code)
		}

		if markdownSpace(code) {
			return factorySpace(effects, bodyRowBreak, TypeWhitespace, 0)(code)
		}

		// Anything else is cell content.
		effects.Enter(TypeData, nil)
		return bodyRowData(code)
	}

	// In table body row data.
	bodyRowData = func(code Code) State {
		if code == CodeEof || code == CodeVerticalBar || markdownLineEndingOrSpace(code) {
			effects.Exit(TypeData)
			return bodyRowBreak(code)
		}

		effects.Consume(code)
		if code == CodeBackslash {
			return bodyRowEscape
		}
		return bodyRowData
	}

	// In table body row escape.
	bodyRowEscape = func(code Code) State {
		if code == CodeBackslash || code == CodeVerticalBar {
			effects.Consume(code)
			return bodyRowData
		}

		return bodyRowData(code)
	}

	return start
}

// resolveGfmTable is upstream's resolveTable.
//
// Upstream walks `events` but reads points from, and applies the edit map to, `context.events`, then returns
// `events`. The flow tokenizer passes `context.events` itself and no flow construct resolves all before the
// table, so they are one array and the return is the edited list; here that is the new context.Events.
func resolveGfmTable(events []Event, context *TokenizeContext) []Event {
	index := -1
	inFirstCellAwaitingPipe := true
	var rowKind gfmTableRowKind
	var lastCell gfmTableRange
	var cell gfmTableRange
	afterHeadAwaitingFirstBodyRow := false
	lastTableEnd := 0
	var currentTable *Token
	var currentBody *Token
	var currentCell *Token
	editMap := &gfmTableEditMap{}

	for index+1 < len(events) {
		index++
		event := events[index]
		token := event.Token

		if event.Enter {
			// Start of head.
			if token.Type == typeTableHead {
				afterHeadAwaitingFirstBodyRow = false

				// Inject previous (body end and) table end.
				if lastTableEnd != 0 {
					flushGfmTableEnd(editMap, context, lastTableEnd, currentTable, currentBody)
					currentBody = nil
					lastTableEnd = 0
				}

				// Inject table start.
				currentTable = &Token{
					Type:  typeTable,
					Start: token.Start,
					// Note: correct end is set later.
					End: token.End,
				}
				editMap.add(index, 0, []Event{{Enter: true, Token: currentTable, Context: context}})
			} else if token.Type == typeTableRow || token.Type == typeTableDelimiterRow {
				inFirstCellAwaitingPipe = true
				currentCell = nil
				lastCell = gfmTableRange{0, 0, 0, 0}
				cell = gfmTableRange{0, index + 1, 0, 0}

				// Inject table body start.
				if afterHeadAwaitingFirstBodyRow {
					afterHeadAwaitingFirstBodyRow = false
					currentBody = &Token{
						Type:  typeTableBody,
						Start: token.Start,
						// Note: correct end is set later.
						End: token.End,
					}
					editMap.add(index, 0, []Event{{Enter: true, Token: currentBody, Context: context}})
				}

				switch {
				case token.Type == typeTableDelimiterRow:
					rowKind = 2
				case currentBody != nil:
					rowKind = 3
				default:
					rowKind = 1
				}
			} else if rowKind != 0 && (token.Type == TypeData ||
				token.Type == typeTableDelimiterMarker ||
				token.Type == typeTableDelimiterFiller) {
				// Cell data.
				inFirstCellAwaitingPipe = false

				// First value in cell.
				if cell[2] == 0 {
					if lastCell[1] != 0 {
						cell[0] = cell[1]
						currentCell = flushGfmTableCell(editMap, context, lastCell, rowKind, -1, currentCell)
						lastCell = gfmTableRange{0, 0, 0, 0}
					}

					cell[2] = index
				}
			} else if token.Type == typeTableCellDivider {
				if inFirstCellAwaitingPipe {
					inFirstCellAwaitingPipe = false
				} else {
					if lastCell[1] != 0 {
						cell[0] = cell[1]
						currentCell = flushGfmTableCell(editMap, context, lastCell, rowKind, -1, currentCell)
					}

					lastCell = cell
					cell = gfmTableRange{lastCell[1], index, 0, 0}
				}
			}
		} else if token.Type == typeTableHead {
			// Exit events.
			afterHeadAwaitingFirstBodyRow = true
			lastTableEnd = index
		} else if token.Type == typeTableRow || token.Type == typeTableDelimiterRow {
			lastTableEnd = index

			if lastCell[1] != 0 {
				cell[0] = cell[1]
				currentCell = flushGfmTableCell(editMap, context, lastCell, rowKind, index, currentCell)
			} else if cell[1] != 0 {
				currentCell = flushGfmTableCell(editMap, context, cell, rowKind, index, currentCell)
			}

			rowKind = 0
		} else if rowKind != 0 && (token.Type == TypeData ||
			token.Type == typeTableDelimiterMarker ||
			token.Type == typeTableDelimiterFiller) {
			cell[3] = index
		}
	}

	if lastTableEnd != 0 {
		flushGfmTableEnd(editMap, context, lastTableEnd, currentTable, currentBody)
	}

	context.Events = editMap.consume(context.Events)

	// To do: move this into `html`, when events are exposed there.
	// That’s what `markdown-rs` does.
	// That needs updates to `mdast-util-gfm-table`.
	for index, event := range context.Events {
		if event.Enter && event.Token.Type == typeTable {
			event.Token.Align = gfmTableAlign(context.Events, index)
		}
	}

	return context.Events
}

// flushGfmTableCell is upstream's flushCell: generate a cell. A rowEnd of -1 is upstream's undefined.
func flushGfmTableCell(editMap *gfmTableEditMap, context *TokenizeContext, cellRange gfmTableRange, rowKind gfmTableRowKind, rowEnd int, previousCell *Token) *Token {
	// `markdown-rs` uses:
	// rowKind === 2 ? 'tableDelimiterCell' : 'tableCell'
	groupName := typeTableData
	switch rowKind {
	case 1:
		groupName = typeTableHeader
	case 2:
		groupName = typeTableDelimiter
	}
	// `markdown-rs` uses:
	// rowKind === 2 ? 'tableDelimiterCellValue' : 'tableCellText'
	valueName := typeTableContent

	// Insert an exit for the previous cell, if there is one.
	//
	// ```markdown
	// > | | aa | bb | cc |
	//          ^-- exit
	//           ^^^^-- this cell
	// ```
	if cellRange[0] != 0 {
		previousCell.End = gfmTablePoint(context.Events, cellRange[0])
		editMap.add(cellRange[0], 0, []Event{{Enter: false, Token: previousCell, Context: context}})
	}

	// Insert enter of this cell.
	//
	// ```markdown
	// > | | aa | bb | cc |
	//           ^-- enter
	//           ^^^^-- this cell
	// ```
	now := gfmTablePoint(context.Events, cellRange[1])
	previousCell = &Token{
		Type:  groupName,
		Start: now,
		// Note: correct end is set later.
		End: now,
	}
	editMap.add(cellRange[1], 0, []Event{{Enter: true, Token: previousCell, Context: context}})

	// Insert text start at first data start and end at last data end, and
	// remove events between.
	//
	// ```markdown
	// > | | aa | bb | cc |
	//            ^-- enter
	//             ^-- exit
	//           ^^^^-- this cell
	// ```
	if cellRange[2] != 0 {
		relatedStart := gfmTablePoint(context.Events, cellRange[2])
		relatedEnd := gfmTablePoint(context.Events, cellRange[3])
		valueToken := &Token{
			Type:  valueName,
			Start: relatedStart,
			End:   relatedEnd,
		}
		editMap.add(cellRange[2], 0, []Event{{Enter: true, Token: valueToken, Context: context}})

		if rowKind != 2 {
			// Fix positional info on remaining events
			start := context.Events[cellRange[2]]
			end := context.Events[cellRange[3]]
			start.Token.End = end.Token.End
			start.Token.Type = TypeChunkText
			start.Token.ContentType = ContentTypeText

			// Remove if needed.
			if cellRange[3] > cellRange[2]+1 {
				a := cellRange[2] + 1
				b := cellRange[3] - cellRange[2] - 1
				editMap.add(a, b, []Event{})
			}
		}

		editMap.add(cellRange[3]+1, 0, []Event{{Enter: false, Token: valueToken, Context: context}})
	}

	// Insert an exit for the last cell, if at the row end.
	//
	// ```markdown
	// > | | aa | bb | cc |
	//                    ^-- exit
	//               ^^^^^^-- this cell (the last one contains two “between” parts)
	// ```
	if rowEnd != -1 {
		previousCell.End = gfmTablePoint(context.Events, rowEnd)
		editMap.add(rowEnd, 0, []Event{{Enter: false, Token: previousCell, Context: context}})
		previousCell = nil
	}

	return previousCell
}

// flushGfmTableEnd is upstream's flushTableEnd: generate table end (and table body end).
func flushGfmTableEnd(editMap *gfmTableEditMap, context *TokenizeContext, index int, table *Token, tableBody *Token) {
	var exits []Event
	related := gfmTablePoint(context.Events, index)

	if tableBody != nil {
		tableBody.End = related
		exits = append(exits, Event{Enter: false, Token: tableBody, Context: context})
	}

	table.End = related
	exits = append(exits, Event{Enter: false, Token: table, Context: context})

	editMap.add(index+1, 0, exits)
}

// gfmTablePoint is upstream's getPoint: the start of an enter, the end of an exit. Upstream copies the point
// object at every use; Point is a value here.
func gfmTablePoint(events []Event, index int) Point {
	event := events[index]
	if event.Enter {
		return event.Token.Start
	}
	return event.Token.End
}

// gfmTableAlign is micromark-extension-gfm-table/lib/infer.js: figure out the alignment of a GFM table from
// its enter event, one of "left", "right", "center" or "none" per column.
func gfmTableAlign(events []Event, index int) []string {
	inDelimiterRow := false
	align := []string{}

	for index < len(events) {
		event := events[index]

		if inDelimiterRow {
			if event.Enter {
				// Start of alignment value: set a new column.
				// To do: `markdown-rs` uses `tableDelimiterCellValue`.
				if event.Token.Type == typeTableContent {
					if events[index+1].Token.Type == typeTableDelimiterMarker {
						align = append(align, "left")
					} else {
						align = append(align, "none")
					}
				}
			} else if event.Token.Type == typeTableContent {
				// Exits:
				// End of alignment value: change the column.
				// To do: `markdown-rs` uses `tableDelimiterCellValue`.
				if events[index-1].Token.Type == typeTableDelimiterMarker {
					alignIndex := len(align) - 1

					if align[alignIndex] == "left" {
						align[alignIndex] = "center"
					} else {
						align[alignIndex] = "right"
					}
				}
			} else if event.Token.Type == typeTableDelimiterRow {
				// Done!
				break
			}
		} else if event.Enter && event.Token.Type == typeTableDelimiterRow {
			inDelimiterRow = true
		}

		index++
	}

	return align
}
