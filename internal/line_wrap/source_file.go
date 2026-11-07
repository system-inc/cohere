package line_wrap

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_long_line"
)

// Category says whether a long line is a violation the tool should fix, or why it is not one.
type Category string

const (
	// CategoryActionable is a violation.
	CategoryActionable Category = "Actionable"
	// CategoryStringAlone is a line whose overflow is one string literal that starts the line, followed by
	// nothing but closing punctuation. A long string is left whole once it sits on its own line.
	CategoryStringAlone Category = "StringAlone"
	// CategoryRawStringBody is a line past the opening line of a multi-line raw string, never judged.
	CategoryRawStringBody Category = "RawStringBody"
	// CategoryKeyedString is a line that starts with `key: `, `name = ` or `name := ` and a string literal
	// followed by nothing but closing punctuation. Only with Options.ExemptKeyedStrings, pending a ruling.
	CategoryKeyedString Category = "KeyedString"
	// CategoryUnbrokenToken is a comment whose overflow is one word with no spaces, such as a URL.
	CategoryUnbrokenToken Category = "UnbrokenToken"
	CategoryDirective     Category = "Directive"
	CategoryImport        Category = "Import"
	// CategoryNotReported is a long line consistency_no_long_line leaves alone, only with Options.ReportedOnly.
	CategoryNotReported Category = "NotReported"
	// CategoryNotCured is a long line left as gofmt printed it: no wrap brings it within the limit, with
	// Options.CuredOnly, or its wrap would move a comment, with Options.KeepComments.
	CategoryNotCured Category = "NotCured"
	// CategoryComment is a long line that opens inside a comment, left as it is, only with Options.KeepComments.
	CategoryComment Category = "Comment"
)

// Categories lists every category in report order.
var Categories = []Category{
	CategoryActionable,
	CategoryStringAlone,
	CategoryKeyedString,
	CategoryRawStringBody,
	CategoryUnbrokenToken,
	CategoryDirective,
	CategoryImport,
	CategoryNotReported,
	CategoryNotCured,
	CategoryComment,
}

// Options are the choices a run makes that a ruling may still change.
type Options struct {
	// ExemptKeyedStrings exempts a line that starts with `key: `, `name = ` or `name := ` and a string literal
	// followed by nothing but closing punctuation, as Prettier leaves such a line. gofmt joins a break after
	// a composite key's colon or a spec's `=` back onto one line, so no rewrap can put those strings alone.
	ExemptKeyedStrings bool

	// ReportedOnly wraps only the long lines consistency_no_long_line reports, every other one counted as
	// CategoryNotReported, so a wrap that fixes the rule's findings never rewrites a line the rule leaves alone. The
	// fix phase sets it; the command line does not, and judges by the categories above alone.
	ReportedOnly bool

	// CuredOnly keeps a wrap only where it cures: every long line the wrap touches, past CategoryNotReported, must
	// end with each of its pieces within the limit. A long line a wrap would touch and leave long is left as it was,
	// and so is every construct around it, and the wrap is tried again without them; when that cannot be done, the
	// file is left as it was. A wrap that breaks a line and leaves it long is churn with no cure. The fix phase sets
	// it.
	CuredOnly bool

	// KeepComments leaves every comment as it is: no prose comment is reflowed, a long line that opens inside a
	// comment is CategoryComment, and a construct whose break would change a comment's text, move it onto a line of
	// its own or off one, change the indentation of one alone on its line, or move what follows it onto another line
	// is left as it was, and the wrap is tried again without it. Comments are data that line-based readers parse (an
	// Example's `// Output:` block, a directive, a project's own marker lines), so a fix must not rewrite them, and a
	// long comment stays a finding for a person. The fix phase sets it. The command line does not: whoever runs it
	// reviews its diff, and taking a tree to zero by hand needs its prose comments reflowed.
	KeepComments bool

	// uncured are the spans of the source, in kept bytes (see keptByte), of the lines a CuredOnly or KeepComments wrap
	// leaves alone. Wrap sets it between its tries; a byte's kept index is the same in every pass, since a wrap only
	// moves whitespace, commas and comment slashes.
	uncured []keptSpan
}

