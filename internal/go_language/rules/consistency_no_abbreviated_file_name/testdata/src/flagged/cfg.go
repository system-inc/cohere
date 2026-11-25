// want `^File name "cfg\.go" abbreviates "cfg"\. Spell it out as "configuration"\. A file's name is read on every path and every import, so the letters saved when it was named are paid back by every reader who has to expand the word themselves and hope they expanded it the way the author meant\.$`

// The whole name is the word.
package flagged

// Load is something the file holds.
func Load(text string) string {
	return text
}
