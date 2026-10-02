package doc

import (
	"fmt"
	"strings"
)

/*
 * The document utilities printers call while building docs, src/document/utilities/index.js and
 * traverse-doc.js in the fork. Statement for statement, including the parts that read as accidents:
 *
 *   - findInDoc stops descending once it has an answer but still pops what is already stacked, so the
 *     callback is never called again after the first result, which is all that is observable.
 *   - mapDoc caches by identity, so a subtree shared by two parents (a conditional group's states share
 *     their parts) maps to one shared result. Go docs that are slices (Concat) have no identity and are
 *     mapped afresh each time, which is safe because every node that can carry identity inside them
 *     (a group, an indent) is a pointer and is cached.
 *   - cleanDoc concatenates adjacent strings and flattens arrays one level, after its children are
 *     already clean, the way upstream's mapDoc order makes it.
 */

// TraverseDoc is upstream's traverseDoc. onEnter returning false skips the doc's children; onExit may
// be nil.
func TraverseDoc(root Doc, onEnter func(Doc) bool, onExit func(Doc), shouldTraverseConditionalGroups bool) {
	stack := []Doc{root}
	for len(stack) > 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if _, isExit := current.(traverseExit); isExit {
			leaving := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			onExit(leaving)
			continue
		}
		if onExit != nil {
			stack = append(stack, current, traverseExit{})
		}
		if onEnter != nil && !onEnter(current) {
			continue
		}

		switch document := current.(type) {
		case Concat:
			for index := len(document) - 1; index >= 0; index-- {
				stack = append(stack, document[index])
			}
		case *Fill:
			for index := len(document.Parts) - 1; index >= 0; index-- {
				stack = append(stack, document.Parts[index])
			}
		case *IfBreak:
			stack = append(stack, document.FlatContents, document.BreakContents)
		case *Group:
			if shouldTraverseConditionalGroups && document.ExpandedStates != nil {
				for index := len(document.ExpandedStates) - 1; index >= 0; index-- {
					stack = append(stack, document.ExpandedStates[index])
				}
			} else {
				stack = append(stack, document.Contents)
			}
		case *Align:
			stack = append(stack, document.Contents)
		case *Indent:
			stack = append(stack, document.Contents)
		case *IndentIfBreak:
			stack = append(stack, document.Contents)
		case *Label:
			stack = append(stack, document.Contents)
		case *LineSuffix:
			stack = append(stack, document.Contents)
		case Text, trimDoc, lineSuffixBoundaryDoc, *Line, breakParentDoc, nil:
		default:
			panic(fmt.Sprintf("invalid doc %T", current))
		}
	}
}

// FindInDoc is upstream's findInDoc: the first non-nil answer fn gives, in traversal order.
func FindInDoc[T any](root Doc, fn func(Doc) (T, bool), defaultValue T) T {
	result := defaultValue
	found := false
	TraverseDoc(root, func(document Doc) bool {
		if found {
			return false
		}
		if value, ok := fn(document); ok {
			found = true
			result = value
		}
		return true
	}, nil, false)
	return result
}

// WillBreak is upstream's willBreak: a broken group, a hard line, or a break-parent anywhere inside.
func WillBreak(document Doc) bool {
	return FindInDoc(document, func(current Doc) (bool, bool) {
		switch typed := current.(type) {
		case *Group:
			if typed.Break {
				return true, true
			}
		case *Line:
			if typed.Hard {
				return true, true
			}
		case breakParentDoc:
			return true, true
		}
		return false, false
	}, false)
}

// CanBreak is upstream's canBreak: any line at all inside.
func CanBreak(document Doc) bool {
	return FindInDoc(document, func(current Doc) (bool, bool) {
		if _, isLine := current.(*Line); isLine {
			return true, true
		}
		return false, false
	}, false)
}

// MapDoc is upstream's mapDoc: children first, then fn on the rebuilt node.
func MapDoc(root Doc, fn func(Doc) Doc) Doc {
	if text, isText := root.(Text); isText {
		return fn(text)
	}
	mapped := map[Doc]Doc{}
	var rec func(Doc) Doc
	rec = func(document Doc) Doc {
		cacheable := isPointerDoc(document)
		if cacheable {
			if result, present := mapped[document]; present {
				return result
			}
		}
		result := mapOne(document, rec, fn)
		if cacheable {
			mapped[document] = result
		}
		return result
	}
	return rec(root)
}

