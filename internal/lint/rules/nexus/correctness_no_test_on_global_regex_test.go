package nexus

import (
	"sort"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/testing"
)

const correctnessNoTestOnGlobalRegexFile = "/repository/source/CorrectnessNoTestOnGlobalRegex.ts"

var correctnessNoTestOnGlobalRegexPrelude = strings.Join([]string{
	"declare const lines: string[];",
	"declare const text: string;",
	"declare const position: number;",
	"declare function use(value: unknown): void;",
	"",
}, "\n")

func correctnessNoTestOnGlobalRegexSource(lines ...string) string {
	return correctnessNoTestOnGlobalRegexPrelude + strings.Join(lines, "\n") + "\n"
}

func correctnessNoTestOnGlobalRegexRun(test *testing.T, sourceText string) rule_testing.Result {
	test.Helper()
	return rule_testing.RunTypedFiles(test, CorrectnessNoTestOnGlobalRegex, map[string]string{
		correctnessNoTestOnGlobalRegexFile: sourceText,
	}, correctnessNoTestOnGlobalRegexFile)
}

// correctnessNoTestOnGlobalRegexExpect asserts the reported spans in source order, and that no finding
// carries a fix.
func correctnessNoTestOnGlobalRegexExpect(test *testing.T, result rule_testing.Result, want ...string) {
	test.Helper()
	// The shared assertion by message id, which proves the rule can fire; the checks below hold
	// each finding's exact text.
	if len(want) > 0 {
		wantIds := make([]string, len(want))
		for index := range wantIds {
			wantIds[index] = correctnessNoTestOnGlobalRegexId
		}
		rule_testing.ExpectFindings(test, result, wantIds...)
	}
	diagnostics := append(result.Diagnostics[:0:0], result.Diagnostics...)
	sort.SliceStable(diagnostics, func(left int, right int) bool {
		return diagnostics[left].Range.Pos() < diagnostics[right].Range.Pos()
	})
	text := result.SourceFile.Text()
	var got []string
	for _, diagnostic := range diagnostics {
		if diagnostic.Message.Id != correctnessNoTestOnGlobalRegexId {
			test.Fatalf("unexpected message id %q", diagnostic.Message.Id)
		}
		if len(diagnostic.Fixes) != 0 || len(diagnostic.Suggestions) != 0 {
			test.Fatalf(
				"expected no fix and no suggestion, got %d and %d",
				len(diagnostic.Fixes),
				len(diagnostic.Suggestions),
			)
		}
		got = append(got, text[diagnostic.Range.Pos():diagnostic.Range.End()])
	}
	if len(got) != len(want) {
		test.Fatalf("expected %d findings %q, got %d %q", len(want), want, len(got), got)
	}
	for index := range want {
		if got[index] != want[index] {
			test.Fatalf("finding %d is %q, want %q", index, got[index], want[index])
		}
	}
}

// correctnessNoTestOnGlobalRegexCompLeak models the comp-leak guard as commit `6a4b7896` added it to
// `modules/os/wisdom/AhraOsWisdom.ts` in ahra: one module-level `g` regex serving both a `.test()`
// and a `replace`. `reset` is the hand patch the file shipped with (`lastIndex = 0` after each true);
// `fixed` gives the `.test()` its own regex without the flag.
func correctnessNoTestOnGlobalRegexCompLeak(reset bool, fixed bool) string {
	testPattern := "compLeakDollarPattern"
	declarations := []string{"const compLeakDollarPattern = /\\$\\s?\\d[\\d,]*(?:\\.\\d{2})?/g;"}
	if fixed {
		testPattern = "compLeakDollarTestPattern"
		declarations = append(declarations, "const compLeakDollarTestPattern = /\\$\\s?\\d[\\d,]*(?:\\.\\d{2})?/;")
	}
	resetLine := ""
	if reset {
		resetLine = "        compLeakDollarPattern.lastIndex = 0; // reset the global regex's cursor for the next call"
	}
	lines := append(declarations,
		"const compWordPattern = /\\bcomp\\b/i;",
		"export function textCarriesCompCommitment(text: string): boolean {",
		"    if("+testPattern+".test(text)) {",
		resetLine,
		"        return true;",
		"    }",
		"    return compWordPattern.test(text);",
		"}",
		"export function maskCompTokens(text: string): string {",
		"    const masked = text.replace(compLeakDollarPattern, '[amount]');",
		"    compLeakDollarPattern.lastIndex = 0;",
		"    return masked;",
		"}",
	)
	return correctnessNoTestOnGlobalRegexSource(lines...)
}

