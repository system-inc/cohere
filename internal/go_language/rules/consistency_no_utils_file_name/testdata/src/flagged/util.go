// want `^File name "util\.go" holds "util", which names nothing in particular, so the file collects whatever has no other home\. Name the file for what it holds, or at the least spell the word "utilities", as the house spells it in a folder's name\.$`

// The whole name is the word.
package flagged

// Parse is something the file holds.
func Parse(text string) string {
	return text
}