func isPointerDoc(document Doc) bool {
	switch document.(type) {
	case *Group, *Fill, *IfBreak, *Indent, *Align, *IndentIfBreak, *LineSuffix, *Label, *Line:
		return true
	}
	return false
}

func mapOne(document Doc, rec func(Doc) Doc, fn func(Doc) Doc) Doc {
	switch typed := document.(type) {
	case Concat:
		parts := make(Concat, len(typed))
		for index, part := range typed {
			parts[index] = rec(part)
		}
		return fn(parts)
	case *Fill:
		parts := make([]Doc, len(typed.Parts))
		for index, part := range typed.Parts {
			parts[index] = rec(part)
		}
		return fn(&Fill{Parts: parts})
	case *IfBreak:
		return fn(&IfBreak{BreakContents: rec(typed.BreakContents), FlatContents: rec(typed.FlatContents), GroupID: typed.GroupID})
	case *Group:
		copied := *typed
		if typed.ExpandedStates != nil {
			states := make([]Doc, len(typed.ExpandedStates))
			for index, state := range typed.ExpandedStates {
				states[index] = rec(state)
			}
			copied.ExpandedStates = states
			copied.Contents = states[0]
		} else {
			copied.Contents = rec(typed.Contents)
		}
		return fn(&copied)
	case *Align:
		copied := *typed
		copied.Contents = rec(typed.Contents)
		return fn(&copied)
	case *Indent:
		return fn(&Indent{Contents: rec(typed.Contents)})
	case *IndentIfBreak:
		copied := *typed
		copied.Contents = rec(typed.Contents)
		return fn(&copied)
	case *Label:
		return fn(&Label{Label: typed.Label, Contents: rec(typed.Contents)})
	case *LineSuffix:
		return fn(&LineSuffix{Contents: rec(typed.Contents)})
	case Text, trimDoc, lineSuffixBoundaryDoc, *Line, breakParentDoc, nil:
		return fn(document)
	}
	panic(fmt.Sprintf("invalid doc %T", document))
}

// isFalsy is JavaScript truthiness for a doc in the places upstream writes `!doc.contents`.
func isFalsy(document Doc) bool {
	if document == nil {
		return true
	}
	text, isText := document.(Text)
	return isText && text == ""
}

// CleanDoc is upstream's cleanDoc.
func CleanDoc(document Doc) Doc {
	return MapDoc(document, cleanDocFn)
}

func cleanDocFn(document Doc) Doc {
	switch typed := document.(type) {
	case *Fill:
		allEmpty := true
		for _, part := range typed.Parts {
			if text, isText := part.(Text); !isText || text != "" {
				allEmpty = false
				break
			}
		}
		if allEmpty {
			return Text("")
		}
		if len(typed.Parts) == 1 {
			return typed.Parts[0]
		}
	case *Group:
		if isFalsy(typed.Contents) && typed.ID == nil && !typed.Break && typed.ExpandedStates == nil {
			return Text("")
		}
		if inner, isGroup := typed.Contents.(*Group); isGroup && inner.ID == typed.ID && inner.Break == typed.Break &&
			sameStates(inner.ExpandedStates, typed.ExpandedStates) {
			return inner
		}
	case *Align:
		if isFalsy(typed.Contents) {
			return Text("")
		}
	case *Indent:
		if isFalsy(typed.Contents) {
			return Text("")
		}
	case *IndentIfBreak:
		if isFalsy(typed.Contents) {
			return Text("")
		}
	case *LineSuffix:
		if isFalsy(typed.Contents) {
			return Text("")
		}
	case *IfBreak:
		if isFalsy(typed.FlatContents) && isFalsy(typed.BreakContents) {
			return Text("")
		}
	case Concat:
		var parts Concat
		for _, part := range typed {
			if isFalsy(part) {
				continue
			}
			current, rest := part, Concat(nil)
			if nested, isConcat := part.(Concat); isConcat {
				if len(nested) == 0 {
					// `[first, ...rest] = []` makes first undefined, which upstream then pushes.
					parts = append(parts, nil)
					continue
				}
				current, rest = nested[0], nested[1:]
			}
			currentText, currentIsText := current.(Text)
			if len(parts) > 0 {
				if previous, previousIsText := parts[len(parts)-1].(Text); currentIsText && previousIsText {
					parts[len(parts)-1] = previous + currentText
					parts = append(parts, rest...)
					continue
				}
			}
			parts = append(parts, current)
			parts = append(parts, rest...)
		}
		if len(parts) == 0 {
			return Text("")
		}
		if len(parts) == 1 {
			return parts[0]
		}
		return parts
	}
	return document
}

