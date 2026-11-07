package rename

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/microsoft/TypeScript/tsc/shim/scanner"
	"github.com/system-inc/cohere/internal/format/format_files"
	"github.com/system-inc/cohere/internal/types/program"
)

// A rename used to leave every comment and doc naming the old symbol alone, so the comment above the code it
// renamed went on naming a symbol that no longer existed, and lied (#p9q52mg). The checker proves code references
// and nothing else, so a text mention is a different kind of site: found by whole-word match, listed in the dry
// run apart from the edits, and rewritten only when asked (`--comments`) and only where a match is likely to mean
// this symbol.
//
// Where a mention may be rewritten:
//
//	comment          a comment in a file the rename touches. Rewritten when the name is distinctive.
//	markdown         prose in the repository's markdown. Rewritten when the name is distinctive.
//	string           a string or template literal in a file the rename touches. Never rewritten: a string is
//	                 a message, a key or a lookup, and changing one changes what the program does.
//	markdown code    a fenced code block in markdown. Never rewritten: an example may be another program's
//	                 code, or output, and is the reader's to change.
//
// A distinctive name is one a text match is unlikely to hit by accident: it has a capital after its first
// letter, as camelCase and PascalCase names of more than one word do, or an underscore, a dollar sign or a digit.
// A plain word, `data` or `value` or `Value`, is listed and never rewritten, since a comment saying "the value"
// is English, not a reference.

// MentionPlace is where a text mention sits.
type MentionPlace string

const (
	// MentionComment is a comment in a file the rename touches.
	MentionComment MentionPlace = "comment"
	// MentionString is a string or template literal in a file the rename touches.
	MentionString MentionPlace = "string"
	// MentionMarkdown is prose in a markdown file.
	MentionMarkdown MentionPlace = "markdown"
	// MentionMarkdownCode is a fenced code block in a markdown file.
	MentionMarkdownCode MentionPlace = "markdown code"
)

// Mention is one whole-word occurrence of the old name in text the checker does not read.
type Mention struct {
	FileName string
	// Range is the occurrence's own bytes.
	Range  core.TextRange
	Line   int
	Column int
	// LineText is the line it sits on, trimmed, for the reader to judge.
	LineText string
	Place    MentionPlace
	// Rewritable is whether `--comments` rewrites it. When it does not, Why says so.
	Rewritable bool
	Why        string
}

// IsDistinctive reports whether a name is unlikely to match text by accident. See the package's mention notes.
func IsDistinctive(name string) bool {
	for index, character := range name {
		switch {
		case index > 0 && character >= 'A' && character <= 'Z':
			return true
		case character == '_' || character == '$' || (character >= '0' && character <= '9'):
			return true
		}
	}
	return false
}

// CollectMentions records the old name's text mentions: in the comments and strings of the files the plan
// touches, and in the markdown under markdownRoot, which the format walk enumerates, so what git ignores and
// what the settings ignore is left out as the formatter leaves it. markdownRoot empty skips markdown.
//
// Mutates: plan its Mentions are set, sorted by file and position
func CollectMentions(graph *program.Graph, plan *Plan, markdownRoot string) error {
	if plan.OldName == "" || len(plan.Refusals) > 0 {
		return nil
	}
	distinctive := IsDistinctive(plan.OldName)
	touched := map[string]bool{}
	for _, edit := range plan.Edits {
		touched[edit.FileName] = true
	}
	for _, file := range graph.ProjectFiles() {
		if touched[file.FileName().AsString()] {
			plan.Mentions = append(plan.Mentions, sourceMentions(file, plan.OldName, distinctive)...)
		}
	}
	if markdownRoot != "" {
		enumeration, enumerateError := format_files.Enumerate(markdownRoot, func(fileName string) bool {
			return strings.EqualFold(filepath.Ext(fileName), ".md")
		})
		if enumerateError != nil {
			return fmt.Errorf("listing the markdown under %s for mentions of %s: %w", markdownRoot, plan.OldName,
				enumerateError)
		}
		plan.MarkdownRoot = markdownRoot
		for _, fileName := range enumeration.Files {
			contents, readError := os.ReadFile(fileName)
			if readError != nil {
				return fmt.Errorf("reading %s for mentions of %s: %w", fileName, plan.OldName, readError)
			}
			plan.Mentions = append(plan.Mentions, markdownMentions(fileName, string(contents), plan.OldName,
				distinctive)...)
		}
	}
	sort.Slice(plan.Mentions, func(first int, second int) bool {
		if plan.Mentions[first].FileName != plan.Mentions[second].FileName {
			return plan.Mentions[first].FileName < plan.Mentions[second].FileName
		}
		return plan.Mentions[first].Range.Pos() < plan.Mentions[second].Range.Pos()
	})
	return nil
}

