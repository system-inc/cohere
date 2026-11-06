package unist

// Ported from yaml-unist-parser 3.2.0, dist/transforms/: map.mjs, seq.mjs, flow-map.mjs, flow-seq.mjs
// and pair.mjs.

import (
	"github.com/system-inc/cohere/internal/format/yaml/compose"
	"github.com/system-inc/cohere/internal/format/yaml/cst"
)

// itemAt is `srcToken.items[index]`: nil, upstream's undefined, past the end.
func itemAt(items []*cst.CollectionItem, index int) *cst.CollectionItem {
	if index < len(items) {
		return items[index]
	}
	return nil
}

// createItemFunc is the factory transformPair builds its item with: createMappingItem or
// createFlowMappingItem.
type createItemFunc func(position *position, key *Node, value *Node) *Node

// transformMap is upstream's transformMap.
func (context *context) transformMap(mapNode *compose.Node, properties []*cst.Token) *Node {
	sourceToken := mapNode.SourceToken
	if sourceToken == nil || sourceToken.Type != "block-map" {
		throwError("Expected block mapping srcToken")
	}
	mappingItems := make([]*Node, len(mapNode.Items))
	for index, pair := range mapNode.Items {
		sourceItem := itemAt(sourceToken.Items, index)
		mappingItems[index] = context.transformPair(pair, sourceItem, context.createMappingItem)
	}
	if len(mapNode.Items) < len(sourceToken.Items) {
		for index := len(mapNode.Items); index < len(sourceToken.Items); index++ {
			sourceItem := sourceToken.Items[index]
			for _, token := range context.extractComments(sourceItem.Start) {
				throwError("Unexpected token type in collection item start: %s", token.Type)
			}
		}
	}
	if len(mappingItems) == 0 {
		// mappingItems[0].position
		throwTypeError("undefined", "position")
	}
	return context.createMapping(
		context.createPosition(
			context.position(mappingItems[0]).start,
			context.position(mappingItems[len(mappingItems)-1]).end,
		),
		context.transformContentProperties(mapNode, properties), mappingItems)
}

// transformSequence is upstream's transformSequence.
func (context *context) transformSequence(seq *compose.Node, properties []*cst.Token) *Node {
	sourceToken := seq.SourceToken
	if sourceToken == nil || sourceToken.Type != "block-seq" {
		throwError("Expected block sequence srcToken")
	}
	sequenceItems := make([]*Node, len(seq.Items))
	for index, itemNode := range seq.Items {
		sourceItem := itemAt(sourceToken.Items, index)
		if sourceItem == nil {
			throwTypeError("undefined", "start")
		}
		propertyTokens := []*cst.Token{}
		var seqItemIndToken *cst.Token
		for _, token := range tokens(sourceItem.Start) {
			if maybeContentPropertyToken(token) {
				propertyTokens = append(propertyTokens, token)
				continue
			}
			if token.Type == "seq-item-ind" {
				seqItemIndToken = token
				continue
			}
			throwError("Unexpected token type in sequence item start: %s", token.Type)
		}
		item := context.transformItemValue(itemNode, propertyTokens)
		var start *point
		if seqItemIndToken != nil {
			start = context.transformOffset(seqItemIndToken.Offset)
		} else {
			if item == nil {
				throwTypeError("null", "position")
			}
			start = context.position(item).start
		}
		// `item?.position.end ?? context.transformOffset(seqItemIndToken.offset + ...)`
		var end *point
		if item != nil {
			end = context.position(item).end
		} else {
			end = context.transformOffset(seqItemIndToken.Offset + len(seqItemIndToken.Source))
		}
		sequenceItems[index] = context.createSequenceItem(context.createPosition(start, end), item)
	}
	if len(seq.Items) < len(sourceToken.Items) {
		for index := len(seq.Items); index < len(sourceToken.Items); index++ {
			sourceItem := sourceToken.Items[index]
			for _, token := range context.extractComments(sourceItem.Start) {
				throwError("Unexpected token type in collection item start: %s", token.Type)
			}
		}
	}
	if len(sequenceItems) == 0 {
		throwTypeError("undefined", "position")
	}
	return context.createSequence(
		context.createPosition(
			context.position(sequenceItems[0]).start,
			context.position(sequenceItems[len(sequenceItems)-1]).end,
		),
		context.transformContentProperties(seq, properties), sequenceItems)
}

// transformItemValue is upstream's transformItemValue. A pair is an item of a sequence a !!pairs or
// !!omap tag resolved; its srcToken is the collection item of the map it came from, and the mapping
// built around it shares the item's position object, as upstream's does.
func (context *context) transformItemValue(itemNode *compose.Node, properties []*cst.Token) *Node {
	if !compose.IsPair(itemNode) {
		if isEmptyNode(itemNode, properties) {
			context.extractComments(properties)
			return nil
		}
		return context.transformNode(itemNode, properties)
	}
	sourceItem := itemNode.SourceItem
	mappingItem := context.transformPair(itemNode, sourceItem, context.createMappingItem)
	return context.createMapping(
		context.position(mappingItem),
		context.transformContentProperties(itemNode.Key, properties),
		[]*Node{mappingItem},
	)
}