// commentSpan is one comment of the file, `//` or `/* */`, with byte offsets.
type commentSpan struct {
	start, end int
	text       string
}

// sourceFile is one parse of a source buffer with the indexes every pass needs. Every span comes from the AST
// and every line number from the token.File's line table.
type sourceFile struct {
	source     []byte
	fileSet    *token.FileSet
	file       *ast.File
	tokenFile  *token.File
	lineStarts []int
	comments   []commentSpan
	// literalStarts maps the start offset of every string literal to it.
	literalStarts map[int]*ast.BasicLit
	literals      []*ast.BasicLit
	rawBodyLines  map[int]bool
	// stringAloneLines holds the lines that start with a string literal followed by nothing but closing
	// punctuation.
	stringAloneLines map[int]bool
	// keyedStarts maps the offset of a key or name that starts its line to the string literal it is given:
	// `key: "…"` in a composite literal, `name = "…"` in a var or const spec or an assignment, `name := "…"`.
	keyedStarts map[int]*ast.BasicLit
	importLines map[int]bool
	// reportedLines are the lines consistency_no_long_line reports, read only with Options.ReportedOnly.
	reportedLines map[int]bool
	// keptStarts holds, for each line from the first and then for the end of the source, how many kept bytes come
	// before it, read only with Options.CuredOnly or Options.KeepComments.
	keptStarts []int
	options    Options
}

func parseSource(source []byte, options Options) (*sourceFile, error) {
	fileSet := token.NewFileSet()
	file, parseFileError := parser.ParseFile(fileSet, "", source, parser.ParseComments|parser.SkipObjectResolution)
	if parseFileError != nil {
		return nil, parseFileError
	}
	sourceFile := &sourceFile{
		source:           source,
		fileSet:          fileSet,
		file:             file,
		tokenFile:        fileSet.File(file.Pos()),
		literalStarts:    map[int]*ast.BasicLit{},
		rawBodyLines:     map[int]bool{},
		stringAloneLines: map[int]bool{},
		keyedStarts:      map[int]*ast.BasicLit{},
		importLines:      map[int]bool{},
		reportedLines:    map[int]bool{},
		options:          options,
	}
	sourceFile.lineStarts = sourceFile.tokenFile.Lines()
	if options.CuredOnly || options.KeepComments {
		sourceFile.keptStarts = keptStarts(source, sourceFile.lineStarts)
	}
	if options.ReportedOnly {
		for _, line := range consistency_no_long_line.ReportedLines(file, sourceFile.tokenFile, source) {
			sourceFile.reportedLines[line] = true
		}
	}
	for _, group := range file.Comments {
		for _, comment := range group.List {
			sourceFile.comments = append(sourceFile.comments, commentSpan{
				start: sourceFile.offset(comment.Pos()),
				end:   sourceFile.offset(comment.End()),
				text:  comment.Text,
			})
		}
	}
	ast.Inspect(file, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.BasicLit:
			if typed.Kind == token.STRING {
				sourceFile.literals = append(sourceFile.literals, typed)
				sourceFile.literalStarts[sourceFile.offset(typed.Pos())] = typed
			}
		case *ast.KeyValueExpr:
			sourceFile.addKeyed(typed.Key, typed.Value)
		case *ast.ValueSpec:
			if len(typed.Names) == 1 && typed.Type == nil && len(typed.Values) == 1 {
				sourceFile.addKeyed(typed.Names[0], typed.Values[0])
			}
		case *ast.AssignStmt:
			if (typed.Tok == token.ASSIGN || typed.Tok == token.DEFINE) && len(typed.Lhs) == 1 && len(typed.Rhs) == 1 {
				if _, isName := typed.Lhs[0].(*ast.Ident); isName {
					sourceFile.addKeyed(typed.Lhs[0], typed.Rhs[0])
				}
			}
		case *ast.ImportSpec:
			for line := sourceFile.lineOfPosition(typed.Pos()); line <= sourceFile.lineOfPosition(typed.End()); line++ {
				sourceFile.importLines[line] = true
			}
		}
		return true
	})
	for _, literal := range sourceFile.literals {
		startLine := sourceFile.lineOfPosition(literal.Pos())
		for line := startLine + 1; line <= sourceFile.lineOfPosition(literal.End()); line++ {
			sourceFile.rawBodyLines[line] = true
		}
		lineStart, lineEnd := sourceFile.lineBounds(startLine)
		first := lineStart +
			len(sourceFile.lineText(startLine)) - len(strings.TrimLeft(sourceFile.lineText(startLine), " \t"))
		if first == sourceFile.offset(literal.Pos()) && sourceFile.aloneFrom(first, lineEnd) {
			sourceFile.stringAloneLines[startLine] = true
		}
	}
	return sourceFile, nil
}

