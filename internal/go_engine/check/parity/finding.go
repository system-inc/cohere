package parity

import (
	"encoding/json"
	"fmt"
	"go/token"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// Finding is one finding as the comparison sees it: where it starts and ends, its message, the positions it relates,
// and the fixes it suggests. The engine writes every one of them into the finding's record, so a port that differs in
// any of them differs in what the engine prints, and two findings are the same only when all of them are.
//
// File, and every file a related entry or an edit names, is named in the fixtures by its path below their root, as
// fixtureLoad.fileName names it, and is the path in Analyze's outcomes, since two packages can each hold a file of
// the same name.
type Finding struct {
	File   string
	Line   int
	Column int

	// EndLine and EndColumn are where the finding's range ends, or 0 when the analyzer gave no end.
	EndLine   int
	EndColumn int

	Message string

	// Related are the diagnostic's related entries, and Fixes its suggested fixes, each in the order it gave them.
	Related []Related `json:",omitempty"`
	Fixes   []Fix     `json:",omitempty"`
}

// Related is one position a finding relates, with its range and what it says there.
type Related struct {
	File      string
	Line      int
	Column    int
	EndLine   int
	EndColumn int
	Message   string
}

// Fix is one suggested fix: its message and its edits, in order.
type Fix struct {
	Message string
	Edits   []Edit `json:",omitempty"`
}

// Edit is one text edit of a fix, by byte offsets in its file, as the engine records one. A token.Pos would differ
// between two loads of the same file, and an offset does not.
type Edit struct {
	File    string
	Start   int
	End     int
	NewText string
}

// newFinding is diagnostic as the comparison sees it, its positions read through fileSet.
func newFinding(fileSet *token.FileSet, diagnostic analysis.Diagnostic) Finding {
	start := fileSet.Position(diagnostic.Pos)
	end := endOf(fileSet, diagnostic.End)
	finding := Finding{
		File:      start.Filename,
		Line:      start.Line,
		Column:    start.Column,
		EndLine:   end.Line,
		EndColumn: end.Column,
		Message:   diagnostic.Message,
	}
	for _, related := range diagnostic.Related {
		position := fileSet.Position(related.Pos)
		relatedEnd := endOf(fileSet, related.End)
		finding.Related = append(finding.Related, Related{
			File:      position.Filename,
			Line:      position.Line,
			Column:    position.Column,
			EndLine:   relatedEnd.Line,
			EndColumn: relatedEnd.Column,
			Message:   related.Message,
		})
	}
	for _, suggested := range diagnostic.SuggestedFixes {
		fix := Fix{Message: suggested.Message}
		for _, textEdit := range suggested.TextEdits {
			fix.Edits = append(fix.Edits, editOf(fileSet, textEdit))
		}
		finding.Fixes = append(finding.Fixes, fix)
	}
	return finding
}

// endOf is where an end position is, or the zero position when the analyzer gave none.
func endOf(fileSet *token.FileSet, end token.Pos) token.Position {
	if !end.IsValid() {
		return token.Position{}
	}
	return fileSet.Position(end)
}

// editOf is textEdit by offsets in its file. An edit with no end inserts at its start, as x/tools applies one.
func editOf(fileSet *token.FileSet, textEdit analysis.TextEdit) Edit {
	file := fileSet.File(textEdit.Pos)
	if file == nil {
		// The checker refuses a fix whose edit is in no file it loaded, so this is never met; were it met, the edit
		// still counts, named by what is known of it, rather than vanishing from the comparison.
		return Edit{Start: int(textEdit.Pos), End: int(textEdit.End), NewText: string(textEdit.NewText)}
	}
	start := file.Offset(textEdit.Pos)
	end := start
	if textEdit.End.IsValid() {
		end = file.Offset(textEdit.End)
	}
	return Edit{File: file.Name(), Start: start, End: end, NewText: string(textEdit.NewText)}
}

// String is the finding as a finding line prints it, its range, related entries and fixes on the same line.
func (finding Finding) String() string {
	described := &strings.Builder{}
	described.WriteString(span(finding.File, finding.Line, finding.Column, finding.EndLine, finding.EndColumn))
	described.WriteString(": " + finding.Message)
	for _, related := range finding.Related {
		described.WriteString(" [related " +
			span(related.File, related.Line, related.Column, related.EndLine, related.EndColumn) + ": " +
			related.Message + "]")
	}
	for _, fix := range finding.Fixes {
		described.WriteString(fmt.Sprintf(" [fix %q:", fix.Message))
		for _, edit := range fix.Edits {
			described.WriteString(fmt.Sprintf(" %s[%d:%d] = %q", edit.File, edit.Start, edit.End, edit.NewText))
		}
		described.WriteString("]")
	}
	return described.String()
}

// span is a position as a finding line prints it, with its end when it has one.
func span(file string, line int, column int, endLine int, endColumn int) string {
	if endLine == 0 {
		return fmt.Sprintf("%s:%d:%d", file, line, column)
	}
	return fmt.Sprintf("%s:%d:%d-%d:%d", file, line, column, endLine, endColumn)
}

// WithFileNames is the finding with every file it names, its own, its related entries' and its edits', renamed by
// rename, as a caller shortens paths for its report.
func (finding Finding) WithFileNames(rename func(string) string) Finding {
	renamed := finding
	renamed.File = rename(finding.File)
	renamed.Related = nil
	for _, related := range finding.Related {
		related.File = rename(related.File)
		renamed.Related = append(renamed.Related, related)
	}
	renamed.Fixes = nil
	for _, fix := range finding.Fixes {
		renamedFix := Fix{Message: fix.Message}
		for _, edit := range fix.Edits {
			edit.File = rename(edit.File)
			renamedFix.Edits = append(renamedFix.Edits, edit)
		}
		renamed.Fixes = append(renamed.Fixes, renamedFix)
	}
	return renamed
}

// key is the finding as Difference counts it: every field, encoded so that no two different findings share a key,
// as two printed lines could if a message spelled out another finding's related entry. An empty list encodes as a
// missing one, so a finding built with no related entries and one built with an empty list are the same.
func (finding Finding) key() string {
	encoded, encodeError := json.Marshal(finding)
	if encodeError != nil {
		// Invariant: a finding holds only strings, integers and slices of structs of them, which always encode.
		panic(encodeError)
	}
	return string(encoded)
}

// Difference is what of from is not in without, counting repeats, so a finding reported twice by one side and once by
// the other is a difference. Every field counts: the range, the related entries and the fixes as much as the message.
func Difference(from []Finding, without []Finding) []Finding {
	remaining := map[string]int{}
	for _, finding := range without {
		remaining[finding.key()]++
	}
	missing := []Finding{}
	for _, finding := range from {
		if remaining[finding.key()] > 0 {
			remaining[finding.key()]--
			continue
		}
		missing = append(missing, finding)
	}
	return missing
}
