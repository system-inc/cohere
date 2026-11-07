// Package line_wrap rewraps Go lines wider than consistency_no_long_line's limit without changing what the program
// means: it breaks a call, a literal, a signature or an operator chain one element per line, reflows a prose comment,
// lets gofmt lay out each pass, and proves the result the same syntax tree with the same comment words (Equivalent).
// What no break can shorten is left long and named, as residue. The fix phase wraps the rule's findings with it, and
// tools/go_codemods/command/line_wrap runs it over a tree.
package line_wrap

import (
	"fmt"
	"go/format"
	"slices"
	"sort"
	"strings"
)

// maximumPasses bounds the edit, format, reparse loop. Each pass breaks one construct per long line, so nested
// constructs take a pass per level.
const maximumPasses = 40

// Residue is a long line left after wrapping, with the shape that kept it long.
type Residue struct {
	Line  int
	Shape string
	Text  string
}

// Count tallies long lines by category.
type Count map[Category]int

func apply(source []byte, edits []edit) []byte {
	sort.SliceStable(edits, func(left, right int) bool {
		return edits[left].start > edits[right].start
	})
	result := append([]byte(nil), source...)
	previousStart := len(source) + 1
	for _, change := range edits {
		if change.end > previousStart {
			continue
		}
		result = append(result[:change.start], append([]byte(change.text), result[change.end:]...)...)
		previousStart = change.start
	}
	return result
}

// CountLongLines is how many lines of one Go source file are wider than the rule's limit, whatever their category.
func CountLongLines(source []byte) (int, error) {
	parsed, parseSourceError := parseSource(source, Options{})
	if parseSourceError != nil {
		return 0, parseSourceError
	}
	return len(parsed.longLines()), nil
}

// Result is what wrapping one file produced.
type Result struct {
	Output   []byte
	Before   Count
	After    Count
	Residue  []Residue
	Passes   int
	Rejected error
}

// Wrap rewraps every actionable long line of one Go source file, then proves the output equivalent. With
// Options.CuredOnly it tries again without each line a try touched and left long, until no try leaves one, and leaves
// the file as it was when a line it already left alone is touched again.
func Wrap(source []byte, options Options) (Result, error) {
	options.uncured = nil
	for {
		result, wrapError := wrapOnce(source, options)
		if wrapError != nil || !options.CuredOnly || result.Rejected != nil || string(result.Output) == string(source) {
			return result, wrapError
		}
		original, parseSourceError := parseSource(source, options)
		if parseSourceError != nil {
			return Result{}, parseSourceError
		}
		uncured, uncuredError := original.uncuredSpans(result.Output)
		if uncuredError != nil {
			result.Output = source
			result.Rejected = uncuredError
			return result, nil
		}
		if len(uncured) == 0 {
			return result, nil
		}
		// Each try leaves more lines alone, so the tries end. A line touched though it was left alone means a
		// construct reached it this package cannot see, so nothing is kept.
		for _, span := range uncured {
			if slices.Contains(options.uncured, span) {
				return Result{Output: source, Before: result.Before, After: result.Before}, nil
			}
		}
		options.uncured = append(options.uncured, uncured...)
	}
}

// wrapOnce is one try of Wrap: every actionable long line rewrapped, and the output proved equivalent.
func wrapOnce(source []byte, options Options) (Result, error) {
	original, parseSourceError := parseSource(source, options)
	if parseSourceError != nil {
		return Result{}, parseSourceError
	}
	result := Result{Output: source, Before: original.count()}
	current := original
	for result.Passes < maximumPasses {
		edits := current.pass()
		if len(edits) == 0 {
			break
		}
		formatted, formatError := format.Source(apply(current.source, edits))
		if formatError != nil {
			return Result{}, fmt.Errorf("pass %d: format: %w", result.Passes+1, formatError)
		}
		if string(formatted) == string(current.source) {
			break
		}
		result.Passes++
		next, parseSourceError := parseSource(formatted, options)
		if parseSourceError != nil {
			return Result{}, fmt.Errorf("pass %d: reparse: %w", result.Passes, parseSourceError)
		}
		current = next
	}
	result.After = current.count()
	for _, line := range current.longLines() {
		if current.classify(line) == CategoryActionable {
			result.Residue = append(
				result.Residue,
				Residue{Line: line, Shape: current.residueShape(line), Text: strings.TrimSpace(current.lineText(line))},
			)
		}
	}
	if string(current.source) == string(source) {
		return result, nil
	}
	if equivalentError := Equivalent(source, current.source); equivalentError != nil {
		result.Rejected = equivalentError
		return result, nil
	}
	result.Output = current.source
	return result, nil
}