// addKeyed records a key or name given a string literal on the same line, when the key starts its line. A
// composite key may be any one-token key: a name, or a string or number literal.
func (sourceFile *sourceFile) addKeyed(key ast.Expr, value ast.Expr) {
	literal, isLiteral := value.(*ast.BasicLit)
	if !isLiteral || literal.Kind != token.STRING {
		return
	}
	switch key.(type) {
	case *ast.Ident, *ast.BasicLit:
	default:
		return
	}
	keyStart := sourceFile.offset(key.Pos())
	if sourceFile.lineOf(keyStart) != sourceFile.lineOfPosition(literal.Pos()) {
		return
	}
	lineStart, _ := sourceFile.lineBounds(sourceFile.lineOf(keyStart))
	if strings.TrimLeft(string(sourceFile.source[lineStart:keyStart]), " \t") != "" {
		return
	}
	sourceFile.keyedStarts[keyStart] = literal
}

// keyedFrom reports whether a key or name starts at offset, given a string literal followed by nothing but
// closing punctuation up to end. It is always false unless Options.ExemptKeyedStrings is set.
func (sourceFile *sourceFile) keyedFrom(offset, end int) bool {
	if !sourceFile.options.ExemptKeyedStrings {
		return false
	}
	literal, found := sourceFile.keyedStarts[offset]
	return found && sourceFile.aloneFrom(sourceFile.offset(literal.Pos()), end)
}

// aloneFrom reports whether a string literal starts at offset and nothing but closing punctuation (`,` `)`
// `}` `;`, the set the ruling names) follows it up to end. A raw literal running onto later lines fills the rest of its
// opening line.
func (sourceFile *sourceFile) aloneFrom(offset, end int) bool {
	literal, found := sourceFile.literalStarts[offset]
	if !found {
		return false
	}
	literalEnd := sourceFile.offset(literal.End())
	if sourceFile.lineOf(literalEnd) != sourceFile.lineOf(offset) {
		return true
	}
	for _, character := range sourceFile.source[literalEnd:max(end, literalEnd)] {
		if !strings.ContainsRune(",)}; \t", rune(character)) {
			return false
		}
	}
	return true
}

// offset turns a position of the parsed file into a byte offset.
func (sourceFile *sourceFile) offset(position token.Pos) int {
	return sourceFile.tokenFile.Offset(position)
}

// lineOfPosition is the line position sits on in the text, not one a //line directive gives it, since every line
// this package measures and edits is a line of the text.
func (sourceFile *sourceFile) lineOfPosition(position token.Pos) int {
	return sourceFile.tokenFile.PositionFor(position, false).Line
}

// lineOf returns the 1-based line holding a byte offset.
func (sourceFile *sourceFile) lineOf(offset int) int {
	return sort.Search(len(sourceFile.lineStarts), func(index int) bool {
		return sourceFile.lineStarts[index] > offset
	})
}

// lineBounds returns the byte range of a 1-based line, without its newline.
func (sourceFile *sourceFile) lineBounds(line int) (int, int) {
	start := sourceFile.lineStarts[line-1]
	end := len(sourceFile.source)
	if line < len(sourceFile.lineStarts) {
		end = sourceFile.lineStarts[line] - 1
	}
	// The token.File has no line after a final newline, so the last line's range would end on that newline.
	if end > start && sourceFile.source[end-1] == '\n' {
		end--
	}
	return start, end
}

func (sourceFile *sourceFile) lineText(line int) string {
	start, end := sourceFile.lineBounds(line)
	return string(sourceFile.source[start:end])
}

func (sourceFile *sourceFile) lineCount() int {
	return len(sourceFile.lineStarts)
}

