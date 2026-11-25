package build_tag

import (
	"go/build/constraint"
	"go/token"
	"strings"
	"unicode"

	"golang.org/x/tools/go/analysis"
)

// fileChecker checks the build constraint lines of one file, Go or not, and gathers what the file's constraints say,
// so that once the file is read the two kinds can be compared. x/tools' buildtag.checker.
type fileChecker struct {
	pass *analysis.Pass
	// plusBuildAllowed holds while a +build line read would still count.
	plusBuildAllowed bool
	// goBuildAllowed holds while a //go:build line read would still count.
	goBuildAllowed bool
	// crossCheck holds while nothing wrong has been found that would make comparing the two kinds meaningless: a
	// misplaced or extra constraint, or one that does not parse.
	crossCheck bool
	// inStarComment holds while the line read is inside a /* */ comment.
	inStarComment bool
	// goBuildPosition is the first //go:build line's, and token.NoPos until one is read.
	goBuildPosition token.Pos
	// plusBuildPosition is the first +build line's, and token.NoPos until one is read.
	plusBuildPosition token.Pos
	// goBuild is what the first //go:build line says, and nil until one parses.
	goBuild constraint.Expr
	// plusBuild is the conjunction of every +build line read while crossCheck held, and nil until one parses.
	plusBuild constraint.Expr
}

// newFileChecker starts checking a file at its top, where both kinds of constraint still count.
func newFileChecker(pass *analysis.Pass) *fileChecker {
	return &fileChecker{
		pass:             pass,
		plusBuildAllowed: true,
		goBuildAllowed:   true,
		crossCheck:       true,
	}
}

// nextLine splits text after its first newline, the line keeping its newline, or, when text holds none, into all of
// it and nothing.
func nextLine(text string) (line string, rest string) {
	end := len(text)
	if newline := strings.IndexByte(text, '\n'); newline >= 0 {
		end = newline + len("\n")
	}
	return text[:end], text[end:]
}

// otherFile checks the lines of a file that is not Go source, whose text begins at start in the file set, as far as
// its leading comments and blank lines go.
func (check *fileChecker) otherFile(start token.Pos, fullText string) {
	// A +build line counts only among the file's leading // comments and blank lines, and only with a blank line after
	// it, so where it stops counting is found first, in a pass of its own: at the last blank line of that run.
	plusBuildCutoff := 0
	for remaining := fullText; remaining != ""; {
		offset := len(fullText) - len(remaining)
		var line string
		line, remaining = nextLine(remaining)
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "//") {
			break
		}
		if trimmed == "" {
			plusBuildCutoff = offset
		}
	}

	for remaining := fullText; remaining != ""; {
		offset := len(fullText) - len(remaining)
		var line string
		line, remaining = nextLine(remaining)
		check.plusBuildAllowed = offset < plusBuildCutoff

		// A // line is read whole, its newline included, even inside a /* */ comment, where a //go:build line is
		// misplaced. Only a line beginning with the slashes is read: an indented one is text.
		if strings.HasPrefix(line, "//") {
			check.comment(start+token.Pos(offset), line)
			continue
		}

		// Any other line is cut down to what is not a /* */ comment, so whether it holds anything else is known. The
		// open comment carries over from line to line.
		code := line
		for {
			code = strings.TrimSpace(code)
			if check.inStarComment {
				_, afterClose, closed := strings.Cut(code, "*/")
				if !closed {
					code = ""
					break
				}
				code = afterClose
				check.inStarComment = false
				continue
			}
			afterOpen, opened := strings.CutPrefix(code, "/*")
			if !opened {
				break
			}
			check.inStarComment = true
			code = afterOpen
		}
		if code != "" {
			// A line that is neither blank nor comment ends the space for valid //go:build comments, and also the part
			// of the file that can be read reliably without parsing it: past it, a "comment" could be inside a
			// multiline string, or the file might not be a program at all. So the check stops.
			break
		}
	}
}

