package comments

import (
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/core"
)

// The guard must not change which comments are found, only how fast the scan gets there, so each case
// compares the guarded scan's ranges with the ungated scan's as well as counting them.
//
// A comparison over 800 real files once passed a gate that would have broken `s\tc`, because no file
// happened to hold the multi-space form. Absence from a sample is not absence in general, so the
// separators and the near-miss shapes are written out by hand.
func TestCommentGuardHandlesEverySeparator(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name       string
		sourceText string
		wantCount  int
	}{
		{"separated by a space", "const a = 1; // trailing\n", 1},
		{"separated by a tab", "const a = 1;\t// trailing\n", 1},
		{"separated by a newline", "const a = 1;\n// leading\nconst b = 2;\n", 1},
		{"separated by a carriage return and newline", "const a = 1;\r\n// leading\r\nconst b = 2;\r\n", 1},
		{"no separator at all", "const a = 1;// trailing\n", 1},
		{"block comment with no separator", "const a = 1;/* trailing */\n", 1},
		{"comment at position zero", "// first line\nconst a = 1;\n", 1},
		{"two comments on one line", "const a = 1; /* one */ /* two */\n", 2},
		{"no comments at all", "const a = 1;\nconst b = 2;\n", 0},

		// A shebang is trivia the comment scanner walks past, and it is the one non-whitespace thing
		// a comment can sit behind. A real file caught this before the hand-written cases did: a
		// real executable script lost its module comment and its first import comment, because the
		// guard stopped at the `#`. Pinned here so it cannot regress quietly.
		{"comment after a shebang", "#!/usr/bin/env -S pnpm tsx\n\n/* module comment */\nconst a = 1;\n", 1},
		{"line comment after a shebang", "#!/usr/bin/env node\n// leading\nconst a = 1;\n", 1},

		// The guard admits these and the scanner then declines them, which is the safe direction and
		// is on purpose: a slash that is not a comment costs one scan, and a comment the guard hid
		// would silence every comment rule for it.
		{"a division is not a comment", "const a = 6 / 2;\n", 0},
		{"a regex literal is not a comment", "const a = /abc/.test(b);\n", 0},
		{"a regex containing a slash", "const a = /a\\/b/.test(c);\n", 0},
		{"a string containing two slashes", "const a = 'http://example.com';\n", 0},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			sourceFile := parseSourceForTest(test, testCase.sourceText)

			guarded := All(sourceFile)
			reference := AllWithoutGuard(sourceFile)

			if len(guarded) != testCase.wantCount {
				test.Fatalf("expected %d comments, guard found %d", testCase.wantCount, len(guarded))
			}
			if len(guarded) != len(reference) {
				test.Fatalf("guard found %d, ungated scan found %d", len(guarded), len(reference))
			}
			for index := range guarded {
				if guarded[index].Range != reference[index].Range {
					test.Fatalf("comment %d differs from the ungated scan", index)
				}
			}
		})
	}
}

// A guard that never skips is pure overhead, and a guard that skips everything is a bug that comparing
// against the ungated scan alone would not catch on input with few comments.
func TestCommentGuardActuallySkips(test *testing.T) {
	test.Parallel()
	// A run of whitespace with no slash cannot begin a comment.
	if canBeginAt("const a = 1;    const b = 2;", 12) {
		test.Fatalf("guard admits a position with no slash after it")
	}
	// A slash after whitespace must be admitted, whatever the whitespace is.
	for _, text := range []string{"a; // c", "a;\t// c", "a;\n// c", "a;\r\n// c", "a;// c", "a;/* c */"} {
		if !canBeginAt(text, 2) {
			test.Fatalf("guard rejects %q, where a comment does begin", text)
		}
	}
}

// placedComment is one comment a placement fixture expects: its text, and the zero-based line and byte column it
// starts at.
type placedComment struct {
	text   string
	line   int
	column int
}