// The real guard as it shipped, hand patch and all: the `.test()` on the shared `g` regex is reported.
func TestCorrectnessNoTestOnGlobalRegexFiresOnAhraOsWisdom(test *testing.T) {
	test.Parallel()

	result := correctnessNoTestOnGlobalRegexRun(test, correctnessNoTestOnGlobalRegexCompLeak(true, false))
	correctnessNoTestOnGlobalRegexExpect(test, result, "compLeakDollarPattern.test(text)")
}

// Without the hand patch, the shape the patch was written for: reported the same.
func TestCorrectnessNoTestOnGlobalRegexFiresOnUnpatchedAhraOsWisdom(test *testing.T) {
	test.Parallel()

	result := correctnessNoTestOnGlobalRegexRun(test, correctnessNoTestOnGlobalRegexCompLeak(false, false))
	correctnessNoTestOnGlobalRegexExpect(test, result, "compLeakDollarPattern.test(text)")
}

// After the fix: the `.test()` has a regex without `g`, the `replace` keeps its own, and nothing fires.
func TestCorrectnessNoTestOnGlobalRegexStaysSilentOnFixedAhraOsWisdom(test *testing.T) {
	test.Parallel()

	rule_testing.ExpectClean(
		test,
		correctnessNoTestOnGlobalRegexRun(test, correctnessNoTestOnGlobalRegexCompLeak(false, true)),
	)
}

func TestCorrectnessNoTestOnGlobalRegexFires(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name  string
		lines []string
		want  []string
	}{
		{"a local regex tested on every turn of a loop", []string{
			"export function count() {",
			"    const pattern = /todo/gi;",
			"    let found = 0;",
			"    for(const line of lines) {",
			"        if(pattern.test(line)) found++;",
			"    }",
			"    return found;",
			"}",
		}, []string{"pattern.test(line)"}},
		{"a local regex tested in a callback", []string{
			"export function matching() {",
			"    const pattern = /todo/g;",
			"    return lines.filter((line) => pattern.test(line));",
			"}",
		}, []string{"pattern.test(line)"}},
		{"a readonly instance property tested in a method", []string{
			"export class Matcher {",
			"    private readonly pattern = /todo/g;",
			"    matches(line: string) {",
			"        return this.pattern.test(line);",
			"    }",
			"}",
		}, []string{"this.pattern.test(line)"}},
		{"a static readonly property", []string{
			"export class Matcher {",
			"    static readonly pattern = /todo/g;",
			"    static matches(line: string) {",
			"        return Matcher.pattern.test(line);",
			"    }",
			"}",
		}, []string{"Matcher.pattern.test(line)"}},
		{"a module-level RegExp built with literal flags", []string{
			"const pattern = new RegExp('todo', 'gu');",
			"export function matches(line: string) {",
			"    return pattern.test(line);",
			"}",
		}, []string{"pattern.test(line)"}},
		{"module-level code inside a loop", []string{
			"const pattern = /todo/g;",
			"let found = 0;",
			"while(found < lines.length) {",
			"    if(pattern.test(lines[found])) use(found);",
			"    found++;",
			"}",
		}, []string{"pattern.test(lines[found])"}},
		{"a global sticky regex never positioned", []string{
			"const word = /\\w+/gy;",
			"export function startsWithWord(source: string) {",
			"    return word.test(source);",
			"}",
		}, []string{"word.test(source)"}},
		{"a for loop's update runs on every turn", []string{
			"export function scan() {",
			"    const pattern = /todo/g;",
			"    for(let index = 0; index < lines.length; index += pattern.test(lines[index]) ? 2 : 1) {",
			"        use(index);",
			"    }",
			"}",
		}, []string{"pattern.test(lines[index])"}},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()

			result := correctnessNoTestOnGlobalRegexRun(test, correctnessNoTestOnGlobalRegexSource(testCase.lines...))
			correctnessNoTestOnGlobalRegexExpect(test, result, testCase.want...)
		})
	}
}

