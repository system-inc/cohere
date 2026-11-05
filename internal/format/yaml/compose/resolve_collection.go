package compose

// Ported from eemeli/yaml 2.9.0, dist/compose/resolve-block-map.js, resolve-block-seq.js,
// resolve-flow-collection.js, util-contains-newline.js and util-map-includes.js.
// util-flow-indent-check.js runs only with the compat schema option, which is never set on this path.

import (
	"strings"

	"github.com/system-inc/cohere/internal/format/yaml/cst"
)

// nodeClass is `tag?.nodeClass ?? fallback`.
func nodeClass(tag *Tag, fallback string) string {
	if tag != nil && tag.NodeClass != "" {
		return tag.NodeClass
	}
	return fallback
}

const startColMsg = "All mapping items must start at the same column"

func resolveBlockMap(composition *composeContext, bm *cst.Token, onError onErrorFunc, tag *Tag) *Node {
	yamlMap := composition.newCollection(nodeClass(tag, "YAMLMap"))
	if composition.atRoot {
		composition.atRoot = false
	}
	offset := bm.Offset
	// commentEnd is null until set; upstream tests it for truthiness, so 0 counts as unset there.
	commentEnd, commentEndSet := 0, false
	for _, collItem := range bm.Items {
		start, key, sep, value := collItem.Start, collItem.Key, collItem.Sep, collItem.Value
		// key properties
		keyProperties := resolveProps(start, resolvePropsOptions{
			indicator:      "explicit-key-ind",
			next:           keyOrFirstSep(key, sep),
			offset:         offset,
			onError:        onError,
			parentIndent:   bm.Indent,
			startOnNewline: true,
		})
		implicitKey := keyProperties.found == nil
		if implicitKey {
			if key != nil {
				if key.Type == "block-seq" {
					onError(offset, "BLOCK_AS_IMPLICIT_KEY", "A block sequence may not be used as an implicit map key", false)
				} else if key.HasIndent() && key.Indent != bm.Indent {
					onError(offset, "BAD_INDENT", startColMsg, false)
				}
			}
			if keyProperties.anchor == nil && keyProperties.tag == nil && sep == nil {
				commentEnd, commentEndSet = keyProperties.end, true
				if keyProperties.comment != "" {
					if yamlMap.Comment != "" {
						yamlMap.Comment += "\n" + keyProperties.comment
					} else {
						yamlMap.Comment = keyProperties.comment
					}
				}
				continue
			}
			if keyProperties.newlineAfterProp != nil || containsNewline(key) {
				// `key ?? start[start.length - 1]`
				var source any = key
				if key == nil {
					source = start[len(start)-1]
				}
				onError(source, "MULTILINE_IMPLICIT_KEY", "Implicit keys need to be on a single line", false)
			}
		} else if keyProperties.found.Indent != bm.Indent {
			onError(offset, "BAD_INDENT", startColMsg, false)
		}
		// key value
		composition.atKey = true
		keyStart := keyProperties.end
		var keyNode *Node
		if key != nil {
			keyNode = composeNode(composition, key, keyProperties, onError)
		} else {
			keyNode = composeEmptyNode(composition, keyStart, start, keyProperties, onError)
		}
		composition.atKey = false
		if mapIncludes(composition, yamlMap.Items, keyNode) {
			onError(keyStart, "DUPLICATE_KEY", "Map keys must be unique", false)
		}
		// value properties
		valueProperties := resolveProps(sepOrEmpty(sep), resolvePropsOptions{
			indicator:      "map-value-ind",
			next:           value,
			offset:         keyNode.Range[2],
			onError:        onError,
			parentIndent:   bm.Indent,
			startOnNewline: key == nil || key.Type == "block-scalar",
		})
		offset = valueProperties.end
		if valueProperties.found != nil {
			if implicitKey {
				if value != nil && value.Type == "block-map" && !valueProperties.hasNewline {
					onError(offset, "BLOCK_AS_IMPLICIT_KEY", "Nested mappings are not allowed in compact mappings", false)
				}
				if composition.options.Strict && keyProperties.start < valueProperties.found.Offset-1024 {
					onError(keyNode.Range, "KEY_OVER_1024_CHARS",
						"The : indicator must be at most 1024 chars after the start of an implicit block mapping key", false)
				}
			}
			// value value
			var valueNode *Node
			if value != nil {
				valueNode = composeNode(composition, value, valueProperties, onError)
			} else {
				valueNode = composeEmptyNode(composition, offset, sep, valueProperties, onError)
			}
			offset = valueNode.Range[2]
			pair := composition.newPair(keyNode, valueNode)
			if composition.options.KeepSourceTokens {
				pair.SrcItem = collItem
			}
			yamlMap.Items = append(yamlMap.Items, pair)
		} else {
			// key with no value
			if implicitKey {
				onError(keyNode.Range, "MISSING_CHAR", "Implicit map keys need to be followed by map values", false)
			}
			if valueProperties.comment != "" {
				if keyNode.Comment != "" {
					keyNode.Comment += "\n" + valueProperties.comment
				} else {
					keyNode.Comment = valueProperties.comment
				}
			}
			pair := composition.newPair(keyNode, nil)
			if composition.options.KeepSourceTokens {
				pair.SrcItem = collItem
			}
			yamlMap.Items = append(yamlMap.Items, pair)
		}
	}
	if commentEndSet && commentEnd != 0 && commentEnd < offset {
		onError(commentEnd, "IMPOSSIBLE", "Map comment with trailing content", false)
	}
	end := offset
	if commentEndSet {
		end = commentEnd
	}
	yamlMap.Range = []int{bm.Offset, offset, end}
	return yamlMap
}