// comment checks one comment at position: a // comment's one line, its text beginning with the slashes, or a /* */
// comment, whose lines after the first are read as lines of their own where they begin with //, inside the comment.
func (check *fileChecker) comment(position token.Pos, text string) {
	if strings.HasPrefix(text, "//") {
		if strings.Contains(text, "+build") {
			check.plusBuildLine(position, text)
		}
		if strings.Contains(text, "//go:build") {
			check.goBuildLine(position, text)
		}
	}
	if !strings.HasPrefix(text, "/*") {
		return
	}
	firstLine, interior, multiline := strings.Cut(text, "\n")
	if !multiline {
		return
	}
	check.inStarComment = true
	linePosition := position + token.Pos(len(firstLine)+len("\n"))
	for remaining := interior; remaining != ""; {
		var line string
		line, remaining = nextLine(remaining)
		if strings.HasPrefix(line, "//") {
			check.comment(linePosition, line)
		}
		linePosition += token.Pos(len(line))
	}
	check.inStarComment = false
}

// goBuildLine checks a // line at position that holds //go:build somewhere in it.
func (check *fileChecker) goBuildLine(position token.Pos, line string) {
	if !constraint.IsGoBuild(line) {
		// A line whose own text, once the slashes and the spaces after them are taken off, is a //go:build line was
		// written with a space after its slashes. The line holds //go:build later on too, since only such lines are
		// read here, so the check is the original's: it does not report `// go:build` alone.
		afterSlashes := strings.TrimSpace(strings.TrimPrefix(line, "//"))
		if !strings.HasPrefix(line, "//go:build") && constraint.IsGoBuild("//"+afterSlashes) {
			check.pass.Reportf(position, "malformed //go:build line (space between // and go:build)")
		}
		return
	}
	if !check.goBuildAllowed || check.inStarComment {
		check.pass.Reportf(position, "misplaced //go:build comment")
		check.crossCheck = false
		return
	}

	if check.goBuildPosition == token.NoPos {
		check.goBuildPosition = position
	} else {
		check.pass.Reportf(position, "unexpected extra //go:build line")
		check.crossCheck = false
	}

	// x/tools' testing hack, kept as the original has it: the Go repository's tests write `// ERROR` after a
	// constraint, and the constraint ends there.
	expression, _, _ := strings.Cut(line, " // ERROR ")
	parsed, parseError := constraint.Parse(expression)
	if parseError != nil {
		check.pass.Reportf(position, "%v", parseError)
		check.crossCheck = false
		return
	}

	check.tags(position, parsed)

	if check.goBuild == nil {
		check.goBuild = parsed
	}
}

// plusBuildLine checks a // line at position that holds +build somewhere in it.
func (check *fileChecker) plusBuildLine(position token.Pos, text string) {
	line := strings.TrimSpace(text)
	if !constraint.IsPlusBuild(line) {
		// A comment with +build but not at its beginning is reported only early in the file. x/tools' own fixtures
		// write their expectations as `// want` comments that mention +build, and those are not reported.
		if check.plusBuildAllowed && !strings.HasPrefix(line, "// want") {
			check.pass.Reportf(position, "possible malformed +build comment")
		}
		return
	}
	// Inside a /* */ comment a +build line is never allowed, since every caller clears plusBuildAllowed before one is
	// read there: the Go file's loop on the comment's opening, and the other file's cutoff at the line opening it.
	if !check.plusBuildAllowed {
		check.pass.Reportf(position, "misplaced +build comment")
		check.crossCheck = false
	}

	if check.plusBuildPosition == token.NoPos {
		check.plusBuildPosition = position
	}

	// x/tools' testing hack, as in goBuildLine.
	expression, _, _ := strings.Cut(line, " // ERROR ")

	// IsPlusBuild above makes the first field `+build`: before it there are only the slashes and spaces, so the cut
	// at ` // ERROR `, which needs a space before its slashes, cannot fall ahead of it.
	fields := strings.Fields(strings.TrimPrefix(expression, "//"))
	for _, argument := range fields[1:] {
		for element := range strings.SplitSeq(argument, ",") {
			if strings.HasPrefix(element, "!!") {
				check.pass.Reportf(position, "invalid double negative in build constraint: %s", argument)
				check.crossCheck = false
				continue
			}
			if strings.ContainsFunc(strings.TrimPrefix(element, "!"), isNotTagCharacter) {
				check.pass.Reportf(position, "invalid non-alphanumeric build constraint: %s", argument)
				check.crossCheck = false
			}
		}
	}

	if !check.crossCheck {
		return
	}
	parsed, parseError := constraint.Parse(expression)
	if parseError != nil {
		// The original's comment says this cannot happen, since constraint.Parse never rejects a +build line, but it
		// does reject one of more than a hundred operators as too complex, and that is reported here, as the original
		// reports it.
		check.pass.Reportf(position, "%v", parseError)
		check.crossCheck = false
		return
	}
	check.tags(position, parsed)

	if check.plusBuild == nil {
		check.plusBuild = parsed
	} else {
		check.plusBuild = &constraint.AndExpr{X: check.plusBuild, Y: parsed}
	}
}

