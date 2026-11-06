package javascript

import (
	"sync"

	"github.com/system-inc/cohere/internal/format/doc"
)

/*
 * Upstream's doc builders under upstream's names, so a print function ports line for line.
 *
 * JavaScript docs are strings, arrays and objects freely mixed; Go's are typed, and every builder here
 * takes Doc. A string is made a Doc where it is passed: a constant as doc.Text, a computed one through
 * textDoc, and an array given as one doc as doc.Concat. So upstream's `["(", print("x"), ")"]` reads
 * `concat(doc.Text("("), print("x", nil), doc.Text(")"))`. A part left nil is the empty string.
 */

var (
	line               = doc.LineDoc
	softline           = doc.Softline
	hardline           = doc.Hardline
	literalline        = doc.Literalline
	breakParent        = doc.BreakParent
	lineSuffixBoundary = doc.LineSuffixBoundary
)

// emptyDoc is upstream's "".
var emptyDoc Doc = doc.Text("")

// internedTexts are the texts the printer writes most, each made a Doc once. A computed string becomes a
// Doc by boxing it, which allocates every time; boxing the same punctuation over and over was 7.6M of the
// printer's allocations on ahra (#fyw36kf). A constant needs none of this, since doc.Text of a constant is
// static data. A text missing here is boxed.
var internedTexts = func() map[string]Doc {
	texts := map[string]Doc{}
	for _, text := range []string{
		"", " ", ",", ", ", ";", ":", ": ", "(", ")", "{", "}", "{ ", " }", "[", "]", "<", ">", ".", "?", "!",
		"=", " =", " = ", "=>", " =>", "...", "?.", "|", "&", "| ", "& ", " | ", " & ", "+", "-", "*", "/", "%",
		"@", "#", "'", "\"", "`", "${", "?:", "?: ", "-?", "+?", "<>", "</", "/>", " />", "{}", "()", "[]",
		"++", "--", "**", "&&", "||", "??", "==", "===", "!=", "!==", "<=", ">=", "+=", "-=", "=>",
		"export", "export ", "import", "import ", "const", "const ", "let ", "var ", "return", "return ", "async",
		"async ", "await", "await ", "function", "function ", "new ", "typeof ", "keyof ", "readonly ",
		"static ", "type ", "interface ", "extends ", " extends ", "implements ", " implements ", "as ", " as ",
		"from ", " from ", "default", "default ", "case ", "if (", "if", "else", "else ", " else", " else ",
		"for (", "while (", "do", "switch (", "try", "try ", "catch", " catch", " catch ", "finally", " finally ",
		"throw ", "break", "continue", "declare ", "abstract ", "private ", "protected ", "public ",
		"override ", "enum ", "namespace ", "module ", "of ", " of ", "in ", " in ", "instanceof", "delete ",
		"void ", "yield", "yield ", "yield*", "get ", "set ", "this", "super", "null", "undefined", "true",
		"false", "is ", " is ", "asserts ", "infer ", "unique ", "satisfies", " satisfies ", "accessor ",
		"declare", "global", "out ", "in", "out", "new", "class", "class ", "debugger", "with (",
	} {
		texts[text] = doc.Text(text)
	}
	return texts
}()

// textDoc is text as a Doc, from internedTexts when it is there.
func textDoc(text string) Doc {
	if interned, present := internedTexts[text]; present {
		return interned
	}
	return doc.Text(text)
}

// orEmptyDoc is document, or emptyDoc where it is nil: a Doc that was never set reaches a builder as nil,
// and the doc the builder makes holds upstream's "" in its place.
func orEmptyDoc(document Doc) Doc {
	if document == nil {
		return emptyDoc
	}
	return document
}

// concat is upstream's array doc.
func concat(parts ...Doc) Doc {
	result := make(doc.Concat, len(parts))
	for index, part := range parts {
		result[index] = orEmptyDoc(part)
	}
	return result
}

var poisonedDoc Doc = doc.Text("CoherePoisonedDoc")

var docMemories = sync.Pool{New: func() any {
	memory := &docMemory{}
	memory.parts.Poison = poisonedDoc
	memory.sequences.Poison = doc.Sequence{Parts: []Doc{poisonedDoc}}
	memory.groups.Poison = doc.Group{Contents: poisonedDoc}
	memory.indents.Poison = doc.Indent{Contents: poisonedDoc}
	return memory
}}

// acquireDocMemory is a docMemory for one format, from the pool. Release it once the doc is laid out.
func acquireDocMemory() *docMemory {
	memory, isDocMemory := docMemories.Get().(*docMemory)
	if !isDocMemory {
		// Invariant: docMemories only ever holds a *docMemory, from its New and from release.
		panic("javascript: the doc memory pool held something other than a *docMemory")
	}
	return memory
}

// docMemoryOf is the path's print's docMemory, or nil for the heap.
func docMemoryOf(path *Path) *docMemory {
	if path.Settings == nil {
		return nil
	}
	return path.Settings.docs
}

