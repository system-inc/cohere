package javascript

import (
	"fmt"
	"sync"

	"github.com/system-inc/cohere/internal/format/arena"
	"github.com/system-inc/cohere/internal/format/doc"
)

/*
 * Upstream's doc builders under upstream's names, so a print function ports line for line.
 *
 * JavaScript docs are strings, arrays and objects freely mixed; Go's are typed. toDoc is the one place
 * that bridges them: a string becomes Text, a slice becomes Concat, nil becomes the empty string. Every
 * builder here takes `any` through toDoc, so `concat("(", print("x", nil), ")")` reads like upstream's
 * `["(", print("x"), ")"]`. A value toDoc does not recognise is a programming error and panics, rather
 * than printing nothing.
 */

var (
	line                       = doc.LineDoc
	softline                   = doc.Softline
	hardline                   = doc.Hardline
	literalline                = doc.Literalline
	hardlineWithoutBreakParent = doc.HardlineWithoutBreakParent
	breakParent                = doc.BreakParent
	lineSuffixBoundary         = doc.LineSuffixBoundary
	trim                       = doc.Trim
)

// emptyDoc is upstream's "".
var emptyDoc Doc = doc.Text("")

// internedTexts are the texts the printer writes most, each made a Doc once. A string becomes a Doc by
// boxing it, which allocates every time; toDoc boxing the same punctuation over and over was 7.6M of the
// printer's allocations on ahra (#fyw36kf). A text missing here is boxed as before.
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

// toDoc converts a JavaScript-shaped doc value.
func toDoc(value any) Doc {
	switch typed := value.(type) {
	case nil:
		return emptyDoc
	case string:
		return textDoc(typed)
	case doc.Doc:
		return typed
	case []Doc:
		return doc.Concat(typed)
	case []any:
		parts := make(doc.Concat, len(typed))
		for index, part := range typed {
			parts[index] = toDoc(part)
		}
		return parts
	}
	panic(fmt.Sprintf("javascript: %T is not a doc", value))
}

// concat is upstream's array doc.
func concat(parts ...any) Doc {
	result := make(doc.Concat, len(parts))
	for index, part := range parts {
		result[index] = toDoc(part)
	}
	return result
}

// concatIn is concat with its parts from the format's slab (settings.docs), when the path's print has one:
// a fresh slice per concatenation was half of concat's 8M allocations a pass on ahra (#fyw36kf). The
// other half, boxing the slice into a Doc, stays.
func concatIn(path *Path, parts ...any) Doc {
	formatSettings, _ := path.Settings.(*settings)
	if formatSettings == nil || formatSettings.docs == nil {
		return concat(parts...)
	}
	result := doc.Concat(formatSettings.docs.Make(len(parts))[:len(parts)])
	for index, part := range parts {
		result[index] = toDoc(part)
	}
	return result
}

// docSlabs hold the slabs concatIn takes parts from, between formats. A released part reads as
// poisonedDoc under cohere_poison, so a doc read after its file was printed shows in the output.
var docSlabs = sync.Pool{New: func() any { return &arena.Slab[doc.Doc]{Poison: poisonedDoc} }}

var poisonedDoc Doc = doc.Text("CoherePoisonedDoc")

// docs converts a list of doc values, for the builders that take arrays.
func docs(parts ...any) []Doc {
	result := make([]Doc, len(parts))
	for index, part := range parts {
		result[index] = toDoc(part)
	}
	return result
}

// group is upstream's group(contents).
func group(contents any) Doc {
	return doc.NewGroup(toDoc(contents), doc.GroupOptions{})
}

// groupWith is upstream's group(contents, options).
func groupWith(contents any, options doc.GroupOptions) Doc {
	return doc.NewGroup(toDoc(contents), options)
}

// conditionalGroup is upstream's conditionalGroup(states, options).
func conditionalGroup(states []Doc, options doc.GroupOptions) Doc {
	return doc.ConditionalGroup(states, options)
}

// indent is upstream's indent(contents).
func indent(contents any) Doc { return doc.NewIndent(toDoc(contents)) }

// align is upstream's align(width, contents) for a numeric width.
func align(width int, contents any) Doc { return doc.NewAlign(width, toDoc(contents)) }

// alignString is upstream's align(string, contents).
func alignString(prefix string, contents any) Doc {
	return doc.AlignWithString(prefix, toDoc(contents))
}

// dedent is upstream's dedent(contents).
func dedent(contents any) Doc { return doc.Dedent(toDoc(contents)) }

// dedentToRoot is upstream's dedentToRoot(contents).
func dedentToRoot(contents any) Doc { return doc.DedentToRoot(toDoc(contents)) }

// markAsRoot is upstream's markAsRoot(contents).
func markAsRoot(contents any) Doc { return doc.MarkAsRoot(toDoc(contents)) }

// fill is upstream's fill(parts).
func fill(parts []Doc) Doc { return doc.NewFill(parts) }

// ifBreak is upstream's ifBreak(breakContents, flatContents).
func ifBreak(breakContents any, flatContents any) Doc {
	return doc.NewIfBreak(toDoc(breakContents), toDoc(flatContents), nil)
}

// ifBreakWithGroup is upstream's ifBreak(breakContents, flatContents, { groupId }).
func ifBreakWithGroup(breakContents any, flatContents any, groupID *doc.GroupID) Doc {
	return doc.NewIfBreak(toDoc(breakContents), toDoc(flatContents), groupID)
}

// indentIfBreak is upstream's indentIfBreak(contents, { groupId, negate }).
func indentIfBreak(contents any, groupID *doc.GroupID, negate bool) Doc {
	return doc.NewIndentIfBreak(toDoc(contents), groupID, negate)
}

// lineSuffix is upstream's lineSuffix(contents).
func lineSuffix(contents any) Doc { return doc.NewLineSuffix(toDoc(contents)) }

// label is upstream's label(name, contents).
func label(name string, contents any) Doc { return doc.NewLabel(name, toDoc(contents)) }

// join is upstream's join(separator, parts).
func join(separator any, parts []Doc) Doc { return doc.Join(toDoc(separator), parts) }

// newGroupID is upstream's Symbol(name) used as a group id.
func newGroupID(name string) *doc.GroupID { return doc.NewGroupID(name) }

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
