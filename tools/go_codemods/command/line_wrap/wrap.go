package main

import (
	"fmt"
	"go/format"
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

// Result is what wrapping one file produced.
type Result struct {
	Output   []byte
	Before   Count
	After    Count
	Residue  []Residue
	Passes   int
	Rejected error
}

// Wrap rewraps every actionable long line of one Go source file, then proves the output equivalent.
func Wrap(source []byte, options Options) (Result, error) {
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