// transformFlowMap is upstream's transformFlowMap.
func (context *context) transformFlowMap(flowMap *compose.Node, properties []*cst.Token) *Node {
	sourceToken := flowMap.SourceToken
	if sourceToken == nil || sourceToken.Type != "flow-collection" {
		throwError("Expected flow-collection CST node for flow map")
	}
	flowMappingItems := make([]*Node, len(flowMap.Items))
	for index, pair := range flowMap.Items {
		sourceItem := itemAt(sourceToken.Items, index)
		flowMappingItems[index] = context.transformPair(pair, sourceItem, context.createFlowMappingItem)
	}
	context.extractTrailingItemComments(len(flowMap.Items), sourceToken.Items)
	var flowMapEndToken *cst.Token
	for _, token := range context.extractComments(sourceToken.End) {
		if token.Type == "flow-map-end" {
			flowMapEndToken = token
			continue
		}
		throwError("Unexpected token type in flow map end: %s", token.Type)
	}
	if flowMapEndToken == nil {
		throwError("Expected flow-map-end token")
	}
	return context.createFlowMapping(
		context.transformRange(sourceToken.FlowStart.Offset, flowMapEndToken.Offset+len(flowMapEndToken.Source)),
		context.transformContentProperties(flowMap, properties), flowMappingItems)
}

// extractTrailingItemComments is the loop transformFlowMap and transformFlowSeq share: the source items
// past the composed ones hold only comments and commas.
func (context *context) extractTrailingItemComments(composed int, items []*cst.CollectionItem) {
	if composed < len(items) {
		for index := composed; index < len(items); index++ {
			sourceItem := items[index]
			for _, token := range context.extractComments(sourceItem.Start) {
				if token.Type == "comma" {
					continue
				}
				throwError("Unexpected token type in collection item start: %s", token.Type)
			}
		}
	}
}

// transformFlowSequence is upstream's transformFlowSequence.
func (context *context) transformFlowSequence(flowSequence *compose.Node, properties []*cst.Token) *Node {
	sourceToken := flowSequence.SourceToken
	if sourceToken == nil || sourceToken.Type != "flow-collection" {
		throwError("Expected flow-collection CST node for flow sequence")
	}
	flowSequenceItems := make([]*Node, len(flowSequence.Items))
	for index, item := range flowSequence.Items {
		sourceItem := itemAt(sourceToken.Items, index)
		if isBlockMappingOfImmediateChildOfFlowSequence(item, sourceItem) {
			flowSequenceItems[index] = context.transformPair(
				item.Items[len(item.Items)-1],
				sourceItem,
				context.createFlowMappingItem,
			)
			continue
		}
		if !compose.IsPair(item) {
			if sourceItem == nil {
				throwTypeError("undefined", "start")
			}
			propertyTokens := []*cst.Token{}
			for _, token := range tokens(sourceItem.Start) {
				if maybeContentPropertyToken(token) {
					propertyTokens = append(propertyTokens, token)
					continue
				}
				if token.Type == "comma" {
					continue
				}
				throwError("Unexpected token type in sequence item start: %s", token.Type)
			}
			node := context.transformNode(item, propertyTokens)
			if node == nil {
				throwTypeError("null", "position")
			}
			flowSequenceItems[index] = context.createFlowSequenceItem(
				context.createPosition(context.position(node).start, context.position(node).end), node)
		} else {
			flowSequenceItems[index] = context.transformPair(item, sourceItem, context.createFlowMappingItem)
		}
	}
	context.extractTrailingItemComments(len(flowSequence.Items), sourceToken.Items)
	var flowSequenceEndToken *cst.Token
	for _, token := range tokens(sourceToken.End) {
		if token.Type == "comment" {
			context.transformComment(token)
			continue
		}
		if token.Type == "flow-seq-end" {
			flowSequenceEndToken = token
			continue
		}
		throwError("Unexpected token type in flow seq end: %s", token.Type)
	}
	if flowSequenceEndToken == nil {
		throwError("Expected flow-seq-end token")
	}
	return context.createFlowSequence(
		context.transformRange(
			sourceToken.FlowStart.Offset,
			flowSequenceEndToken.Offset+len(flowSequenceEndToken.Source),
		),
		context.transformContentProperties(flowSequence, properties), flowSequenceItems)
}

// isBlockMappingOfImmediateChildOfFlowSequence is upstream's: the map the composer wraps around a pair
// in a flow sequence, `[ key: value ]`, which has no source token and one pair whose source is this
// item. Both sides of the last comparison can be undefined, and undefined === undefined.
func isBlockMappingOfImmediateChildOfFlowSequence(item *compose.Node, sourceItem *cst.CollectionItem) bool {
	if item.SourceToken != nil {
		return false
	}
	if !compose.IsMap(item) {
		return false
	}
	if len(item.Items) != 1 {
		return false
	}
	return item.Items[0].SourceItem == sourceItem
}

