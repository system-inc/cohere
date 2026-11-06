package css

// src/language-css/loc.js: calculateLoc, locStart and locEnd.
//
// Prettier's positions live on node.source as startOffset and endOffset, which the printer reads
// through locStart and locEnd (and reads node.source itself: whether it exists, and source.start.line).
// The port keeps them there, in the source map, and mirrors them into the node's Range once the walk
// is done (setRanges).
//
// Offsets are bytes. Every length upstream adds (prop.length, name.length, text.length) is a byte length
// here, which agrees with the byte offsets the sub-parsers report. The one place upstream turns a line
// and column into an offset, lineColumnToIndex, converts the column from the library's UTF-16 units.
//
// replaceQuotesInInlineComments is postcss-less's workaround and is reached by neither the css parser nor
// the scss one, so it is not ported.

import (
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/system-inc/cohere/internal/format/estree"
	"github.com/system-inc/cohere/internal/format/printing"
)

func fixValueWordLoc(node *estree.Node, originalIndex int) int {
	value := node.String("value")
	if value == "-" || value == "--" || !strings.HasPrefix(value, "-") {
		return originalIndex
	}
	if len(value) > 1 && value[1] == '-' {
		return originalIndex - 2
	}
	return originalIndex - 1
}

func calculateLocStart(node *estree.Node, text string) int {
	source := sourceOf(node)

	// `postcss>=8`
	if offset, isNumber := numberIn(mapIn(source, "start"), "offset"); isNumber {
		return offset
	}

	// value-* nodes have this
	if sourceIndex, isNumber := numberProperty(node, "sourceIndex"); isNumber {
		if node.Type() == "value-word" {
			return fixValueWordLoc(node, sourceIndex)
		}
		return sourceIndex
	}

	if start := mapIn(source, "start"); start != nil {
		return lineColumnToIndex(start, text)
	}

	// Invariant: this is upstream's throw, not a crash: parseCatching recovers every javaScriptError into
	// the parse's error, as upstream's caller catches it.
	/* c8 ignore next */
	panic(javaScriptError{message: "Can not locate node."})
}

// calculateLocEnd returns false where upstream returns null.
func calculateLocEnd(node *estree.Node, text string) (int, bool) {
	source := sourceOf(node)

	if node.Type() == "css-comment" && node.Truthy("inline") {
		// Neither postcss nor postcss-scss marks node.inline (postcss-scss sets raws.inline, and its
		// inline comment's source.end.offset below); this is postcss-less's.
		startOffset, _ := numberIn(source, "startOffset")
		end := printing.SkipEverythingButNewLine(text, startOffset, false)
		return end, end >= 0
	}

	if sourceIndex, isNumber := numberProperty(node, "sourceIndex"); node.Type() == "value-paren" && isNumber {
		if node.String("value") == ")" {
			return sourceIndex + len(node.String("value")), true
		}
		return sourceIndex, true
	}

	// `postcss>=8`
	if offset, isNumber := numberIn(mapIn(source, "end"), "offset"); isNumber {
		return offset, true
	}

	if source != nil {
		if end := mapIn(source, "end"); end != nil {
			index := lineColumnToIndex(end, text)
			if node.Type() == "value-word" {
				return fixValueWordLoc(node, index), true
			}
			return index, true
		}

		if nodes := node.List("nodes"); len(nodes) > 0 {
			return calculateLocEnd(nodes[len(nodes)-1], text)
		}

		if name, isString := asString(node.Get("name")); node.Type() == "css-atrule" && isString {
			afterName, _ := rawStringIn(node, "afterName")
			parameters, _ := rawStringIn(node, "params")
			return calculateLocStart(node, text) + 1 + len(name) + len(afterName) + len(parameters), true
		}
	}

	if sourceIndex, isNumber := numberProperty(node, "sourceIndex"); isNumber {
		if value, isString := asString(node.Get("value")); isString {
			return sourceIndex + len(value), true
		}
	}

	return 0, false
}

func calculateLoc(node *estree.Node, text string) {
	calculateNodeLoc(node, text, 0, false)
}

