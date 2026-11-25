package flagged

// A //line directive renames the lines after it, and a word inside a block comment is still found on the file's own
// line: had the rule counted from the line the directive claims, line 900 of a file this short, it would not exist.

//line renamed.go:900
// want +3 `"TODO"`
/*
 * The directive above renumbers this comment.
 * TODO find the word on the right line
 */
func renamed() {}
