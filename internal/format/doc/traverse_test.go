package doc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// showDoc is a canonical serialization of a doc's structure, so a comparison of two docs catches a pair that
// prints the same but builds a different tree.
func showDoc(document Doc) string {
	quote := func(text string) string {
		var buffer bytes.Buffer
		encoder := json.NewEncoder(&buffer)
		encoder.SetEscapeHTML(false)
		// Ignored: a string always has a JSON form, and a bytes.Buffer never fails a write.
		_ = encoder.Encode(text)
		return strings.TrimSuffix(buffer.String(), "\n")
	}
	list := func(parts []Doc) string {
		shown := make([]string, len(parts))
		for index, part := range parts {
			shown[index] = showDoc(part)
		}
		return strings.Join(shown, ",")
	}
	idName := func(id *GroupId) string {
		if id == nil {
			return ""
		}
		return id.name
	}
	switch typed := document.(type) {
	case nil:
		return "undefined"
	case Text:
		return quote(string(typed))
	case *Group:
		result := "group(" + idName(typed.Id) + "," + fmt.Sprint(typed.Break) + "," + showDoc(typed.Contents)
		if typed.ExpandedStates != nil {
			result += ",states[" + list(typed.ExpandedStates) + "]"
		}
		return result + ")"
	case *Fill:
		return "fill[" + list(typed.Parts) + "]"
	case *IfBreak:
		return "ifBreak(" +
			idName(typed.GroupId) +
			"," +
			showDoc(typed.BreakContents) +
			"," +
			showDoc(typed.FlatContents) +
			")"
	case *Indent:
		return "indent(" + showDoc(typed.Contents) + ")"
	case *Align:
		var argument string
		switch typed.Kind {
		case AlignWidth:
			argument = fmt.Sprint(typed.Width)
		case AlignString:
			argument = quote(typed.String)
		case AlignRoot:
			argument = "markRoot"
		case AlignDedentToRoot:
			argument = "root"
		}
		return "align(" + argument + "," + showDoc(typed.Contents) + ")"
	case *IndentIfBreak:
		return "indentIfBreak(" +
			idName(typed.GroupId) +
			"," +
			fmt.Sprint(typed.Negate) +
			"," +
			showDoc(typed.Contents) +
			")"
	case *LineSuffix:
		return "lineSuffix(" + showDoc(typed.Contents) + ")"
	case lineSuffixBoundaryDoc:
		return "lineSuffixBoundary"
	case *Line:
		return "line(" + fmt.Sprint(typed.Soft) + "," + fmt.Sprint(typed.Hard) + "," + fmt.Sprint(typed.Literal) + ")"
	case breakParentDoc:
		return "breakParent"
	case trimDoc:
		return "trim"
	case *Label:
		return "label(" + typed.Label + "," + showDoc(typed.Contents) + ")"
	}
	if parts, isArray := Parts(document); isArray {
		return "[" + list(parts) + "]"
	}
	panic(fmt.Sprintf("unknown doc %T", document))
}