func calculateNodeLoc(node *estree.Node, text string, rootOffset int, isRootOfText bool) {
	source := sourceOf(node)
	_, hasStartOffset := numberIn(source, "startOffset")
	_, hasEndOffset := numberIn(source, "endOffset")
	_, hasSourceIndex := numberProperty(node, "sourceIndex")

	if isRootOfText && hasStringType(node) {
		source = ensureSource(node)
		source["startOffset"] = estree.Integer(rootOffset)
		source["endOffset"] = estree.Integer(rootOffset + len(text))
	} else if hasStartOffset && hasEndOffset {
		// Already calculated while walking another view of the same nested tree.
	} else if source != nil || hasSourceIndex {
		// The source exists before calculateLocEnd runs, which decides between its branches on it.
		source = ensureSource(node)
		maximumEndOffset := rootOffset + len(text)
		source["startOffset"] = estree.Integer(min(calculateLocStart(node, text)+rootOffset, maximumEndOffset))
		if endOffset, isNumber := calculateLocEnd(node, text); isNumber {
			source["endOffset"] = estree.Integer(min(endOffset+rootOffset, maximumEndOffset))
		} else {
			source["endOffset"] = nil
		}
	}

	for _, key := range node.Keys() {
		if key == "source" {
			continue
		}

		for _, childNode := range objectChildren(node.Get(key)) {
			nestedText, nestedRootOffset, isNested := getNestedLoc(node, childNode, rootOffset)
			if isNested {
				calculateNodeLoc(childNode, nestedText, nestedRootOffset, true)
			} else {
				calculateNodeLoc(childNode, text, rootOffset, false)
			}

			fillEmptyLocFromParent(childNode, node)
		}
	}

	fillLocFromChildren(node)
	fillEmptyChildLocs(node)
}

// getNestedLoc returns upstream's {text, rootOffset}, and false where upstream returns undefined.
func getNestedLoc(parentNode *estree.Node, node *estree.Node, rootOffset int) (string, int, bool) {
	if node.Type() == "value-root" || node.Type() == "value-unknown" {
		return firstTruthyString(node.Get("text"), node.Get("value")), getValueRootOffset(parentNode), true
	}

	if parameters, isNode := parentNode.Get(
		"params",
	).(*estree.Node); node.Type() == "media-query-list" || (isNode && parameters == node) {
		return firstTruthyString(
			estree.String(rawString(parentNode, "params")),
			node.Get("value"),
		), getAtRuleParamsRootOffset(parentNode), true
	}

	if strings.HasPrefix(node.Type(), "selector-") {
		if selectorText, isString := getSelectorText(parentNode, node); isString {
			return selectorText, getSelectorRootOffset(parentNode, node, selectorText, rootOffset), true
		}
	}

	return "", 0, false
}

func getValueRootOffset(node *estree.Node) int {
	result, _ := numberIn(sourceOf(node), "startOffset")
	if property, isString := asString(node.Get("prop")); isString {
		result += len(property)
	}

	if name, isString := asString(node.Get("name")); node.Type() == "css-atrule" && isString {
		afterName, _ := rawStringIn(node, "afterName")
		result += 1 + len(name) + len(leadingColonAndWhitespace(afterName))
	}

	if between, isString := rawStringIn(node, "between"); node.Type() != "css-atrule" && isString {
		result += len(between)
	}

	return result
}

func getAtRuleParamsRootOffset(node *estree.Node) int {
	result, _ := numberIn(sourceOf(node), "startOffset")

	if name, isString := asString(node.Get("name")); node.Type() == "css-atrule" && isString {
		afterName, _ := rawStringIn(node, "afterName")
		result += 1 + len(name) + len(leadingColonAndWhitespace(afterName))
	}

	return result
}

// leadingColonAndWhitespace is afterName.match(/^\s*:?\s*/)[0].
func leadingColonAndWhitespace(afterName string) string {
	index := len(afterName) - len(trimStart(afterName))
	if index < len(afterName) && afterName[index] == ':' {
		index++
	}
	return afterName[:len(afterName)-len(trimStart(afterName[index:]))]
}

