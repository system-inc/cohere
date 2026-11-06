// Package lookalike holds shapes that look like a task left in a comment and are not: the words in another case, or
// inside a longer word, or outside a comment altogether.
package lookalike

// A sentence about TODOS, or the TODOs of a file, names the notes and defers nothing.

// Lowercase is prose: there is a todo list, a fixme branch, and xxx in a name. Todo and Fixme open sentences.

// Inside a longer word the letters are another word: TODO_LIST, myTODO, TODO2, XXXX, XXXL and 0xXXX.

// A letter of any script continues a word: ÜTODO, TODOé and ТODO spelled with a Cyrillic Te.

// The word "todo" in quotes is still lowercase.

const TODO_LIST = "TODO"

var commentInAString = "// TODO cache this"

var blockCommentInARawString = `/* FIXME the cache */`

type tagged struct {
	Name string `json:"XXX"`
}

var tag = tagged{Name: commentInAString + blockCommentInARawString + TODO_LIST}
