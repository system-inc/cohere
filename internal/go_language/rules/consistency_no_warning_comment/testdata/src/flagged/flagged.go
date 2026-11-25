// Package flagged holds comments the rule must report. Each case is its own comment group, a blank line apart. Each
// expectation rides on the line its finding is reported at, or names that line with +N from a line comment above
// it, and its pattern is in backticks, which the rule does not read, so it adds no word of its own.
package flagged

// TODO retry with a backoff // want `^"TODO" in a comment marks work left for later`

/* FIXME the cache is never cleared */ // want `^"FIXME" in a comment`

// XXX this only works on one machine // want `^"XXX" in a comment`

// A directive's name is lowercase, so this is a task and no directive.

//TODO: no space after the slashes // want `"TODO"`

// A go/doc note is still a task left for later.

// TODO(someone): split this function // want `"TODO"`

// The word at the end of a sentence counts: we handle the error later, TODO. // want `"TODO"`

// Punctuation and spaces are boundaries: (TODO), FIXME! and "XXX". // want `"TODO"` `"FIXME"` `"XXX"`

// So is a hyphen or a slash: TODO-later and FIXME/maybe. // want `"TODO"` `"FIXME"`

// A word after a multi-byte rune is reported at its own byte: é TODO. // want `"TODO"`

// One finding per word, so two of the same word are two findings: TODO, then TODO. // want `"TODO"` `"TODO"`

// A backtick pair masks only what it holds: `TODO` is a mention, and TODO is not. // want `"TODO"`

// An unclosed backtick masks nothing. Its want is double quoted and spelled as no word: ` FIXME // want "FIXM[E]"

var limit = 3 // TODO read this from the settings // want `"TODO"`

// retry tries again.
//
// FIXME: it never gives up. // want `"FIXME"`
func retry() {
	// XXX the read below races the settings // want `"XXX"`
	_ = limit
}

// want +3 `"TODO"`
/*
 * A block comment reports at the word, on the line the word is on.
 * TODO split this block
 */

// want +2 `"FIXME"`
/*
FIXME at the start of a line inside a block comment
*/

// A directive in the group is set aside, and the comments around it are still read. The group is no doc comment,
// where gofmt would move the directive to the end.
//go:generate echo generated
// XXX the generator above is slow // want `"XXX"`

func generated() {}

// sweep clears the cache, and TODO: make it incremental. // want `"TODO"`
//
//	sweep() // TODO in a code block is an example and is not read
//
// FIXME the prose after the block is read again. // want `"FIXME"`
//
//	fmt.Println("`")
//
// The lone backtick in the block above pairs with none here, so TODO is read before `this`. // want "TOD[O]"
func sweep() {
	// An indented line straight after prose continues the paragraph, as CommonMark reads it, so it is read:
	//	TODO in a continuation line // want `"TODO"`

	// A block ends at the first line that is not indented, blank line or not:
	//
	//	sweep() // XXX in the block
	// XXX in the prose straight after it // want `"XXX"`
}