// concatIn is concat from the format's docMemory, when the path's print has one: its parts from the slab
// and the doc a *doc.Sequence from the arena, so it allocates nothing.
func concatIn(path *Path, parts ...Doc) Doc {
	memory := docMemoryOf(path)
	if memory == nil {
		return concat(parts...)
	}
	result := memory.parts.Make(len(parts))[:len(parts)]
	for index, part := range parts {
		result[index] = orEmptyDoc(part)
	}
	return memory.sequences.New(doc.Sequence{Parts: result})
}

// sequenceIn is parts as an array doc: a *doc.Sequence from the format's docMemory when the path's print has
// one, or a Concat.
func sequenceIn(path *Path, parts []Doc) Doc {
	memory := docMemoryOf(path)
	if memory == nil {
		return doc.Concat(parts)
	}
	return memory.sequences.New(doc.Sequence{Parts: parts})
}

// partsIn is a []Doc of length count for a concatenation the caller fills, from the format's docMemory when
// the path's print has one.
func partsIn(path *Path, count int) []Doc {
	memory := docMemoryOf(path)
	if memory == nil {
		return make([]Doc, count)
	}
	return memory.parts.Make(count)[:count]
}

// groupIn is group with the group from the format's docMemory.
func groupIn(path *Path, contents Doc) Doc {
	return groupWithIn(path, contents, doc.GroupOptions{})
}

// groupWithIn is groupWith with the group from the format's docMemory. It builds what doc.NewGroup does.
func groupWithIn(path *Path, contents Doc, options doc.GroupOptions) Doc {
	memory := docMemoryOf(path)
	if memory == nil {
		return groupWith(contents, options)
	}
	return memory.groups.New(doc.Group{
		Id:             options.Id,
		Contents:       orEmptyDoc(contents),
		Break:          options.ShouldBreak,
		ExpandedStates: options.ExpandedStates,
	})
}

// indentIn is indent with the indent from the format's docMemory. It builds what doc.NewIndent does.
func indentIn(path *Path, contents Doc) Doc {
	memory := docMemoryOf(path)
	if memory == nil {
		return indent(contents)
	}
	return memory.indents.New(doc.Indent{Contents: orEmptyDoc(contents)})
}

// groupWith is upstream's group(contents, options).
func groupWith(contents Doc, options doc.GroupOptions) Doc {
	return doc.NewGroup(orEmptyDoc(contents), options)
}

// conditionalGroup is upstream's conditionalGroup(states, options).
func conditionalGroup(states []Doc, options doc.GroupOptions) Doc {
	return doc.ConditionalGroup(states, options)
}

// indent is upstream's indent(contents).
func indent(contents Doc) Doc { return doc.NewIndent(orEmptyDoc(contents)) }

// align is upstream's align(width, contents) for a numeric width.
func align(width int, contents Doc) Doc { return doc.NewAlign(width, orEmptyDoc(contents)) }

// dedent is upstream's dedent(contents).
func dedent(contents Doc) Doc { return doc.Dedent(orEmptyDoc(contents)) }

// dedentToRoot is upstream's dedentToRoot(contents).
func dedentToRoot(contents Doc) Doc { return doc.DedentToRoot(orEmptyDoc(contents)) }

// markAsRoot is upstream's markAsRoot(contents).
func markAsRoot(contents Doc) Doc { return doc.MarkAsRoot(orEmptyDoc(contents)) }

// fill is upstream's fill(parts).
func fill(parts []Doc) Doc { return doc.NewFill(parts) }

// ifBreak is upstream's ifBreak(breakContents, flatContents).
func ifBreak(breakContents Doc, flatContents Doc) Doc {
	return doc.NewIfBreak(orEmptyDoc(breakContents), orEmptyDoc(flatContents), nil)
}

// ifBreakWithGroup is upstream's ifBreak(breakContents, flatContents, { groupId }).
func ifBreakWithGroup(breakContents Doc, flatContents Doc, groupId *doc.GroupId) Doc {
	return doc.NewIfBreak(orEmptyDoc(breakContents), orEmptyDoc(flatContents), groupId)
}

// indentIfBreak is upstream's indentIfBreak(contents, { groupId, negate }).
func indentIfBreak(contents Doc, groupId *doc.GroupId, negate bool) Doc {
	return doc.NewIndentIfBreak(orEmptyDoc(contents), groupId, negate)
}

// label is upstream's label(name, contents).
func label(name string, contents Doc) Doc { return doc.NewLabel(name, orEmptyDoc(contents)) }

// join is upstream's join(separator, parts).
func join(separator Doc, parts []Doc) Doc { return doc.Join(orEmptyDoc(separator), parts) }

// newGroupId is upstream's Symbol(name) used as a group id.
func newGroupId(name string) *doc.GroupId { return doc.NewGroupId(name) }

// labelOf is upstream's `doc.label`, the label of a labelled doc, or "".
func labelOf(document Doc) string {
	if labelled, isLabel := document.(*doc.Label); isLabel {
		return labelled.Label
	}
	return ""
}

// isEmptyString is upstream's `doc === ""`.
func isEmptyString(document Doc) bool {
	text, isText := document.(doc.Text)
	return document == nil || isText && text == ""
}