func getSelectorText(parentNode *estree.Node, selectorNode *estree.Node) (string, bool) {
	if selector, isString := rawStringIn(selectorNode, "selector"); isString {
		return selector, true
	}

	if selector, isNode := parentNode.Get("selector").(*estree.Node); !isNode || selector != selectorNode {
		return "", false
	}

	if parentNode.Truthy("mixin") {
		identifier, _ := rawStringIn(parentNode, "identifier")
		afterName, _ := rawStringIn(parentNode, "afterName")
		parameters, _ := rawStringIn(parentNode, "params")
		return identifier + parentNode.String("name") + afterName + parameters, true
	}

	if selector, isString := rawStringIn(parentNode, "selector"); isString {
		return selector, true
	}

	if parameters, isString := rawStringIn(parentNode, "params"); parentNode.Truthy("customSelector") && isString {
		return trim(sliceJavaScript(parameters, len(parentNode.String("customSelector")), len(parameters))), true
	}

	if parameters, isString := rawStringIn(parentNode, "params"); isString {
		return parameters, true
	}

	if value, isString := rawStringIn(parentNode, "value"); parentNode.Type() == "css-decl" && isString {
		if parentNode.Truthy("extend") && strings.HasPrefix(value, "extend(") {
			return sliceJavaScript(value, len("extend("), len(value)-1), true
		}

		return value, true
	}

	return "", false
}

func getSelectorRootOffset(
	parentNode *estree.Node,
	selectorNode *estree.Node,
	selectorText string,
	rootOffset int,
) int {
	if sourceIndex, isNumber := numberProperty(selectorNode, "sourceIndex"); isNumber {
		if _, isString := rawStringIn(selectorNode, "selector"); isString {
			return rootOffset + sourceIndex
		}
	}

	parentStartOffset, _ := numberIn(sourceOf(parentNode), "startOffset")

	if parentNode.Truthy("mixin") {
		return parentStartOffset
	}

	if selector, isString := rawStringIn(parentNode, "selector"); isString {
		return parentStartOffset + getStringOffset(selector, selectorText)
	}

	if parameters, isString := rawStringIn(parentNode, "params"); isString {
		return getAtRuleParamsRootOffset(parentNode) + getStringOffset(parameters, selectorText)
	}

	if value, isString := rawStringIn(parentNode, "value"); parentNode.Type() == "css-decl" && isString {
		return getValueRootOffset(parentNode) + getStringOffset(value, selectorText)
	}

	return rootOffset
}

func getStringOffset(text string, search string) int {
	index := strings.Index(text, search)
	if index == -1 {
		return 0
	}
	return index
}

func fillLocFromChildren(node *estree.Node) {
	if !hasStringType(node) {
		return
	}

	source := sourceOf(node)
	_, hasStartOffset := numberIn(source, "startOffset")
	_, hasEndOffset := numberIn(source, "endOffset")
	hasOffsets := hasStartOffset && hasEndOffset
	hasLineColumn := source != nil && estree.IsTruthy(source["start"]) && estree.IsTruthy(source["end"])

	if hasOffsets && hasLineColumn {
		return
	}

	found := false
	startOffset, endOffset := 0, 0
	// The children whose offsets won, whose source.start and source.end the node takes. Nothing writes a
	// source's start or end between here and the reads below, so reading them there reads what upstream
	// kept while walking.
	var startChild *estree.Node
	var endChild *estree.Node

	for _, key := range node.Keys() {
		if key == "source" || key == "raws" || key == "spaces" {
			continue
		}

		for _, childNode := range objectChildren(node.Get(key)) {
			childSource := sourceOf(childNode)
			childStartOffset, hasChildStartOffset := numberIn(childSource, "startOffset")
			childEndOffset, hasChildEndOffset := numberIn(childSource, "endOffset")
			if !hasChildStartOffset || !hasChildEndOffset {
				continue
			}

			// Upstream starts from Infinity and -Infinity, so the first child sets both.
			if !found || childStartOffset < startOffset {
				startOffset = childStartOffset
				startChild = childNode
			}

			if !found || childEndOffset > endOffset {
				endOffset = childEndOffset
				endChild = childNode
			}
			found = true
		}
	}

	if found {
		source = ensureSource(node)
		if !hasOffsets {
			source["startOffset"] = estree.Integer(startOffset)
			source["endOffset"] = estree.Integer(endOffset)
		}
		// `??=` writes the property even when the value it writes is undefined.
		if source["start"] == nil {
			source["start"] = sourceOf(startChild)["start"]
		}
		if source["end"] == nil {
			source["end"] = sourceOf(endChild)["end"]
		}
	}
}

