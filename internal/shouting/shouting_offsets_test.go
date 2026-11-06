package shouting

import (
	"slices"
	"testing"
)

// Every shouted word is returned where it sits, as a byte offset, so a caller reporting at the word lands on it: a
// repeat is its own entry, an acronym is none, and multi-byte text before the word, in prose or inside masked code,
// moves the offset by its bytes.
func TestShoutedTokensInMaskedGivesEachWordItsByteOffset(test *testing.T) {
	test.Parallel()
	text := "see `café` then NEVER cache the API, crème NEVER"
	got := ShoutedTokensInMasked(MaskCodeAndCommands(text))
	want := []ShoutedToken{{Text: "NEVER", Offset: 17}, {Text: "NEVER", Offset: 45}}
	if !slices.Equal(got, want) {
		test.Fatalf("ShoutedTokensInMasked(%q) = %v, want %v", text, got, want)
	}
	for _, shouted := range got {
		if covered := text[shouted.Offset : shouted.Offset+len(shouted.Text)]; covered != shouted.Text {
			test.Errorf("the offset %d covers %q in the text, not %q", shouted.Offset, covered, shouted.Text)
		}
	}
	if tokens := TokensInMasked(MaskCodeAndCommands(text)); !slices.Equal(tokens, []string{"NEVER"}) {
		test.Errorf("TokensInMasked still names each word once, and gave %v", tokens)
	}
}
