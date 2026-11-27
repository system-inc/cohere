package micromark

import (
	"fmt"
	"slices"
	"testing"
)

// TestMemoryReuseMatchesFreshParses parses every fixture through one Memory, reset between parses, and
// compares each with a parse that allocates its own (#vbjv3d6). A reused attempt or tokenizer that kept
// anything of an earlier document shows as a difference, and the second round reuses slots that every
// kind of document has already been through.
func TestMemoryReuseMatchesFreshParses(test *testing.T) {
	test.Parallel()
	inputs := slices.Concat(eventFixtures, attentionFixtures, blockQuoteFixtures, autolinkFixtures,
		codeFencedFixtures, codeTextFixtures, characterReferenceFixtures, codeIndentedFixtures, gfmTableFixtures,
		gfmFootnoteFixtures, definitionFixtures, definitionFailureFixtures, definitionReferenceFixtures,
		gfmAutolinkLiteralFixtures, htmlTextOverrideFixtures, listFixtures, headingAtxFixtures,
		gfmTaskListItemFixtures, gfmStrikethroughFixtures, labelEndFixtures, wikiLinkFixtures,
		wikiLinkRefusalFixtures, liquidFixtures, htmlTextFixtures, htmlFlowFixtures, mathFixtures,
		setextUnderlineFixtures)
	memory := NewMemory()
	for round := 1; round <= 2; round++ {
		for _, input := range inputs {
			expected := portEvents(input)
			actual := portEventsFrom(input, memory)
			memory.Reset()
			if difference := firstEventDifference(expected, actual); difference != "" {
				test.Fatalf("round %d, input %q: %s", round, truncateInput(input), difference)
			}
		}
	}
}

// TestMemoryResetReleasesTokens proves the comparison above can fail: events read after their Memory was
// reset no longer describe the parse.
func TestMemoryResetReleasesTokens(test *testing.T) {
	test.Parallel()
	memory := NewMemory()
	events := Parse(SourceUnits("# a *b*\n\n- c\n"), MarkdownConstructs(), memory)
	before := events[0].Token.Type
	memory.Reset()
	if after := events[0].Token.Type; after == before {
		test.Fatalf("the first token still reads %q after its memory was reset", after)
	}
}

// portEvents describes the Go port's events, one line each: the direction, the token type and both
// positions.
func portEvents(input string) []string {
	return portEventsFrom(input, nil)
}

// portEventsFrom is portEvents with the parse taking its tokens and tokenizers from memory.
func portEventsFrom(input string, memory *Memory) (described []string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			described = []string{fmt.Sprintf("panic: %v", recovered)}
		}
	}()
	events := Parse(SourceUnits(input), MarkdownConstructs(), memory)
	for _, event := range events {
		kind := "exit"
		if event.Enter {
			kind = "enter"
		}
		start, end := event.Token.Start, event.Token.End
		described = append(described, fmt.Sprintf("%s %s %d:%d:%d-%d:%d:%d", kind, event.Token.Type,
			start.Line, start.Column, start.Offset, end.Line, end.Column, end.Offset))
	}
	return described
}

// firstEventDifference names the first event two descriptions differ at, or "".
func firstEventDifference(expected []string, actual []string) string {
	for index := 0; index < len(expected) || index < len(actual); index++ {
		var want string
		var got string
		if index < len(expected) {
			want = expected[index]
		}
		if index < len(actual) {
			got = actual[index]
		}
		if want != got {
			return fmt.Sprintf("event %d: want %q, got %q", index, want, got)
		}
	}
	return ""
}

// truncateInput is an input short enough to read in a failure message.
func truncateInput(input string) string {
	if len(input) > 80 {
		return input[:80] + "…"
	}
	return input
}