// sourceMentions are the old name's whole-word occurrences in one source file's comments and strings. Comments
// are every leading and trailing comment the scanner finds at a node's edges, which is every comment in the
// file, each counted once. A string equal to the old name is left to the blindness list, which already names it.
func sourceMentions(file *ast.SourceFile, name string, distinctive bool) []Mention {
	text := file.Text()
	if !strings.Contains(text, name) {
		return nil
	}
	fileName := file.FileName().AsString()
	var mentions []Mention
	seen := map[int]bool{}
	var factory ast.NodeFactory
	addComment := func(comment ast.CommentRange) {
		if seen[comment.Pos()] {
			return
		}
		seen[comment.Pos()] = true
		mentions = append(mentions, wordMentions(fileName, text, comment.Pos(), comment.End(), name,
			MentionComment, distinctive)...)
	}
	var visit func(node *ast.Node) bool
	visit = func(node *ast.Node) bool {
		if node == nil {
			return false
		}
		for comment := range scanner.GetLeadingCommentRanges(&factory, text, node.Pos()) {
			addComment(comment)
		}
		for comment := range scanner.GetTrailingCommentRanges(&factory, text, node.End()) {
			addComment(comment)
		}
		switch node.Kind {
		case ast.KindStringLiteral, ast.KindNoSubstitutionTemplateLiteral, ast.KindTemplateHead,
			ast.KindTemplateMiddle, ast.KindTemplateTail:
			if node.Text() != name {
				span := tokenSpan(file, node)
				mentions = append(mentions, wordMentions(fileName, text, span.Pos(), span.End(), name,
					MentionString, distinctive)...)
			}
		}
		node.ForEachChild(visit)
		return false
	}
	file.AsNode().ForEachChild(visit)
	if endOfFile := file.EndOfFileToken; endOfFile != nil {
		for comment := range scanner.GetLeadingCommentRanges(&factory, text, endOfFile.Pos()) {
			addComment(comment)
		}
	}
	return mentions
}

// markdownMentions are the old name's whole-word occurrences in one markdown file, those inside a fenced code
// block marked as code.
func markdownMentions(fileName string, text string, name string, distinctive bool) []Mention {
	if !strings.Contains(text, name) {
		return nil
	}
	var mentions []Mention
	fence := ""
	offset := 0
	for _, line := range strings.SplitAfter(text, "\n") {
		trimmed := strings.TrimLeft(line, " ")
		opening := ""
		for _, marker := range []string{"```", "~~~"} {
			if strings.HasPrefix(trimmed, marker) {
				opening = marker
			}
		}
		place := MentionMarkdown
		switch {
		case fence != "":
			place = MentionMarkdownCode
			if opening == fence {
				fence = ""
			}
		case opening != "":
			place, fence = MentionMarkdownCode, opening
		}
		mentions = append(mentions, wordMentions(fileName, text, offset, offset+len(line), name, place,
			distinctive)...)
		offset += len(line)
	}
	return mentions
}

// wordMentions are the whole-word occurrences of name in text between start and end, all in one place.
func wordMentions(
	fileName string,
	text string,
	start int,
	end int,
	name string,
	place MentionPlace,
	distinctive bool,
) []Mention {
	var mentions []Mention
	for searched := start; searched < end; {
		found := strings.Index(text[searched:end], name)
		if found < 0 {
			break
		}
		position := searched + found
		searched = position + len(name)
		if isWordCharacter(text, position-1) || isWordCharacter(text, position+len(name)) {
			continue
		}
		line, column := lineColumnOf(text, position)
		mention := Mention{
			FileName: fileName,
			Range:    core.NewTextRange(position, position+len(name)),
			Line:     line,
			Column:   column,
			LineText: lineAround(text, position),
			Place:    place,
		}
		switch {
		case place == MentionString:
			mention.Why = "a string may be a message, a key or a lookup, so it is never rewritten"
		case place == MentionMarkdownCode:
			mention.Why = "a code fence may hold another program's code or output, so it is never rewritten"
		case !distinctive:
			mention.Why = fmt.Sprintf("%q is a plain word, which a text match may not mean, so it is never rewritten",
				name)
		default:
			mention.Rewritable = true
		}
		mentions = append(mentions, mention)
	}
	return mentions
}

// isWordCharacter reports whether the byte at index continues an identifier, so a match beside it is part of a
// longer word. Outside the text is no word.
func isWordCharacter(text string, index int) bool {
	if index < 0 || index >= len(text) {
		return false
	}
	character := text[index]
	return (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
		(character >= '0' && character <= '9') || character == '_' || character == '$'
}

// lineAround is the line holding position, trimmed and cut to what a reader scans.
func lineAround(text string, position int) string {
	start := strings.LastIndexByte(text[:position], '\n') + 1
	end := strings.IndexByte(text[position:], '\n')
	if end < 0 {
		end = len(text)
	} else {
		end += position
	}
	line := strings.TrimSpace(text[start:end])
	if runes := []rune(line); len(runes) > 100 {
		line = string(runes[:100]) + "..."
	}
	return line
}