// commentAt returns the comment covering a byte offset.
func (sourceFile *sourceFile) commentAt(offset int) (commentSpan, bool) {
	index := sort.Search(len(sourceFile.comments), func(index int) bool {
		return sourceFile.comments[index].end > offset
	})
	if index < len(sourceFile.comments) && sourceFile.comments[index].start <= offset {
		return sourceFile.comments[index], true
	}
	return commentSpan{}, false
}

// literalAt returns the string literal covering a byte offset.
func (sourceFile *sourceFile) literalAt(offset int) (*ast.BasicLit, bool) {
	index := sort.Search(len(sourceFile.literals), func(index int) bool {
		return sourceFile.offset(sourceFile.literals[index].End()) > offset
	})
	if index < len(sourceFile.literals) && sourceFile.offset(sourceFile.literals[index].Pos()) <= offset {
		return sourceFile.literals[index], true
	}
	return nil, false
}

// width measures a string in columns as the rule does, a tab as its TabColumns and every other rune as one.
func width(text string) int {
	columns := 0
	for _, character := range text {
		if character == '\t' {
			columns += consistency_no_long_line.TabColumns
		} else {
			columns++
		}
	}
	return columns
}

// crossing returns the byte index of the first rune that lands past the rule's Limit, or -1 when the line fits.
func crossing(text string) int {
	columns := 0
	for index, character := range text {
		if character == '\t' {
			columns += consistency_no_long_line.TabColumns
		} else {
			columns++
		}
		if columns > consistency_no_long_line.Limit {
			return index
		}
	}
	return -1
}

func indentation(text string) string {
	return text[:len(text)-len(strings.TrimLeft(text, " \t"))]
}

// IsDirective mirrors go/ast's test for a directive comment such as //go:generate or //line: a comment a tool
// reads by its exact text, which neither a wrap nor a rename may change.
func IsDirective(comment string) bool {
	if strings.HasPrefix(comment, "//line ") ||
		strings.HasPrefix(comment, "//extern ") ||
		strings.HasPrefix(comment, "//export ") {
		return true
	}
	if !strings.HasPrefix(comment, "//") {
		return false
	}
	text := comment[2:]
	colon := strings.Index(text, ":")
	if colon <= 0 || colon+1 >= len(text) {
		return false
	}
	for index := 0; index <= colon+1; index++ {
		if index == colon {
			continue
		}
		character := text[index]
		if !('a' <= character && character <= 'z' || '0' <= character && character <= '9') {
			return false
		}
	}
	return true
}

// commentOnlyLine returns the comment that starts a line after its indentation.
func (sourceFile *sourceFile) commentOnlyLine(line int) (commentSpan, bool) {
	start, _ := sourceFile.lineBounds(line)
	text := sourceFile.lineText(line)
	first := start + len(text) - len(strings.TrimLeft(text, " \t"))
	comment, found := sourceFile.commentAt(first)
	if !found || comment.start != first {
		return commentSpan{}, false
	}
	return comment, true
}

// opensInComment is whether the first byte of a line past its indentation is inside a comment: a comment alone on its
// line, or a line of a block comment that runs over several.
func (sourceFile *sourceFile) opensInComment(line int) bool {
	start, _ := sourceFile.lineBounds(line)
	text := sourceFile.lineText(line)
	_, inComment := sourceFile.commentAt(start + len(text) - len(strings.TrimLeft(text, " \t")))
	return inComment
}

// commaBetween reports whether a comma sits between two offsets. Between a list's last element and its closing
// bracket there is only whitespace, comments and an optional comma, so any comma byte outside a comment is it.
func (sourceFile *sourceFile) commaBetween(start, end int) bool {
	for offset := start; offset < end; offset++ {
		if comment, found := sourceFile.commentAt(offset); found {
			offset = comment.end - 1
			continue
		}
		if sourceFile.source[offset] == ',' {
			return true
		}
	}
	return false
}

