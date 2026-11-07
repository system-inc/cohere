package global_rename

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/system-inc/cohere/tools/go_codemods/rename"
)

// lineLimit and tabColumns are the house's line length, as cohere-go/consistency-no-long-line measures it: a tab
// counts as four columns and every other rune as one.
const (
	lineLimit  = 120
	tabColumns = 4
)

// lineWidth is a line's width in columns.
func lineWidth(line string) int {
	width := 0
	for _, character := range line {
		if character == '\t' {
			width += tabColumns
			continue
		}
		width++
	}
	return width
}

// refuseLongLines refuses every rename that would leave a line over the limit that was not over it before, and
// reports whether it refused any. Each file the accepted renames touch is rewritten in memory, formatted, and
// read line by line. A long line is old when, with every new name spelled back as its old one and runs of space
// read as one, the file already held it long; anything else is new, and is blamed on the renames whose new names
// it spells, or, where gofmt only realigned it, on those its block of lines spells. The rule exempts some long
// lines (a lone string, a URL in a comment), which this does not try to model: refusing one of those costs a
// rename, never a finding.
func (engine *engine) refuseLongLines() bool {
	byFile := rename.GroupByFile(engine.pendingEdits())
	backToOld := map[string]string{}
	keysByNewName := map[string][]Key{}
	for key, newName := range engine.accepted {
		backToOld[newName] = engine.candidates[key].OldName
		keysByNewName[newName] = append(keysByNewName[newName], key)
	}
	filenames := make([]string, 0, len(byFile))
	for filename := range byFile {
		filenames = append(filenames, filename)
	}
	sort.Strings(filenames)
	refused := false
	for _, filename := range filenames {
		original, rewritten, rewriteError := rename.RewriteFile(filename, byFile[filename])
		if rewriteError != nil {
			// The apply reports a file that cannot be rewritten; nothing here can be measured.
			continue
		}
		oldLong := map[string]int{}
		for _, line := range strings.Split(string(original), "\n") {
			if lineWidth(line) > lineLimit {
				oldLong[normalizedLine(line, nil)]++
			}
		}
		editedHere := map[Key]bool{}
		for _, edit := range byFile[filename] {
			for _, key := range keysByNewName[edit.NewText] {
				editedHere[key] = true
			}
		}
		lines := strings.Split(string(rewritten), "\n")
		for index, line := range lines {
			if lineWidth(line) <= lineLimit {
				continue
			}
			normalized := normalizedLine(line, backToOld)
			if oldLong[normalized] > 0 {
				oldLong[normalized]--
				continue
			}
			blamed := blamedKeys(lines, index, keysByNewName, editedHere)
			if len(blamed) == 0 {
				// Nothing on the line or its block spells a new name, yet the rewrite made it long: every rename
				// edited in the file answers for it.
				for key := range editedHere {
					blamed = append(blamed, key)
				}
			}
			for _, key := range blamed {
				if _, open := engine.accepted[key]; !open {
					continue
				}
				engine.reject(key, "line length", fmt.Sprintf(
					"would make %s:%d %d columns, over %d",
					engine.relative(filename),
					index+1,
					lineWidth(line),
					lineLimit,
				))
				refused = true
			}
		}
	}
	return refused
}

// blamedKeys are the renames edited in this file whose new names line index spells, or, when it spells none, any
// line of its block does: gofmt realigns a block of fields or values to its longest name.
func blamedKeys(lines []string, index int, keysByNewName map[string][]Key, editedHere map[Key]bool) []Key {
	spelled := func(line string) []Key {
		var keys []Key
		for _, word := range identifierWords(line) {
			for _, key := range keysByNewName[word] {
				if editedHere[key] {
					keys = append(keys, key)
				}
			}
		}
		return keys
	}
	if keys := spelled(lines[index]); len(keys) > 0 {
		return keys
	}
	var keys []Key
	for start := index; start >= 0 && strings.TrimSpace(lines[start]) != ""; start-- {
		keys = append(keys, spelled(lines[start])...)
	}
	for end := index + 1; end < len(lines) && strings.TrimSpace(lines[end]) != ""; end++ {
		keys = append(keys, spelled(lines[end])...)
	}
	return keys
}

// normalizedLine is a line with every word back is asked about respelled, and every run of spaces and tabs read
// as one space.
func normalizedLine(line string, back map[string]string) string {
	var builder strings.Builder
	word := strings.Builder{}
	flush := func() {
		if word.Len() == 0 {
			return
		}
		text := word.String()
		if old, renamed := back[text]; renamed {
			text = old
		}
		builder.WriteString(text)
		word.Reset()
	}
	space := false
	for _, character := range strings.TrimSpace(line) {
		if isIdentifierRune(character) {
			if space {
				builder.WriteByte(' ')
				space = false
			}
			word.WriteRune(character)
			continue
		}
		flush()
		if character == ' ' || character == '\t' {
			space = true
			continue
		}
		if space {
			builder.WriteByte(' ')
			space = false
		}
		builder.WriteRune(character)
	}
	flush()
	return builder.String()
}

// identifierWords are the identifier-shaped words of a line.
func identifierWords(line string) []string {
	return strings.FieldsFunc(line, func(character rune) bool { return !isIdentifierRune(character) })
}

func isIdentifierRune(character rune) bool {
	return character == '_' || unicode.IsLetter(character) || unicode.IsDigit(character)
}

// pendingEdits are the edits the accepted renames would make now: identifiers, comment respellings, tags and
// platform twins.
func (engine *engine) pendingEdits() []rename.Edit {
	var edits []rename.Edit
	for key, newName := range engine.accepted {
		oldName := engine.candidates[key].OldName
		for _, site := range engine.sites[key] {
			edits = append(
				edits,
				rename.Edit{Filename: site.filename, Offset: site.offset, OldText: oldName, NewText: newName},
			)
		}
		edits = append(edits, engine.tagEdits[key]...)
		edits = append(edits, engine.twinEdits[key]...)
	}
	edits = append(edits, engine.commentEdits()...)
	kept, _ := rename.Deduplicate(edits)
	return kept
}