// isNotTagCharacter reports a character a +build term may not hold: anything but a letter, a digit, an underscore
// and a dot.
func isNotTagCharacter(character rune) bool {
	return !unicode.IsLetter(character) && !unicode.IsDigit(character) && character != '_' && character != '.'
}

// finish compares the file's two kinds of constraint, once it is read, when it has both and nothing wrong was found.
func (check *fileChecker) finish() {
	if !check.crossCheck || check.plusBuildPosition == token.NoPos || check.goBuildPosition == token.NoPos {
		return
	}

	// With crossCheck holding, the first //go:build line parsed, so goBuild is set, and every +build line did, so
	// plusBuild is too.
	lines, linesError := constraint.PlusBuildLines(check.goBuild)
	if linesError != nil {
		check.pass.Reportf(check.goBuildPosition, "%v", linesError)
		return
	}
	// PlusBuildLines returns a line for each conjunct of what it is handed, and an expression has at least one, so
	// expected is set once the loop ends.
	var expected constraint.Expr
	for _, line := range lines {
		parsed, parseError := constraint.Parse(line)
		if parseError != nil {
			// A line PlusBuildLines wrote that does not parse is the library's fault, not the file's, so it is not
			// reported, as the original does not report it.
			return
		}
		if expected == nil {
			expected = parsed
		} else {
			expected = &constraint.AndExpr{X: expected, Y: parsed}
		}
	}
	if expected.String() != check.plusBuild.String() {
		check.pass.Reportf(check.plusBuildPosition, "+build lines do not match //go:build condition")
	}
}

// tags reports each tag of expression, a constraint at position, that looks like a Go version and is not a valid one.
func (check *fileChecker) tags(position token.Pos, expression constraint.Expr) {
	// Eval serves as the walk: it visits every tag, left to right, evaluating both sides of each operator rather than
	// stopping early. What it returns is immaterial.
	_ = expression.Eval(func(tag string) bool {
		if malformedGoTag(tag) {
			check.pass.Reportf(position, "invalid go version %q in build constraint", tag)
		}
		return false
	})
}

// malformedGoTag reports whether tag is likely a Go version constraint written wrong: one beginning go1 that is not a
// valid version, or a close misspelling of the go1. prefix before what would be one.
func malformedGoTag(tag string) bool {
	if strings.HasPrefix(tag, "go1") {
		return !validGoVersion(tag)
	}
	for _, misspelling := range []string{"go.", "g1.", "go"} {
		version, misspelled := strings.CutPrefix(tag, misspelling)
		if misspelled && validGoVersion("go1."+version) {
			return true
		}
	}
	return false
}

// validGoVersion reports whether tag is a valid Go version as a build constraint reads one.
func validGoVersion(tag string) bool {
	return constraint.GoVersion(&constraint.TagExpr{Tag: tag}) != ""
}