// sameStates is upstream's `a.expandedStates === b.expandedStates`: identity of the array, which in
// Go is two nil slices, or two slices sharing a backing array and length.
func sameStates(left []Doc, right []Doc) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return len(left) == len(right) && &left[0] == &right[0]
}

// ReplaceEndOfLine is upstream's replaceEndOfLine: every newline in every string becomes replacement,
// literalline by default.
func ReplaceEndOfLine(document Doc, replacement Doc) Doc {
	if replacement == nil {
		replacement = Literalline
	}
	return MapDoc(document, func(current Doc) Doc {
		text, isText := current.(Text)
		if !isText {
			return current
		}
		lines := strings.Split(string(text), "\n")
		parts := make([]Doc, len(lines))
		for index, line := range lines {
			parts[index] = Text(line)
		}
		return Join(replacement, parts)
	})
}

// RemoveLines is upstream's removeLines: soft lines vanish, lines become spaces, if-breaks go flat.
func RemoveLines(document Doc) Doc {
	return MapDoc(document, func(current Doc) Doc {
		switch typed := current.(type) {
		case *Line:
			if !typed.Hard {
				if typed.Soft {
					return Text("")
				}
				return Text(" ")
			}
		case *IfBreak:
			return typed.FlatContents
		}
		return current
	})
}

// IsEmptyDoc is upstream's isEmptyDoc: whether cleanDoc(doc) would be "".
func IsEmptyDoc(document Doc) bool {
	isEmpty := true
	TraverseDoc(document, func(current Doc) bool {
		switch typed := current.(type) {
		case Text:
			if typed == "" {
				return true
			}
			isEmpty = false
			return false
		case trimDoc, lineSuffixBoundaryDoc, *Line, breakParentDoc:
			isEmpty = false
			return false
		}
		return true
	}, nil, false)
	return isEmpty
}

// StripTrailingHardline is upstream's stripTrailingHardline.
func StripTrailingHardline(document Doc) Doc {
	return stripTrailingHardlineFromDoc(CleanDoc(document))
}

func stripTrailingHardlineFromParts(parts []Doc) []Doc {
	parts = append([]Doc(nil), parts...)
	for len(parts) >= 2 {
		_, isLine := parts[len(parts)-2].(*Line)
		_, isBreakParent := parts[len(parts)-1].(breakParentDoc)
		if !isLine || !isBreakParent {
			break
		}
		parts = parts[:len(parts)-2]
	}
	if len(parts) > 0 {
		parts[len(parts)-1] = stripTrailingHardlineFromDoc(parts[len(parts)-1])
	}
	return parts
}

func stripTrailingHardlineFromDoc(document Doc) Doc {
	switch typed := document.(type) {
	case *Indent:
		return &Indent{Contents: stripTrailingHardlineFromDoc(typed.Contents)}
	case *IndentIfBreak:
		copied := *typed
		copied.Contents = stripTrailingHardlineFromDoc(typed.Contents)
		return &copied
	case *Group:
		copied := *typed
		copied.Contents = stripTrailingHardlineFromDoc(typed.Contents)
		return &copied
	case *LineSuffix:
		return &LineSuffix{Contents: stripTrailingHardlineFromDoc(typed.Contents)}
	case *Label:
		return &Label{Label: typed.Label, Contents: stripTrailingHardlineFromDoc(typed.Contents)}
	case *IfBreak:
		return &IfBreak{
			BreakContents: stripTrailingHardlineFromDoc(typed.BreakContents),
			FlatContents:  stripTrailingHardlineFromDoc(typed.FlatContents),
			GroupID:       typed.GroupID,
		}
	case *Fill:
		return &Fill{Parts: stripTrailingHardlineFromParts(typed.Parts)}
	case Concat:
		return Concat(stripTrailingHardlineFromParts(typed))
	case Text:
		// trim-newlines' trimNewlinesEnd: trailing \r and \n only.
		return Text(strings.TrimRight(string(typed), "\r\n"))
	}
	return document
}