// The guard must find the same comments as the ungated scan wherever a comment can sit, and both must find each one
// where it is.
//
// A comparison over 1,200 real files held this before, and a sample shows only the placements it happens to hold.
// Each fixture here is a file holding a comment in every placement of one kind: leading and trailing a statement,
// dangling in an empty body or list, between decorators, after a trailing comma, and inside JSX. The expected list
// is written out, so a placement both scans lose fails as surely as one the guard alone loses, which a comparison
// of the two cannot see.
func TestCommentGuardFindsTheSameCommentsInEveryPlacement(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name       string
		scriptKind core.ScriptKind
		sourceText string
		want       []placedComment
	}{
		{
			name:       "around a decorated class",
			scriptKind: core.ScriptKindTS,
			sourceText: strings.Join([]string{
				"// leading the file",
				"import { Injectable, Logged } from './injection'; // trailing an import",
				"",
				"/** The service, documented. */",
				"@Injectable() // trailing a decorator",
				"/* between decorators */",
				"@Logged()",
				"export class Service {",
				"    // leading a member",
				"    constructor(/* no parameters */) {}",
				"",
				"    run(): void {",
				"        /* dangling in an empty body */",
				"    }",
				"",
				"    @Logged() /* between a decorator and its method */ stop(): void {}",
				"",
				"    names = [",
				"        'first', // after the first element",
				"        'second', // after a trailing comma",
				"    ];",
				"}",
				"// after the last statement",
				"",
			}, "\n"),
			want: []placedComment{
				{"// leading the file", 0, 0},
				{"// trailing an import", 1, 50},
				{"/** The service, documented. */", 3, 0},
				{"// trailing a decorator", 4, 14},
				{"/* between decorators */", 5, 0},
				{"// leading a member", 8, 4},
				{"/* no parameters */", 9, 16},
				{"/* dangling in an empty body */", 12, 8},
				{"/* between a decorator and its method */", 15, 14},
				{"// after the first element", 18, 17},
				{"// after a trailing comma", 19, 18},
				{"// after the last statement", 22, 0},
			},
		},
		{
			name:       "inside JSX",
			scriptKind: core.ScriptKindTSX,
			sourceText: strings.Join([]string{
				"export function Card(properties: { title: string }) {",
				"    return (",
				"        <section>",
				"            {/* an empty expression container */}",
				"            <span title={properties.title /* trailing an attribute value */}>{properties.title}</span>",
				"            {",
				"                // a line comment alone in a container",
				"            }",
				"            <input /* between a tag name and its end */ />",
				"        </section>",
				"    ); // trailing the return",
				"}",
				"",
			}, "\n"),
			want: []placedComment{
				{"/* an empty expression container */", 3, 13},
				{"/* trailing an attribute value */", 4, 42},
				{"// a line comment alone in a container", 6, 16},
				{"/* between a tag name and its end */", 8, 19},
				{"// trailing the return", 10, 7},
			},
		},
		{
			name:       "dangling in empty lists and bodies",
			scriptKind: core.ScriptKindTS,
			sourceText: strings.Join([]string{
				"#!/usr/bin/env node",
				"/* after a shebang */",
				"enum Empty { /* no members */ }",
				"interface Nothing {",
				"    // no members either",
				"}",
				"const call = run(/* no arguments */);",
				"const pair: [first: string /* the key */, second: number] = ['a', 1];",
				"switch (call) {",
				"    case 1: {",
				"        // falls through",
				"    }",
				"    default: /* the rest */",
				"}",
			}, "\n"),
			want: []placedComment{
				{"/* after a shebang */", 1, 0},
				{"/* no members */", 2, 13},
				{"// no members either", 4, 4},
				{"/* no arguments */", 6, 17},
				{"/* the key */", 7, 27},
				{"// falls through", 10, 8},
				{"/* the rest */", 12, 13},
			},
		},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			sourceFile := parseSourceForTestAs(test, testCase.sourceText, testCase.scriptKind)

			guarded := All(sourceFile)
			reference := AllWithoutGuard(sourceFile)

			got := make([]placedComment, 0, len(guarded))
			for _, comment := range guarded {
				got = append(got, placedComment{
					text:   comment.Text,
					line:   comment.StartLine,
					column: comment.StartColumn,
				})
			}
			if len(got) != len(testCase.want) {
				test.Fatalf("the guarded scan found %d comments, want %d", len(got), len(testCase.want))
			}
			for index, want := range testCase.want {
				if got[index] != want {
					test.Errorf("comment %d: the guarded scan found %+v, want %+v", index, got[index], want)
				}
			}

			if len(guarded) != len(reference) {
				test.Fatalf("the guard found %d comments, the ungated scan %d", len(guarded), len(reference))
			}
			for index := range guarded {
				if guarded[index].Range != reference[index].Range || guarded[index].Text != reference[index].Text {
					test.Errorf("comment %d: the guard found %q at %v, the ungated scan %q at %v", index,
						guarded[index].Text, guarded[index].Range, reference[index].Text, reference[index].Range)
				}
			}
		})
	}
}