// transformPair is upstream's transformPair (pair.mjs).
func (context *context) transformPair(
	pair *compose.Node,
	sourceItem *cst.CollectionItem,
	createNode createItemFunc,
) *Node {
	if sourceItem == nil {
		throwTypeError("undefined", "start")
	}
	keyPropTokens := []*cst.Token{}
	var explicitKeyIndToken *cst.Token
	for _, token := range tokens(sourceItem.Start) {
		if maybeContentPropertyToken(token) {
			keyPropTokens = append(keyPropTokens, token)
			continue
		}
		if token.Type == "explicit-key-ind" {
			explicitKeyIndToken = token
			continue
		}
		if token.Type == "comma" {
			continue
		}
		throwError("Unexpected token type in collection item start: %s", token.Type)
	}
	valuePropTokens := []*cst.Token{}
	var mapValueIndToken *cst.Token
	for _, token := range tokens(sourceItem.Sep) {
		if maybeContentPropertyToken(token) {
			valuePropTokens = append(valuePropTokens, token)
			continue
		}
		if token.Type == "map-value-ind" {
			mapValueIndToken = token
			continue
		}
		throwError("Unexpected token type in collection item sep: %s", token.Type)
	}

	// explicitKeyIndToken?.offset ?? srcItem.key?.offset ?? mapValueIndToken?.offset ?? srcItem.value.offset
	var keyStartOffset int
	switch {
	case explicitKeyIndToken != nil:
		keyStartOffset = explicitKeyIndToken.Offset
	case sourceItem.Key != nil:
		keyStartOffset = sourceItem.Key.Offset
	case mapValueIndToken != nil:
		keyStartOffset = mapValueIndToken.Offset
	default:
		if sourceItem.Value == nil {
			throwTypeError("undefined", "offset")
		}
		keyStartOffset = sourceItem.Value.Offset
	}
	keyRange := []int{keyStartOffset, 0}
	switch {
	case sourceItem.Key != nil:
		if pair.Key == nil {
			throwTypeError("null", "range")
		}
		if pair.Key.Range == nil {
			throwTypeError("undefined", "1")
		}
		keyRange[1] = pair.Key.Range[1]
	case explicitKeyIndToken != nil:
		keyRange[1] = explicitKeyIndToken.Offset + len(explicitKeyIndToken.Source)
	default:
		keyRange[1] = keyStartOffset
	}
	var valueRange []int
	if pair.Value != nil {
		// mapValueIndToken?.offset ?? srcItem.value?.offset ?? pair.value.range[0]
		var valueStartOffset int
		switch {
		case mapValueIndToken != nil:
			valueStartOffset = mapValueIndToken.Offset
		case sourceItem.Value != nil:
			valueStartOffset = sourceItem.Value.Offset
		default:
			valueStartOffset = pair.Value.Range[0]
		}
		valueRange = []int{valueStartOffset, 0}
		switch {
		case sourceItem.Value != nil:
			valueRange[1] = pair.Value.Range[1]
		case mapValueIndToken != nil:
			valueRange[1] = mapValueIndToken.Offset + len(mapValueIndToken.Source)
		default:
			valueRange[1] = valueStartOffset
		}
	}
	return context.transformAstPair(pair, createNode, keyRange, keyPropTokens, valueRange, valuePropTokens)
}

// transformAstPair is upstream's transformAstPair. Upstream's additionalKeyData and additionalValueData
// are { range, props }: the ranges, nil for null, and the prop tokens.
func (context *context) transformAstPair(
	pair *compose.Node,
	createNode createItemFunc,
	keyRange []int,
	keyProperties []*cst.Token,
	valueRange []int,
	valueProperties []*cst.Token,
) *Node {
	var keyContent *Node
	if !isEmptyNode(pair.Key, keyProperties) {
		keyContent = context.transformNode(pair.Key, keyProperties)
	} else {
		context.extractComments(keyProperties)
	}
	var valueContent *Node
	if !isEmptyNode(pair.Value, valueProperties) {
		valueContent = context.transformNode(pair.Value, valueProperties)
	} else {
		context.extractComments(valueProperties)
	}

	// keyRange is never null upstream either; keyContent's start is the fallback it never takes.
	keyEnd := keyRange[1]
	if keyContent != nil {
		keyEnd = context.position(keyContent).end.offset
	}
	mappingKey := context.createMappingKey(context.transformRange(keyRange[0], keyEnd), keyContent)

	// `valueContent || additionalValueData.range ? createMappingValue(...) : null`
	var mappingValue *Node
	if valueContent != nil || valueRange != nil {
		var start int
		if valueRange != nil {
			start = valueRange[0]
		} else {
			start = context.position(valueContent).start.offset
		}
		var end int
		if valueContent != nil {
			end = context.position(valueContent).end.offset
		} else {
			end = valueRange[0] + 1
		}
		mappingValue = context.createMappingValue(context.transformRange(start, end), valueContent)
	}

	itemEnd := context.position(mappingKey).end
	if mappingValue != nil {
		itemEnd = context.position(mappingValue).end
	} else {
		mappingValue = context.createMappingValue(context.createEmptyPosition(context.position(mappingKey).end), nil)
	}
	return createNode(context.createPosition(context.position(mappingKey).start, itemEnd), mappingKey, mappingValue)
}