// keyOrFirstSep is `key ?? sep?.[0]`.
func keyOrFirstSep(key *cst.Token, sep []*cst.Token) *cst.Token {
	if key != nil {
		return key
	}
	if len(sep) > 0 {
		return sep[0]
	}
	return nil
}

// sepOrEmpty is `sep ?? []`.
func sepOrEmpty(sep []*cst.Token) []*cst.Token {
	if sep == nil {
		return []*cst.Token{}
	}
	return sep
}

func resolveBlockSeq(composition *composeContext, bs *cst.Token, onError onErrorFunc, tag *Tag) *Node {
	seq := composition.newCollection(nodeClass(tag, "YAMLSeq"))
	if composition.atRoot {
		composition.atRoot = false
	}
	if composition.atKey {
		composition.atKey = false
	}
	offset := bs.Offset
	commentEnd, commentEndSet := 0, false
	for _, item := range bs.Items {
		start, value := item.Start, item.Value
		properties := resolveProps(start, resolvePropsOptions{
			indicator:      "seq-item-ind",
			next:           value,
			offset:         offset,
			onError:        onError,
			parentIndent:   bs.Indent,
			startOnNewline: true,
		})
		if properties.found == nil {
			if properties.anchor != nil || properties.tag != nil || value != nil {
				if value != nil && value.Type == "block-seq" {
					onError(properties.end, "BAD_INDENT", "All sequence items must start at the same column", false)
				} else {
					onError(offset, "MISSING_CHAR", "Sequence item without - indicator", false)
				}
			} else {
				commentEnd, commentEndSet = properties.end, true
				if properties.comment != "" {
					seq.Comment = properties.comment
				}
				continue
			}
		}
		var node *Node
		if value != nil {
			node = composeNode(composition, value, properties, onError)
		} else {
			node = composeEmptyNode(composition, properties.end, start, properties, onError)
		}
		offset = node.Range[2]
		seq.Items = append(seq.Items, node)
	}
	end := offset
	if commentEndSet {
		end = commentEnd
	}
	seq.Range = []int{bs.Offset, offset, end}
	return seq
}

const blockMsg = "Block collections are not allowed within flow collections"

func isBlock(token *cst.Token) bool {
	return token != nil && (token.Type == "block-map" || token.Type == "block-seq")
}

// typeError is a TypeError upstream throws by reading a property of undefined. composeNode catches it
// where upstream's try/catch around composeCollection does.
type typeError string

