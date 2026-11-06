package textrules

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Span is one expected finding, by what it covers rather than by coordinates, so one expectation is exact
// in every language: Go's form of a case has a `package` line the others do not. Exactly one of the three
// forms is written.
type Span struct {
	// Covers is the text the finding covers, and Occurrence which occurrence of it in the source, from 1.
	Covers     *string
	Occurrence int

	// OccurrenceIn is the occurrence in each language, written as an object in place of Occurrence's number,
	// where one number can't name the same text in every language: a one-letter name's declaration is the
	// fifth `n` in TypeScript's form and the third in Go's, because `function` holds two. An occurrence of 0
	// says the language's form has no such text, because its grammar can't express the construct (the text
	// rules' second principle): a TypeScript property's name is its wire key, so us03's TypeScript form has no
	// field whose name could differ from its wire spelling, and the finding over it is Go's and Swift's.
	OccurrenceIn map[Engine]int

	// FileStart is a point at the start of the file, for a rule about the file's path.
	FileStart bool

	// LinesFrom runs from the start of this 1-based line to the end of the file, for a rule about the file's
	// length. A line is the language's own: each engine's terminators are a fact of its grammar.
	LinesFrom int
}

// UnmarshalJSON reads a span, whose occurrence is a number or an object naming one per language, and refuses
// an unknown key.
func (span *Span) UnmarshalJSON(raw []byte) error {
	var written struct {
		Covers     *string         `json:"covers"`
		Occurrence json.RawMessage `json:"occurrence"`
		FileStart  bool            `json:"fileStart"`
		LinesFrom  int             `json:"linesFrom"`
	}
	if decodeStrictlyError := decodeStrictly(raw, &written); decodeStrictlyError != nil {
		return decodeStrictlyError
	}
	*span = Span{Covers: written.Covers, FileStart: written.FileStart, LinesFrom: written.LinesFrom}
	if len(written.Occurrence) == 0 {
		return nil
	}
	if decodeError := json.Unmarshal(written.Occurrence, &span.Occurrence); decodeError == nil {
		return nil
	}
	if decodeStrictlyError := decodeStrictly(written.Occurrence, &span.OccurrenceIn); decodeStrictlyError != nil {
		return fmt.Errorf("occurrence is a number, or an object naming one per language: %w", decodeStrictlyError)
	}
	return nil
}

// occurrenceFor is which occurrence of Covers the span names in an engine's language, or 0 where it names none.
func (span Span) occurrenceFor(engine Engine) int {
	if span.OccurrenceIn != nil {
		return span.OccurrenceIn[engine]
	}
	return span.Occurrence
}

// AppliesTo reports whether the span is expected in an engine's form at all: everywhere, unless its
// occurrence there is written as 0.
func (span Span) AppliesTo(engine Engine) bool {
	if span.OccurrenceIn == nil {
		return true
	}
	occurrence, written := span.OccurrenceIn[engine]
	return !written || occurrence != 0
}

// String names the span as a case writes it.
func (span Span) String() string {
	switch {
	case span.Covers != nil && span.OccurrenceIn != nil:
		return fmt.Sprintf("%q (occurrences %v)", *span.Covers, span.OccurrenceIn)
	case span.Covers != nil:
		return fmt.Sprintf("%q (occurrence %d)", *span.Covers, span.Occurrence)
	case span.FileStart:
		return "the file's start"
	default:
		return fmt.Sprintf("lines from %d", span.LinesFrom)
	}
}

// check refuses a span that is not exactly one of the three forms.
func (span Span) check() error {
	forms := 0
	if span.Covers != nil {
		forms++
		if *span.Covers == "" {
			return errors.New("covers is empty")
		}
		switch {
		case span.OccurrenceIn != nil && span.Occurrence != 0:
			return fmt.Errorf("covers %q gives its occurrence as a number and per language", *span.Covers)
		case span.OccurrenceIn != nil:
			for engine, occurrence := range span.OccurrenceIn {
				if _, known := extensionOf[engine]; !known || occurrence < 0 {
					return fmt.Errorf("covers %q gives %s occurrence %d, and each is a language's, from 1, or 0 "+
						"where its form has no such text", *span.Covers, engine, occurrence)
				}
			}
		case span.Occurrence < 1:
			return fmt.Errorf("covers %q needs an occurrence of 1 or more", *span.Covers)
		}
	} else if span.Occurrence != 0 || span.OccurrenceIn != nil {
		return errors.New("occurrence is written without covers")
	}
	if span.FileStart {
		forms++
	}
	if span.LinesFrom != 0 {
		forms++
		if span.LinesFrom < 1 {
			return fmt.Errorf("linesFrom is %d, and lines count from 1", span.LinesFrom)
		}
	}
	if forms != 1 {
		return fmt.Errorf("a span is exactly one of covers, fileStart and linesFrom, and this one writes %d", forms)
	}
	return nil
}
