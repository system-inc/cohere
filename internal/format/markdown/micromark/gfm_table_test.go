package micromark

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gfmTableFixtures hold no definition, html text, strikethrough, autolink literal, footnote, task list
// item, math, liquid or wiki-link syntax: those constructs are still pending, so a fixture leaning on them
// would differ for reasons that are not the table's. That rules out `[`, `<`, `~`, `$`, `{`, `@` and
// `www.`/`http` in cells.
var gfmTableFixtures = []string{
	// GFM spec examples (section 4.10, Tables).
	"| foo | bar |\n| --- | --- |\n| baz | bim |\n",
	"| abc | defghi |\n:-: | -----------:\nbar | baz\n",
	"| f\\|oo  |\n| ------ |\n| b `\\|` az |\n| b **\\|** im |\n",
	"| abc | def |\n| --- | --- |\n| bar | baz |\n> bar\n",
	"| abc | def |\n| --- | --- |\n| bar | baz |\nbar\n\nbar\n",
	"| abc | def |\n| --- |\n| bar |\n",
	"| abc | def |\n| --- | --- |\n| bar |\n| bar | baz | boo |\n",
	"| abc | def |\n| --- | --- |\n",

	// Head row: pipes optional at either end, a single cell, a lone pipe, whitespace.
	"a|b\n-|-",
	"a | b\n--|--\nc | d",
	"|a|\n|-|",
	"a|\n-|",
	"|a\n|-",
	"a\n-",
	"|\n-",
	"|\n|-",
	"|\n|",
	"| \n|\n|",
	"||\n||",
	"| |\n|-|",
	"||\n|-|",
	"|a||\n|-|-|",
	"a|b\n-|-|-",
	"a|b|c\n-|-",
	"  a|b\n  -|-",
	"   |a|\n   |-|",
	"    |a|\n|-|",
	"| a \\| b |\n| - |",
	"| a \\\\| b |\n| - | - |",
	"| a\\b |\n| - |",
	"| a\\",
	"| a\\\n| - |",
	"| `a|b` |\n| - |",
	"| `a\\|b` | c |\n| - | - |\n| `d|e` |",
	"| *a* | **b** |\n| - | - |\n| _c_ | d |",
	"| é | 𝔸 |\n| - | - |\n| \u00a0 | 😀 |",
	"|a|\t|b|\n|-|-|-|",

	// Delimiter row: alignments, whitespace, markers out of place.
	"|a|b|c|d|\n|:-|-:|:-:|-|",
	"|a|\n|:|",
	"|a|\n|::|",
	"|a|\n|:-:-|",
	"|a|\n|-:-|",
	"|a|\n| - |",
	"|a|\n|\t-\t|",
	"|a|\n  |-|",
	"|a|\n   |-|",
	"|a|\n    |-|",
	"|a|\n\t|-|",
	"|a|\n|-| ",
	"|a|\n|-|x",
	"|a|\n|x|",
	"|a|\n-",
	"|a|\n---",
	"a|b\n- -",
	"a|b\n-|-\n",
	"|a|\n|-|\n\n|b|",
	"|a|\n|",
	"|a|\n||",
	"|a|b|\n|-||",
	"|a|\n|- |",
	"|a|\n|-:|\n|b|",
	"|a|\n| :--- |",
	"|a|\n",
	"|a|",
	"a|b",
	"a|b\nc",

	// Body rows: missing and extra cells, empty rows, escapes, data runs.
	"|a|b|\n|-|-|\n|c|",
	"|a|b|\n|-|-|\n|c|d|e|f|",
	"|a|\n|-|\n|",
	"|a|\n|-|\n||",
	"|a|\n|-|\n|  |",
	"|a|\n|-|\nb",
	"|a|\n|-|\nb c d|e",
	"|a|\n|-|\n|b\\|c|",
	"|a|\n|-|\n|b\\\\|c|",
	"|a|\n|-|\n|b\\",
	"|a|\n|-|\n\\",
	"|a|\n|-|\n|b|\n|c|\n|d|",
	"|a|\n|-|\n|b|\n\n|c|",
	"|a|\n|-|\n|b|\n# c",
	"|a|\n|-|\n|b|\n***",
	"|a|\n|-|\n|b|\n    c",
	"|a|\n|-|\n|b|\n```\nc\n```",

	// Around it: paragraphs, interrupting, other tables, headings.
	"p\n|a|\n|-|",
	"p\na|b\n-|-",
	"p\n|a|\n|-|\n|b|",
	"p\n\n|a|\n|-|",
	"|a|\n|-|\n|b|\n|c|\n|-|",
	"|a|\n|-|\n\n|b|\n|-|\n|c|",
	"# h\n|a|\n|-|",
	"|a|\n===",
	"|a|\n|-|\n===",
	"a\n|b|\n===",

	// Inside containers, and lazy lines.
	"> |a|\n> |-|\n> |b|",
	"> |a|\n> |-|\n|b|",
	"> |a|\n|-|",
	"> p\n|a|\n|-|",
	"- |a|\n  |-|\n  |b|",
	"- |a|\n  |-|\n|b|",
	"- |a|\n|-|",
	"- a\n- |b|\n  |-|",
	"> - |a|\n>   |-|\n>   |b|",
	"1. |a|b|\n   |:-|-:|\n   |c|d|",

	// Line endings.
	"|a|\r\n|-|\r\n|b|\r\n",
	"|a|\r|-|\r|b|\r",
	"|a|\r\n|-|\n|b|\r",
	"|a|b|\r\n|:-|-:|\r\n\r\n|c|",
}