func resolveFlowCollection(composition *composeContext, fc *cst.Token, onError onErrorFunc, tag *Tag) *Node {
	isMap := equalsASCII(fc.FlowStart.Source, "{")
	fcName := "flow sequence"
	fallback := "YAMLSeq"
	if isMap {
		fcName = "flow map"
		fallback = "YAMLMap"
	}
	coll := composition.newCollection(nodeClass(tag, fallback))
	coll.Flow = true
	atRoot := composition.atRoot
	if atRoot {
		composition.atRoot = false
	}
	if composition.atKey {
		composition.atKey = false
	}
	offset := fc.Offset + len(fc.FlowStart.Source)
	for i := 0; i < len(fc.Items); i++ {
		collItem := fc.Items[i]
		start, key, sep, value := collItem.Start, collItem.Key, collItem.Sep, collItem.Value
		properties := resolveProps(start, resolvePropsOptions{
			flow:           fcName,
			indicator:      "explicit-key-ind",
			next:           keyOrFirstSep(key, sep),
			offset:         offset,
			onError:        onError,
			parentIndent:   fc.Indent,
			startOnNewline: false,
		})
		if properties.found == nil {
			if properties.anchor == nil && properties.tag == nil && sep == nil && value == nil {
				if i == 0 && properties.comma != nil {
					onError(properties.comma, "UNEXPECTED_TOKEN", "Unexpected , in "+fcName, false)
				} else if i < len(fc.Items)-1 {
					onError(properties.start, "UNEXPECTED_TOKEN", "Unexpected empty item in "+fcName, false)
				}
				if properties.comment != "" {
					if coll.Comment != "" {
						coll.Comment += "\n" + properties.comment
					} else {
						coll.Comment = properties.comment
					}
				}
				offset = properties.end
				continue
			}
			if !isMap && composition.options.Strict && containsNewline(key) {
				onError(key, // checked by containsNewline()
					"MULTILINE_IMPLICIT_KEY", "Implicit keys of flow sequence pairs need to be on a single line", false)
			}
		}
		if i == 0 {
			if properties.comma != nil {
				onError(properties.comma, "UNEXPECTED_TOKEN", "Unexpected , in "+fcName, false)
			}
		} else {
			if properties.comma == nil {
				onError(properties.start, "MISSING_CHAR", "Missing , between "+fcName+" items", false)
			}
			if properties.comment != "" {
				previousItemComment := []uint16{}
			loop:
				for _, st := range start {
					switch st.Type {
					case "comma", "space":
					case "comment":
						previousItemComment = substring(st.Source, 1, len(st.Source))
						break loop
					default:
						break loop
					}
				}
				if len(previousItemComment) > 0 {
					if len(coll.Items) == 0 {
						// `coll.items[coll.items.length - 1]` is undefined.
						panic(typeError("Cannot read properties of undefined (reading 'comment')"))
					}
					previous := coll.Items[len(coll.Items)-1]
					if IsPair(previous) {
						// `prev.value ?? prev.key`
						if previous.Value != nil {
							previous = previous.Value
						} else {
							previous = previous.Key
						}
					}
					if previous.Comment != "" {
						previous.Comment += "\n" + unitsToString(previousItemComment)
					} else {
						previous.Comment = unitsToString(previousItemComment)
					}
					properties.comment = unitsToString(substring(stringToUnits(properties.comment), len(previousItemComment)+1,
						UnitLength(properties.comment)))
				}
			}
		}
		if !isMap && sep == nil && properties.found == nil {
			// item is a value in a seq
			// → key & sep are empty, start does not include ? or :
			var valueNode *Node
			if value != nil {
				valueNode = composeNode(composition, value, properties, onError)
			} else {
				valueNode = composeEmptyNode(composition, properties.end, sep, properties, onError)
			}
			coll.Items = append(coll.Items, valueNode)
			offset = valueNode.Range[2]
			if isBlock(value) {
				onError(valueNode.Range, "BLOCK_IN_FLOW", blockMsg, false)
			}
		} else {
			// item is a key+value pair
			// key value
			composition.atKey = true
			keyStart := properties.end
			var keyNode *Node
			if key != nil {
				keyNode = composeNode(composition, key, properties, onError)
			} else {
				keyNode = composeEmptyNode(composition, keyStart, start, properties, onError)
			}
			if isBlock(key) {
				onError(keyNode.Range, "BLOCK_IN_FLOW", blockMsg, false)
			}
			composition.atKey = false
			// value properties
			valueProperties := resolveProps(sepOrEmpty(sep), resolvePropsOptions{
				flow:           fcName,
				indicator:      "map-value-ind",
				next:           value,
				offset:         keyNode.Range[2],
				onError:        onError,
				parentIndent:   fc.Indent,
				startOnNewline: false,
			})
			if valueProperties.found != nil {
				if !isMap && properties.found == nil && composition.options.Strict {
					for _, st := range sep {
						if st == valueProperties.found {
							break
						}
						if st.Type == "newline" {
							onError(st, "MULTILINE_IMPLICIT_KEY", "Implicit keys of flow sequence pairs need to be on a single line", false)
							break
						}
					}
					if properties.start < valueProperties.found.Offset-1024 {
						onError(valueProperties.found, "KEY_OVER_1024_CHARS",
							"The : indicator must be at most 1024 chars after the start of an implicit flow sequence key", false)
					}
				}
			} else if value != nil {
				if value.HasSource() && characterAt(value.Source, 0) == ':' {
					onError(value, "MISSING_CHAR", "Missing space after : in "+fcName, false)
				} else {
					onError(valueProperties.start, "MISSING_CHAR", "Missing , or : between "+fcName+" items", false)
				}
			}
			// value value
			var valueNode *Node
			if value != nil {
				valueNode = composeNode(composition, value, valueProperties, onError)
			} else if valueProperties.found != nil {
				valueNode = composeEmptyNode(composition, valueProperties.end, sep, valueProperties, onError)
			}
			if valueNode != nil {
				if isBlock(value) {
					onError(valueNode.Range, "BLOCK_IN_FLOW", blockMsg, false)
				}
			} else if valueProperties.comment != "" {
				if keyNode.Comment != "" {
					keyNode.Comment += "\n" + valueProperties.comment
				} else {
					keyNode.Comment = valueProperties.comment
				}
			}
			pair := composition.newPair(keyNode, valueNode)
			if composition.options.KeepSourceTokens {
				pair.SrcItem = collItem
			}
			if isMap {
				if mapIncludes(composition, coll.Items, keyNode) {
					onError(keyStart, "DUPLICATE_KEY", "Map keys must be unique", false)
				}
				coll.Items = append(coll.Items, pair)
			} else {
				yamlMap := composition.newCollection("YAMLMap")
				yamlMap.Flow = true
				yamlMap.Items = append(yamlMap.Items, pair)
				endRange := keyNode.Range
				if valueNode != nil {
					endRange = valueNode.Range
				}
				yamlMap.Range = []int{keyNode.Range[0], endRange[1], endRange[2]}
				coll.Items = append(coll.Items, yamlMap)
			}
			if valueNode != nil {
				offset = valueNode.Range[2]
			} else {
				offset = valueProperties.end
			}
		}
	}
	expectedEnd := "]"
	if isMap {
		expectedEnd = "}"
	}
	// `const [ce, ...ee] = fc.end`
	var ce *cst.Token
	ee := []*cst.Token{}
	if len(fc.End) > 0 {
		ce = fc.End[0]
		ee = append(ee, fc.End[1:]...)
	}
	cePos := offset
	if ce != nil && equalsASCII(ce.Source, expectedEnd) {
		cePos = ce.Offset + len(ce.Source)
	} else {
		name := strings.ToUpper(fcName[:1]) + fcName[1:]
		code := "BAD_INDENT"
		message := name + " in block collection must be sufficiently indented and end with a " + expectedEnd
		if atRoot {
			code = "MISSING_CHAR"
			message = name + " must end with a " + expectedEnd
		}
		onError(offset, code, message, false)
		if ce != nil && len(ce.Source) != 1 {
			ee = append([]*cst.Token{ce}, ee...)
		}
	}
	if len(ee) > 0 {
		end := resolveEnd(ee, cePos, composition.options.Strict, onError)
		if end.comment != "" {
			if coll.Comment != "" {
				coll.Comment += "\n" + end.comment
			} else {
				coll.Comment = end.comment
			}
		}
		coll.Range = []int{fc.Offset, cePos, end.offset}
	} else {
		coll.Range = []int{fc.Offset, cePos, cePos}
	}
	return coll
}