func TestCorrectnessNoTestOnGlobalRegexStaysSilent(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name  string
		lines []string
	}{
		{"a module-level regex without g", []string{
			"const pattern = /todo/i;",
			"export function matches(line: string) {",
			"    return pattern.test(line);",
			"}",
		}},
		{"the exec loop, MediumConversationSource's shape", []string{
			"const sectionPattern = /<\\/?section\\b[^>]*>/gi;",
			"export function sections(sourceHtml: string) {",
			"    let depth = 0;",
			"    for(let token = sectionPattern.exec(sourceHtml); token; token = sectionPattern.exec(sourceHtml)) {",
			"        depth += /^<\\//.test(token[0]) ? -1 : 1;",
			"    }",
			"    return depth;",
			"}",
		}},
		{"test as a while condition, counting matches", []string{
			"const pattern = /todo/g;",
			"export function count(line: string) {",
			"    let found = 0;",
			"    while(pattern.test(line)) found++;",
			"    return found;",
			"}",
		}},
		{"test inside a do-while condition", []string{
			"const pattern = /todo/g;",
			"export function count(line: string) {",
			"    let found = -1;",
			"    do { found++; } while(found < 100 && pattern.test(line));",
			"    return found;",
			"}",
		}},
		{"a regex made in the same run as the call", []string{
			"export function matches(line: string) {",
			"    const pattern = /todo/g;",
			"    return pattern.test(line);",
			"}",
		}},
		{"a regex made inside the loop that tests it", []string{
			"export function count() {",
			"    let found = 0;",
			"    for(const line of lines) {",
			"        const pattern = /todo/g;",
			"        if(pattern.test(line)) found++;",
			"    }",
			"    return found;",
			"}",
		}},
		{"a test in a for...of's iterated expression, which runs once", []string{
			"export function once() {",
			"    const pattern = /todo/g;",
			"    for(const each of [pattern.test(text)]) use(each);",
			"}",
		}},
		{"a sticky regex without g, outside the rule", []string{
			"const word = /\\w+/y;",
			"export function startsWithWord(source: string) {",
			"    return word.test(source);",
			"}",
		}},
		{"a sticky regex positioned on purpose", []string{
			"const word = /\\w+/y;",
			"export function wordAt(source: string, index: number) {",
			"    word.lastIndex = index;",
			"    return word.test(source);",
			"}",
		}},
		{"a global regex positioned to search from an index", []string{
			"const pattern = /todo/g;",
			"export function after(line: string) {",
			"    pattern.lastIndex = position;",
			"    return pattern.test(line);",
			"}",
		}},
		{"a let, which may hold another regex by the call", []string{
			"let pattern = /todo/g;",
			"export function matches(line: string) {",
			"    pattern = /todo/;",
			"    return pattern.test(line);",
			"}",
		}},
		{"a writable property", []string{
			"export class Matcher {",
			"    pattern = /todo/g;",
			"    matches(line: string) {",
			"        return this.pattern.test(line);",
			"    }",
			"}",
		}},
		{"RegExp built from a variable's flags", []string{
			"declare const flags: string;",
			"const pattern = new RegExp('todo', flags);",
			"export function matches(line: string) {",
			"    return pattern.test(line);",
			"}",
		}},
		{"a test method that is not RegExp's", []string{
			"const pattern = { source: /todo/g, test(line: string) { return line.length > 0; } };",
			"export function matches(line: string) {",
			"    return pattern.test(line);",
			"}",
		}},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()

			rule_testing.ExpectClean(
				test,
				correctnessNoTestOnGlobalRegexRun(test, correctnessNoTestOnGlobalRegexSource(testCase.lines...)),
			)
		})
	}
}
