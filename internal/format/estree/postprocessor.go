package estree

type postprocessor struct {
	text     string
	stripped *StrippedText
}

// visit is upstream's visitNode, postprocess/visit-node.js.
func (processor *postprocessor) visit(node *Node) *Node {
	if node == nil {
		return nil
	}
	if result := processor.onEnter(node); result != node {
		return processor.visit(result)
	}
	for _, key := range VisitorKeys(node) {
		switch value := node.Get(key).(type) {
		case *Node:
			node.Set(key, processor.visit(value))
		case Nodes:
			for index := range value {
				value[index] = processor.visit(value[index])
			}
		default:
			// Upstream assigns node[key] for every visitor key, so a key the converter never set
			// exists afterwards with an undefined value. `key in node` can see that.
			if !node.Has(key) {
				node.Set(key, nil)
			}
		}
	}
	return processor.onLeave(node)
}

// onEnter is the postprocess's step on a node before its children are visited.
//
// Mutates: node a template element's range moves to espree's positions in place, as upstream's does, since
// the visit keeps the node it was handed
func (processor *postprocessor) onEnter(node *Node) *Node {
	processor.setContentEnd(node)

	switch node.Type() {
	case "TemplateElement":
		// typescript follows the espree style positions.
		start := LocStart(node) + 1
		end := LocEnd(node) - 2
		if node.Bool("tail") {
			end = LocEnd(node) - 1
		}
		node.Range = [2]int{start, end}
	case "TSParenthesizedType":
		return node.Child("typeAnnotation")
	case "TSUnionType", "TSIntersectionType":
		if types := node.List("types"); len(types) == 1 {
			return types[0]
		}
	}
	return node
}

func (processor *postprocessor) onLeave(node *Node) *Node {
	if node.Is("LogicalExpression") && isUnbalancedLogicalTree(node) {
		return rebalanceLogicalTree(node)
	}
	return node
}

// setContentEnd is upstream's setContentEnd: the end of a statement before its semicolon, with the
// comments between them stripped out of consideration.
//
// Mutates: node the content end is recorded on the statement itself, where the printer reads it
func (processor *postprocessor) setContentEnd(node *Node) {
	if !ShouldAddContentEnd(node) {
		return
	}
	end := LocEndWithFullText(node)
	if end == 0 || processor.text[end-1] != ';' {
		return
	}
	end--
	textBeforeSemicolon := processor.stripped.Text()[LocStart(node):end]
	cleaned := TrimEndJavaScript(textBeforeSemicolon)
	node.ContentEnd = end - (len(textBeforeSemicolon) - len(cleaned))
	node.HasContentEnd = true
}