func fillEmptyLocFromParent(node *estree.Node, parentNode *estree.Node) {
	parentSource := sourceOf(parentNode)
	parentStartOffset, hasParentStartOffset := numberIn(parentSource, "startOffset")
	_, hasParentEndOffset := numberIn(parentSource, "endOffset")
	if !hasStringType(node) ||
		node.Truthy("source") ||
		!hasParentStartOffset ||
		!hasParentEndOffset ||
		!isEmptyLocNode(node) {
		return
	}

	node.Set("source", estree.Record{
		"startOffset": estree.Integer(parentStartOffset),
		"endOffset":   estree.Integer(parentStartOffset),
		"start":       parentSource["start"],
		"end":         parentSource["start"],
	})
}

func fillEmptyChildLocs(node *estree.Node) {
	source := sourceOf(node)
	_, hasStartOffset := numberIn(source, "startOffset")
	_, hasEndOffset := numberIn(source, "endOffset")
	if !hasStartOffset || !hasEndOffset {
		return
	}

	for _, key := range node.Keys() {
		if key == "source" || key == "raws" || key == "spaces" {
			continue
		}

		for _, childNode := range objectChildren(node.Get(key)) {
			fillEmptyLocFromParent(childNode, node)
		}
	}
}

func isEmptyLocNode(node *estree.Node) bool {
	return isEmptyArray(node.Get("nodes")) || isEmptyArray(node.Get("groups"))
}

// locStart is node.source.startOffset.
func locStart(node *estree.Node) int {
	startOffset, _ := numberIn(sourceOf(node), "startOffset")
	return startOffset
}

// locEnd is node.source.endOffset. Where upstream's is null (calculateLocEnd found nothing), this is 0,
// what JavaScript makes of null in slice() and arithmetic.
func locEnd(node *estree.Node) int {
	endOffset, _ := numberIn(sourceOf(node), "endOffset")
	return endOffset
}

// setRanges mirrors every node's startOffset and endOffset into its Range, once calculateLoc is done.
func setRanges(value estree.Value) {
	for _, node := range objectChildren(value) {
		node.Range = [2]int{locStart(node), locEnd(node)}
		for _, key := range node.Keys() {
			if key != "source" {
				setRanges(node.Get(key))
			}
		}
	}
}

// lineColumnToIndex is src/utilities/line-column-to-index.js: the offset of a line (one-based) and
// column. The column is in the library's UTF-16 units and is walked as such from the line's start; a
// column past the end of the text adds its remainder one byte per unit, as upstream's sum would.
// Upstream's `text.indexOf("\n", index) + 1` restarts at 0 when a line is missing, and so does this.
func lineColumnToIndex(lineColumn estree.Record, text string) int {
	line, _ := numberIn(lineColumn, "line")
	column, _ := numberIn(lineColumn, "column")
	index := 0
	for skipped := 0; skipped < line-1; skipped++ {
		index = indexFrom(text, "\n", index) + 1
	}
	for column > 0 && index < len(text) {
		character, size := utf8.DecodeRuneInString(text[index:])
		units := 1
		if character != utf8.RuneError && utf16.RuneLen(character) == 2 {
			units = 2
		}
		if units > column {
			break
		}
		column -= units
		index += size
	}
	return index + column
}

// indexFrom is text.indexOf(search, from).
func indexFrom(text string, search string, from int) int {
	if from > len(text) {
		return -1
	}
	index := strings.Index(text[from:], search)
	if index == -1 {
		return -1
	}
	return from + index
}
