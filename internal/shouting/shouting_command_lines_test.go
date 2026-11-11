package shouting

import (
	"slices"
	"testing"
)

// A line opening with a command word is masked as a command unless it continues a sentence and what follows the word
// is not shaped like arguments, so a command keeps its mask in every shape it is written in, and a sentence a
// formatter wrapped onto `git` or `ahra` is read. Each masked case carries a shout inside the command, which is what
// would report if the mask let go; each read case is prose whose shout the old mask hid (#tp7ahz0).
func TestCommandLinesAreMaskedAndProseOnThemIsRead(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name string
		text string
		want []string
	}{
		{"git status after a blank line", "Check the tree first\n\ngit status --short NEVER", nil},
		{"git status after a colon", "Check the tree first:\ngit status NEVER", nil},
		{
			"git status with a flag, continuing a sentence",
			"the tree is read with\ngit status --porcelain NEVER",
			nil,
		},
		{
			"a Usage list of ahra commands",
			"Usage:\nahra os skills <username> --render <id>\nahra backup list NEVER\nahra run THE THING",
			nil,
		},
		{"sqlite3 after a blank line", "Read the rows\n\nsqlite3 db SELECT * FROM users", nil},
		{"SQL in capitals, continuing a sentence", "read the rows with\nsqlite3 db SELECT * FROM users", nil},
		{
			"a run of command lines",
			"Then land it\n\ngit fetch origin +<sha>:refs/heads/main\ngit push --force-with-lease=main NEVER\n" +
				"git merge-base --is-ancestor <tip> <sha> ALWAYS",
			nil,
		},
		{"subcommands before a placeholder", "the skills render through\nahra os skills <username> NEVER", nil},
		{"a file name argument", "it uploads with\npnpm tsx PhiSocialUpload.ts --dry-run NEVER", nil},
		{"a prompt line always", "the prose goes on\n$ ahra run, which NEVER waits", nil},
		{"an s c line always", "the prose goes on\ns c, which NEVER waits", nil},
		{"an s c line with its letters spaced apart", "the prose goes on\ns   c --no-cache ALWAYS", nil},
		{
			"a table of commands set in under a sentence",
			"/*\n *   Catalog family, read the catalog itself\n" +
				" *     ahra os skills catalog   every skill and a CHECK\n */",
			nil,
		},
		{"a command first in a block comment", "/*\n * git and go are stand-ins: it marks HOME\n */", nil},
		{
			"the wrapped loom sentence",
			"the test runs\ngit and go are stand-ins: it marks HOME when it runs.",
			[]string{"HOME"},
		},
		{
			"the wrapped loom sentence behind a gutter",
			"/*\n * the test runs\n * git and go are stand-ins: it marks HOME\n */",
			[]string{"HOME"},
		},
		{
			"a prose continuation after ahra",
			"a rule that only needs a count calls\nahra run, which NEVER waits",
			[]string{"NEVER"},
		},
		{
			"a prose continuation after npm",
			"the toolchain is pinned, and\nnpm comes from the registry, ALWAYS held to it",
			[]string{"ALWAYS"},
		},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(subtest *testing.T) {
			subtest.Parallel()
			masked := MaskCodeAndCommands(testCase.text)
			if len(masked) != len(testCase.text) {
				subtest.Fatalf("masking %q changed its length from %d to %d bytes",
					testCase.text, len(testCase.text), len(masked))
			}
			if got := distinctShoutedWords(masked); !slices.Equal(got, testCase.want) {
				subtest.Fatalf("shouts in %q: got %v, want %v (masked: %q)", testCase.text, got, testCase.want, masked)
			}
		})
	}
}