func TestGfmTableEvents(test *testing.T) {
	test.Parallel()
	compareEvents(test, gfmTableFixtures)
}

// gfmTableAlignOracleScript is the event oracle's script, describing only each table's `_align`, which the
// event descriptions leave out and mdast-util-gfm-table reads.
var gfmTableAlignOracleScript = strings.Replace(
	oracleScript,
	`return events.map(([kind, token]) => describe(kind, token));`,
	`return events.filter(([kind, token]) => kind === "enter" && token.type === "table").map(([, token]) => token._align.join(","));`,
	1,
)

// TestGfmTableAlignMatchesUpstream compares every fixture's table alignments with upstream's `_align`.
func TestGfmTableAlignMatchesUpstream(test *testing.T) {
	test.Parallel()
	if !strings.Contains(gfmTableAlignOracleScript, "_align") {
		test.Fatal("the align oracle script no longer matches the event oracle's")
	}
	if _, lookPathError := exec.LookPath("node"); lookPathError != nil {
		test.Skip("node is not installed; the event oracle needs it")
	}
	directory := test.TempDir()
	script := filepath.Join(directory, "align.mjs")
	if writeError := os.WriteFile(
		script,
		[]byte(strings.ReplaceAll(gfmTableAlignOracleScript, "FORK", forkRoot(test))),
		0o644,
	); writeError != nil {
		test.Fatal(writeError)
	}
	encoded, encodeError := json.Marshal(gfmTableFixtures)
	if encodeError != nil {
		test.Fatal(encodeError)
	}
	command := exec.Command("node", script)
	command.Stdin = strings.NewReader(string(encoded))
	output, outputError := command.Output()
	if outputError != nil {
		test.Fatal(outputError)
	}
	var expected [][]string
	if decodeError := json.Unmarshal(output, &expected); decodeError != nil {
		test.Fatal(decodeError)
	}
	tables := 0
	for index, input := range gfmTableFixtures {
		var actual []string
		for _, event := range Parse(SourceUnits(input), MarkdownConstructs(), nil) {
			if event.Enter && event.Token.Type == typeTable {
				actual = append(actual, strings.Join(event.Token.Align, ","))
			}
		}
		tables += len(actual)
		if fmt.Sprint(expected[index]) != fmt.Sprint(actual) {
			test.Errorf("input %q: want aligns %q, got %q", truncateInput(input), expected[index], actual)
		}
	}
	if tables == 0 {
		test.Fatal("no fixture produced a table")
	}
	test.Logf("%d tables across %d fixtures", tables, len(gfmTableFixtures))
}