// containsNewline is upstream's, with its null for no key as false: every caller tests it for
// truthiness.
func containsNewline(key *cst.Token) bool {
	if key == nil {
		return false
	}
	switch key.Type {
	case "alias", "scalar", "double-quoted-scalar", "single-quoted-scalar":
		if includesUnit(key.Source, '\n') {
			return true
		}
		for _, st := range key.End {
			if st.Type == "newline" {
				return true
			}
		}
		return false
	case "flow-collection":
		for _, it := range key.Items {
			for _, st := range it.Start {
				if st.Type == "newline" {
					return true
				}
			}
			for _, st := range it.Sep {
				if st.Type == "newline" {
					return true
				}
			}
			if containsNewline(it.Key) || containsNewline(it.Value) {
				return true
			}
		}
		return false
	default:
		return true
	}
}

// mapIncludes is upstream's with uniqueKeys false or true; upstream's custom comparison function is
// not an option here.
func mapIncludes(composition *composeContext, items []*Node, search *Node) bool {
	if !composition.options.UniqueKeys {
		return false
	}
	for _, pair := range items {
		a := pair.Key
		if a == search || IsScalar(a) && IsScalar(search) && strictEquals(a.ScalarValue, search.ScalarValue) {
			return true
		}
	}
	return false
}

// strictEquals is `a === b` for scalar values: NaN is not itself, and objects are only themselves.
func strictEquals(a any, b any) bool {
	switch x := a.(type) {
	case float64:
		y, isNumber := b.(float64)
		return isNumber && x == y
	case Date, Binary:
		return false
	}
	switch b.(type) {
	case Date, Binary:
		return false
	}
	return a == b
}