// classify says whether a long line is a violation, and when it is not, why.
func (sourceFile *sourceFile) classify(line int) Category {
	if sourceFile.options.ReportedOnly && !sourceFile.reportedLines[line] {
		return CategoryNotReported
	}
	if sourceFile.isUncured(line) {
		return CategoryNotCured
	}
	if sourceFile.rawBodyLines[line] {
		return CategoryRawStringBody
	}
	if sourceFile.importLines[line] {
		return CategoryImport
	}
	if comment, isComment := sourceFile.commentOnlyLine(line); isComment && IsDirective(comment.text) {
		return CategoryDirective
	}
	if sourceFile.options.KeepComments && sourceFile.opensInComment(line) {
		return CategoryComment
	}
	if sourceFile.stringAloneLines[line] {
		return CategoryStringAlone
	}
	start, end := sourceFile.lineBounds(line)
	text := sourceFile.lineText(line)
	if sourceFile.keyedFrom(start+len(text)-len(strings.TrimLeft(text, " \t")), end) {
		return CategoryKeyedString
	}
	cross := crossing(text)
	if strings.ContainsAny(text[cross:], " \t") {
		return CategoryActionable
	}
	if _, inComment := sourceFile.commentAt(start + cross); inComment {
		word := text[strings.LastIndexAny(text[:cross], " \t")+1:]
		if strings.Contains(word, "://") || utf8.RuneCountInString(word) >= 40 {
			return CategoryUnbrokenToken
		}
	}
	return CategoryActionable
}

// longLines lists every 1-based line wider than the rule's Limit.
func (sourceFile *sourceFile) longLines() []int {
	var lines []int
	for line := 1; line <= sourceFile.lineCount(); line++ {
		if width(sourceFile.lineText(line)) > consistency_no_long_line.Limit {
			lines = append(lines, line)
		}
	}
	return lines
}

func (sourceFile *sourceFile) count() Count {
	count := Count{}
	for _, line := range sourceFile.longLines() {
		count[sourceFile.classify(line)]++
	}
	return count
}

// pass computes one round of non-overlapping edits, one per actionable long line.
func (sourceFile *sourceFile) pass() []edit {
	plans := sourceFile.structuralPlans()
	plansByLine := map[int][]int{}
	for index, candidate := range plans {
		for _, point := range candidate.breaks {
			lines := plansByLine[point.line]
			if len(lines) == 0 || lines[len(lines)-1] != index {
				plansByLine[point.line] = append(lines, index)
			}
		}
	}
	type claim struct{ start, end int }
	var claims []claim
	// A line left uncured is claimed from the start, so no construct around it is broken or laid out again.
	overlaps := func(start, end int) bool {
		if sourceFile.spansUncured(start, end) {
			return true
		}
		for _, existing := range claims {
			if start < existing.end && existing.start < end {
				return true
			}
		}
		return false
	}
	var edits []edit
	for _, line := range sourceFile.longLines() {
		if sourceFile.classify(line) != CategoryActionable {
			continue
		}
		if chosen, found := sourceFile.choosePlan(plans, plansByLine[line], line); found {
			if !overlaps(chosen.start, chosen.end) {
				claims = append(claims, claim{chosen.start, chosen.end})
				edits = append(edits, chosen.edits()...)
			}
			continue
		}
		if commentEdit, found := sourceFile.commentEdit(line); found {
			if !overlaps(commentEdit.start, commentEdit.end) {
				claims = append(claims, claim{commentEdit.start, commentEdit.end})
				edits = append(edits, commentEdit)
			}
		}
	}
	return edits
}

// choosePlan takes the outermost construct whose break makes the line fit. When none fits it takes the one
// leaving the narrowest widest line, outermost on a tie. A call or literal ending in a multi-line function
// literal is only taken when nothing else breaks the line.
func (sourceFile *sourceFile) choosePlan(plans []plan, indexes []int, line int) (plan, bool) {
	if len(indexes) == 0 {
		return plan{}, false
	}
	var ordered []int
	var lastResort []int
	for _, index := range indexes {
		if plans[index].lastFuncLiteral {
			lastResort = append(lastResort, index)
		} else {
			ordered = append(ordered, index)
		}
	}
	if len(ordered) == 0 {
		ordered = lastResort
	}
	best := -1
	bestWidth := 0
	for _, index := range ordered {
		estimated := sourceFile.estimate(plans[index], line)
		if estimated <= consistency_no_long_line.Limit {
			return plans[index], true
		}
		if best < 0 || estimated < bestWidth {
			best = index
			bestWidth = estimated
		}
	}
	return plans[best], true
}
