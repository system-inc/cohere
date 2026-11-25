// Package suppression reads the //cohere:ignore and //cohere:file-ignore comments in Go source, and applies
// them to a run's findings.
//
// A suppression is a rule scoped off for one line, so it is honest only when it says why, and it stays honest
// only while it still withholds something: a stale exemption is a rule silently turned off. The TypeScript side
// (internal/lint/suppression) records a missing reason and counts it. This side is stricter: a directive
// without a reason suppresses nothing and is itself a finding, and so is one that withholds nothing.
//
// The index comes from go/scanner rather than a parse. A directive is a fact about a line, and the scanner
// already says whether code precedes a comment on its line, which is all the placement rule needs.
package suppression

import (
	"bytes"
	"go/scanner"
	"go/token"
	"slices"
	"strings"
	"unicode"
)

const (
	ignoreKeyword     = "cohere:ignore"
	fileIgnoreKeyword = "cohere:file-ignore"
)

// Index is every directive in one file. Build writes it and nothing writes it after, so a run can build many
// at once and Apply can read them from any goroutine.
type Index struct {
	// File is the path the run's findings name this file by.
	File string

	directives []directive
}

// directive is one comment that is, or reads as an attempt at, a suppression.
type directive struct {
	// line and column are where the comment starts, 1-based, the column in bytes.
	line   int
	column int

	// wholeFile is a file-ignore, which covers every line of its file wherever it sits.
	wholeFile bool

	// coveredLine is the one line an ignore covers: the next line for a comment on its own line, its own line
	// for a comment after code.
	coveredLine int

	// rules are the rule names as written, resolved against the run's known rules only when applied.
	rules []string

	// hasReason is whether the text after the rule names holds a word, and reason is that text, trimmed.
	hasReason bool
	reason    string

	// malformed is a comment that cannot be a directive though it reads as one: it names no rule, or a space
	// after the slashes makes it prose.
	malformed bool
}

// directiveMark is in every directive, well formed or not: both keywords open with it, and a comment's text is
// bytes of the source, so a file without it holds no directive and is not scanned.
var directiveMark = []byte("cohere:")

// Build scans one file's source for directives. Source that does not scan cleanly still yields the comments
// the scanner reaches, since the types phase is what reports why a file does not compile.
func Build(file string, source []byte) *Index {
	if !bytes.Contains(source, directiveMark) {
		return &Index{File: file}
	}
	fileSet := token.NewFileSet()
	tokenFile := fileSet.AddFile(file, -1, len(source))
	var sourceScanner scanner.Scanner
	sourceScanner.Init(tokenFile, source, nil, scanner.ScanComments)

	index := &Index{File: file}
	lastCodeLine := 0
	for {
		position, scanned, literal := sourceScanner.Scan()
		if scanned == token.EOF {
			break
		}
		// Unadjusted, because a //line directive renames lines for the compiler, while findings and the
		// reader both count the lines of the file as written.
		line := tokenFile.PositionFor(position, false).Line
		if scanned != token.COMMENT {
			// The scanner inserts a semicolon at a line's end, spelled "\n", which is not code anyone wrote.
			if scanned == token.SEMICOLON && literal == "\n" {
				continue
			}
			// A raw string can span lines, and the code it belongs to ends on the last of them.
			lastCodeLine = line + strings.Count(literal, "\n")
			continue
		}
		parsed, isDirective := parseDirective(literal)
		if !isDirective {
			continue
		}
		parsed.line = line
		parsed.column = tokenFile.PositionFor(position, false).Column
		parsed.coveredLine = line + 1
		if lastCodeLine == line {
			parsed.coveredLine = line
		}
		index.directives = append(index.directives, parsed)
	}
	return index
}

// parseDirective reads one comment's text, slashes included, and reports whether it is a directive at all.
// A block comment never is: go's own directives are `//` comments only, and a suppression reads like one.
func parseDirective(text string) (directive, bool) {
	body, isLineComment := strings.CutPrefix(text, "//")
	if !isLineComment {
		return directive{}, false
	}
	if spaced := strings.TrimLeft(body, " \t"); spaced != body {
		// With a space after the slashes go reads the comment as prose, so the author's intent suppresses
		// nothing. Saying so beats letting it look like it works.
		if _, _, matched := cutKeyword(spaced); matched {
			return directive{malformed: true}, true
		}
		return directive{}, false
	}
	rest, wholeFile, matched := cutKeyword(body)
	if !matched {
		return directive{}, false
	}

	parsed := directive{wholeFile: wholeFile}
	fields := strings.TrimSpace(rest)
	if fields == "" {
		parsed.malformed = true
		return parsed, true
	}
	ruleList, reason := fields, ""
	if end := strings.IndexAny(fields, " \t"); end >= 0 {
		ruleList, reason = fields[:end], fields[end:]
	}
	parsed.rules = strings.Split(ruleList, ",")
	// An empty name is a comma with a space after it, so the author's list ran on into what is now read as the
	// reason. Guessing which words were rules would suppress something nobody named.
	if slices.Contains(parsed.rules, "") {
		return directive{malformed: true}, true
	}
	parsed.hasReason = strings.ContainsFunc(reason, isWordCharacter)
	parsed.reason = strings.TrimSpace(reason)
	return parsed, true
}

// Keepings are the directives in the file that name ruleName, in full or by its name past the namespace, and give a
// reason, so withhold for it: for a report that applies one name's directives itself rather than through Apply, as
// the duplicates report keeps a function in its shape (#jfvhbj7). A directive without a reason, or malformed, keeps
// nothing, as it withholds nothing in Apply, and the lint phase says what is wrong with it.
func (index *Index) Keepings(ruleName string) []Keeping {
	var keepings []Keeping
	for _, indexed := range index.directives {
		if indexed.malformed || !indexed.hasReason {
			continue
		}
		if !slices.ContainsFunc(indexed.rules, func(written string) bool {
			return written == ruleName || ruleNamespace+written == ruleName
		}) {
			continue
		}
		keepings = append(keepings, Keeping{
			File:        index.File,
			Line:        indexed.line,
			WholeFile:   indexed.wholeFile,
			CoveredLine: indexed.coveredLine,
			Reason:      indexed.reason,
		})
	}
	return keepings
}

// cutKeyword strips a directive keyword from the start of a comment's body. The keyword must end the body or be
// followed by whitespace, so `cohere:ignored` is not a misspelled directive but some other comment.
func cutKeyword(body string) (rest string, wholeFile bool, matched bool) {
	for _, keyword := range []string{ignoreKeyword, fileIgnoreKeyword} {
		after, found := strings.CutPrefix(body, keyword)
		if !found {
			continue
		}
		if after == "" || after[0] == ' ' || after[0] == '\t' {
			return after, keyword == fileIgnoreKeyword, true
		}
	}
	return "", false, false
}

func isWordCharacter(character rune) bool {
	return character == '_' || unicode.IsLetter(character) || unicode.IsDigit(character)
}
