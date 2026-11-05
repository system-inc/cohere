package core

import (
	"testing"

	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/lint/testing"
)

// uselessEscapeFile is where the fixtures pretend to live.
//
// A `.tsx` extension rather than `.ts`, because six of upstream's pass cases and one of its fail
// cases are JSX, and the whole point of those seven is the distinction between a string literal
// that *is* a JSX attribute value and one sitting inside an expression container. Parsed as plain
// TypeScript the JSX never becomes JSX nodes at all, and all seven would assert nothing.
const uselessEscapeFile = "/repository/source/UselessEscape.tsx"

// The corpus is oxc's, copied rather than rewritten.
//
// Every case below is verbatim from `oxc/crates/oxc_linter/src/rules/eslint/no_useless_escape.rs`:
// one Tester block holding 204 pass and 89 fail cases, with 53 of them carrying an
// `allowRegexCharacters` option. The snapshot records 105 diagnostics from those 89 inputs, so the
// per-input count is measured from the snapshot rather than assumed to be one: nine inputs report
// more than once and one reports six times.
//
// The counts were recovered by aligning each snapshot diagnostic against the source line it prints,
// which is the only field that survives upstream's own duplicate entries. Upstream lists two of its
// multiline template cases twice, byte-identical, and a count derived by walking the snapshot in
// order assigns both diagnostics to the first copy.

// TestNoUselessEscapeFires runs every failing input and asserts the exact diagnostic count.
func TestNoUselessEscapeFires(test *testing.T) {
	test.Parallel()

	cases := []struct {
		sourceText string
		findings   int
		options    rule.Given[NoUselessEscapeOptions]
	}{
		{"var foo = /\\#/;", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /\\;/;", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = \"\\'\";", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = \"\\#/\";", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = \"\\a\"", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = \"\\B\";", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = \"\\@\";", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = \"foo \\a bar\";", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = '\\\"';", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = '\\#';", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = '\\$';", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = '\\p';", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = '\\p\\a\\@';", 3, rule.Given[NoUselessEscapeOptions]{}},
		{"<foo attr={\"\\d\"}/>", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = '\\`';", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = `\\\"`;", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = `\\'`;", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = `\\#`;", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = '\\`foo\\`';", 2, rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = `\\\"${foo}\\\"`;", 2, rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = `\\'${foo}\\'`;", 2, rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = `\\#${foo}`;", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"let foo = '\\ ';", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"let foo = /\\ /;", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = `\\$\\{{${foo}`;", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = `\\$a${foo}`;", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = `a\\{{${foo}`;", 1, rule.Given[NoUselessEscapeOptions]{}},

		// Not upstream's. A brace escaped as the first content character of a chunk is the one input
		// that reads the seeded previous character, and upstream's corpus never writes it: every case it
		// has puts a real character or a dollar before the brace. Mutating the seed from a backtick to a
		// dollar therefore survived a green suite, silencing exactly this input while the other 292 stayed
		// clean. Modelling upstream's own scanner on it confirms it reports, so this is the case, not a
		// divergence.
		{"var foo = `\\{`;", 1, rule.Given[NoUselessEscapeOptions]{}},
		// The same seam read from the other end: a chunk after a substitution also begins with no
		// real previous character, so a brace escaped right after the closing brace of `${foo}` is
		// judged by the seed too.
		{"var foo = `${foo}\\{`;", 1, rule.Given[NoUselessEscapeOptions]{}},
		// Also not upstream's, and the same seam from the regex side. Outside a character class the
		// letter q is not a class escape, so this reports the `\\q` itself. It reports that and nothing
		// else, which is the half worth pinning: the shelf's scanner consumes a braced `\\q` as one
		// token wherever it appears, so the backslash inside the braces is never reached. Hardcoding
		// the escape width to two bytes made this read two findings instead of one.
		//
		// The count was written as two here from reasoning about where `\\q` is special before it was
		// measured, and the fixture caught that rather than the rule.
		{"var foo = /\\q{a\\#b}/v;", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /[ab\\-]/", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /[\\-ab]/", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /[ab\\?]/", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /[ab\\.]/", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /[a\\|b]/", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /\\-/", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /[\\-]/", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /[ab\\$]/", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /[\\(paren]/", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /[\\[]/", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /[\\/]/", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /[\\B]/", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /[a][\\-b]/", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /\\-[]/", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /[a\\^]/", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"`multiline template\n\t\t\tliteral with useless \\escape`", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"`multiline template\n\t\t\tliteral with useless \\escape`", 1, rule.Given[NoUselessEscapeOptions]{}},
		{
			"`template literal with line continuation \\\n\t\t\tand useless \\escape`",
			1,
			rule.Given[NoUselessEscapeOptions]{},
		},
		{
			"`template literal with line continuation \\\n\t\t\tand useless \\escape`",
			1,
			rule.Given[NoUselessEscapeOptions]{},
		},
		{
			"`template literal with mixed linebreaks \r\r\n\n\\and useless escape`",
			1,
			rule.Given[NoUselessEscapeOptions]{},
		},
		{
			"`template literal with mixed linebreaks in line continuations \\\n\\\r\\\r\n\\and useless escape`",
			1,
			rule.Given[NoUselessEscapeOptions]{},
		},
		{"`\\a```", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"\"use\\ strict\";", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /\\（([^\\）\\（]+)\\）$|\\(([^\\)\\)]+)\\)$/;", 6, rule.Given[NoUselessEscapeOptions]{}},
		{"var stringLiteralWithNextLine = \"line 1\\\u0085line 2\";", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"var stringLiteralWithNextLine = `line 1\\\u0085line 2`;", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"({ foo() { \"foo\"; \"bar\"; \"ba\\z\" } })", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"/[^\\^]/", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"/[^\\^]/u", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\$]/v", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\&\\&]/v", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\!\\!]/v", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\#\\#]/v", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\%\\%]/v", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\*\\*]/v", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\+\\+]/v", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\,\\,]/v", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\.\\.]/v", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\:\\:]/v", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\;\\;]/v", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\<\\<]/v", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\=\\=]/v", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\>\\>]/v", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\?\\?]/v", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\@\\@]/v", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\`\\`]/v", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\~\\~]/v", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"/[^\\^\\^]/v", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"/[_\\^\\^]/v", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\&\\&&\\&]/v", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\p{ASCII}--\\.]/v", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\p{ASCII}&&\\.]/v", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\.--[.&]]/v", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\.&&[.&]]/v", 1, rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\.--\\.--\\.]/v", 3, rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\.&&\\.&&\\.]/v", 3, rule.Given[NoUselessEscapeOptions]{}},
		{"/[[\\.&]--[\\.&]]/v", 2, rule.Given[NoUselessEscapeOptions]{}},
		{"/[[\\.&]&&[\\.&]]/v", 2, rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = \"\\#/\";", 1, rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{"#"}})},
		{"var foo = /\\#\\@/;", 1, rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{"#"}})},
		{"var foo = /[a\\@b]/", 1, rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{"#"}})},
		{"/[\\@\\@]/v", 1, rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{"#"}})},
	}

	for _, testCase := range cases {
		test.Run(testCase.sourceText, func(test *testing.T) {
			test.Parallel()
			wantIds := make([]string, testCase.findings)
			for index := range wantIds {
				wantIds[index] = "noUselessEscape"
			}
			rule_testing.ExpectFindings(test,
				rule_testing.RunGiven(test, NoUselessEscape, uselessEscapeFile,
					testCase.sourceText, testCase.options), wantIds...)
		})
	}
}

// TestNoUselessEscapeStaysSilent runs every clean input.
//
// These are the half that catch a port. The escape tables are the whole rule and each entry was
// added upstream because somebody hit it: `/\w\$\*\./` is four escapes outside a class that are
// all required, `/[a\-b]/` is a hyphen escaped in the middle of a class where escaping it is the
// only way to mean a literal, and the fifty v-flag cases pin the reserved double punctuator rule
// that no amount of reading the prose would produce correctly.
func TestNoUselessEscapeStaysSilent(test *testing.T) {
	test.Parallel()

	cases := []struct {
		sourceText string
		options    rule.Given[NoUselessEscapeOptions]
	}{
		{"var foo = /\\./", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /\\//g", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /\"\"/", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /''/", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /([A-Z])\\t+/g", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /([A-Z])\\n+/g", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /([A-Z])\\v+/g", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /\\D/", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /\\W/", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /\\w/", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /\\\\/g", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /\\w\\$\\*\\./", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /\\^\\+\\./", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /\\|\\}\\{\\./", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /]\\[\\(\\)\\//", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = \"\\x123\"", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = \"\\u00a9\"", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = \"\\377\"", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = \"\\\"\"", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = \"xs\\u2111\"", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = \"foo \\\\ bar\";", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = \"\\t\";", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = \"foo \\b bar\";", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = '\\n';", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = 'foo \\r bar';", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = '\\v';", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = '\\f';", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = '\\\n';", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = '\\\r\n';", rule.Given[NoUselessEscapeOptions]{}},
		{"<foo attr=\"\\d\"/>", rule.Given[NoUselessEscapeOptions]{}},
		{"<div> Testing: \\ </div>", rule.Given[NoUselessEscapeOptions]{}},
		{"<div> Testing: &#x5C </div>", rule.Given[NoUselessEscapeOptions]{}},
		{"<foo attr='\\d'></foo>", rule.Given[NoUselessEscapeOptions]{}},
		{"<> Testing: \\ </>", rule.Given[NoUselessEscapeOptions]{}},
		{"<> Testing: &#x5C </>", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = `\\x123`", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = `\\u00a9`", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = `xs\\u2111`", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = `foo \\\\ bar`;", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = `\\t`;", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = `foo \\b bar`;", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = `\\n`;", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = `foo \\r bar`;", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = `\\v`;", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = `\\f`;", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = `\\\n`;", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = `\\\r\n`;", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = `${foo} \\x123`", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = `${foo} \\u00a9`", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = `${foo} xs\\u2111`", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = `${foo} \\\\ ${bar}`;", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = `${foo} \\b ${bar}`;", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = `${foo}\\t`;", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = `${foo}\\n`;", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = `${foo}\\r`;", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = `${foo}\\v`;", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = `${foo}\\f`;", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = `${foo}\\\n`;", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = `${foo}\\\r\n`;", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = `\\``", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = `\\`${foo}\\``", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = `\\${{${foo}`;", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = `$\\{{${foo}`;", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = String.raw`\\.`", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = myFunc`\\.`", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /[\\d]/", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /[a\\-b]/", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /foo\\?/", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /example\\.com/", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /foo\\|bar/", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /\\^bar/", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /[\\^bar]/", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /\\(bar\\)/", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /[[\\]]/", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /[[]\\./", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /[\\]\\]]/", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /\\[abc]/", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /\\[foo\\.bar]/", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /vi/m", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /\\B/", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /\\0/", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /\\1/", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /(a)\\1/", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /(a)\\12/", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /[\\0]/", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = 'foo \\  bar'", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = 'foo \\  bar'", rule.Given[NoUselessEscapeOptions]{}},
		{"/]/", rule.Given[NoUselessEscapeOptions]{}},
		{"/\\]/", rule.Given[NoUselessEscapeOptions]{}},
		{"/\\]/u", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /foo\\]/", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /[[]\\]/", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /\\[foo\\.bar\\]/", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /(?<a>)\\k<a>/", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /(\\\\?<a>)/", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /\\p{ASCII}/u", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /\\P{ASCII}/u", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /[\\p{ASCII}]/u", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /[\\P{ASCII}]/u", rule.Given[NoUselessEscapeOptions]{}},
		{"/[^^]/", rule.Given[NoUselessEscapeOptions]{}},
		{"/[^^]/u", rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\q{abc}]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\(]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\)]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\{]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\]]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\}]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\/]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\-]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\|]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\$$]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\&&]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\!!]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\##]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\%%]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\**]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\++]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\,,]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\..]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\::]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\;;]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\<<]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\==]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\>>]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\??]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\@@]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\``]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\~~]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"/[^\\^^]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"/[_\\^^]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"/[$\\$]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"/[&\\&]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"/[!\\!]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"/[#\\#]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"/[%\\%]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"/[*\\*]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"/[+\\+]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"/[,\\,]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"/[.\\.]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"/[:\\:]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"/[;\\;]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"/[<\\<]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"/[=\\=]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"/[>\\>]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"/[?\\?]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"/[@\\@]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"/[`\\`]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"/[~\\~]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"/[^^\\^]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"/[_^\\^]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\&&&\\&]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"/[[\\-]\\-]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\^]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"const s = \"⚓💥⚓\";\n/[\\&&]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"const s = \"⚓💥⚓\";\n/[&\\&]/v", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /\\#/;", rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{"#"}})},
		{"var foo = /\\;/;", rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{";"}})},
		{"var foo = /\\#\\;/;", rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{"#", ";"}})},
		{"var foo = /[ab\\-]/", rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{"-"}})},
		{"var foo = /[\\-ab]/", rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{"-"}})},
		{"var foo = /[ab\\?]/", rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{"?"}})},
		{"var foo = /[ab\\.]/", rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{"."}})},
		{"var foo = /[a\\|b]/", rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{"|"}})},
		{"var foo = /\\-/", rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{"-"}})},
		{"var foo = /[\\-]/", rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{"-"}})},
		{"var foo = /[ab\\$]/", rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{"$"}})},
		{"var foo = /[\\(paren]/", rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{"("}})},
		{"var foo = /[\\[]/", rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{"["}})},
		{"var foo = /[\\/]/", rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{"/"}})},
		{"var foo = /[\\B]/", rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{"B"}})},
		{"var foo = /[a][\\-b]/", rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{"-"}})},
		{"var foo = /\\-[]/", rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{"-"}})},
		{"var foo = /[a\\^]/", rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{"^"}})},
		{"/[^\\^]/", rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{"^"}})},
		{"/[^\\^]/u", rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{"^"}})},
		{"/[\\$]/v", rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{"$"}})},
		{"/[\\&\\&]/v", rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{"&"}})},
		{"/[\\!!]/v", rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{"!"}})},
		{"/[\\##]/v", rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{"#"}})},
		{"/[\\%%]/v", rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{"%"}})},
		{"/[\\*\\*]/v", rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{"*"}})},
		{"/[\\+\\+]/v", rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{"+"}})},
		{"/[\\,,]/v", rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{","}})},
		{"/[\\..]/v", rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{"."}})},
		{"/[\\:\\:]/v", rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{":"}})},
		{"/[\\;\\;]/v", rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{";"}})},
		{"/[\\<\\<]/v", rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{"<"}})},
		{"/[\\=\\=]/v", rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{"="}})},
		{"/[\\>\\>]/v", rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{">"}})},
		{"/[\\?\\?]/v", rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{"?"}})},
		{"/[\\@\\@]/v", rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{"@"}})},
		{"/[\\``]/v", rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{"`"}})},
		{"/[\\~\\~]/v", rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{"~"}})},
		{"/[^\\^\\^]/v", rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{"^"}})},
		{"/[_\\^\\^]/v", rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{"^"}})},
		{"/[\\&\\&&\\&]/v", rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{"&"}})},
		{"/[\\p{ASCII}--\\.]/v", rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{"."}})},
		{"/[\\p{ASCII}&&\\.]/v", rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{"."}})},
		// Not upstream's. A wide escape carries a payload the scan must step over whole, and nothing in
		// upstream's corpus puts a backslash inside one. Hardcoding the escape width to two bytes
		// therefore survived a green suite while reporting a phantom finding on each of these: the scan
		// resumed inside the braces and read the interior backslash as an escape of its own. A property
		// escape is one token however long its name is, and a `\\q` string disjunction is one token
		// inside a v-flag class, so the interior of both is pattern text rather than escapes.
		{"var foo = /\\p{AS\\#CII}/u;", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /[\\p{AS\\#CII}]/u;", rule.Given[NoUselessEscapeOptions]{}},
		{"var foo = /[\\q{a\\#b}]/v;", rule.Given[NoUselessEscapeOptions]{}},
		{"/[\\.--[.&]]/v", rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{"."}})},
		{"/[\\.&&[.&]]/v", rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{"."}})},
		{"/[\\.--\\.--\\.]/v", rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{"."}})},
		{"/[\\.&&\\.&&\\.]/v", rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{"."}})},
		{"/[[\\.&]--[\\.&]]/v", rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{"."}})},
		{"/[[\\.&]&&[\\.&]]/v", rule.Configured(NoUselessEscapeOptions{AllowRegexCharacters: []string{"."}})},
	}

	for _, testCase := range cases {
		test.Run(testCase.sourceText, func(test *testing.T) {
			test.Parallel()
			rule_testing.ExpectClean(test,
				rule_testing.RunGiven(test, NoUselessEscape, uselessEscapeFile,
					testCase.sourceText, testCase.options))
		})
	}
}

// TestNoUselessEscapeRepairsSource applies the fixes and compares the resulting text.
//
// Brief-mandated and it is the assertion that matters most here: this rule's whole output is a span
// and a one-character replacement, and `ExpectFindings` sees neither. A fix that writes the right
// character one byte to the left deletes the character before the backslash and leaves the backslash
// standing, and every count-based fixture passes while the rewrite corrupts the file.
//
// The expectations are upstream's own `fix` vector, verbatim.
func TestNoUselessEscapeRepairsSource(test *testing.T) {
	test.Parallel()

	cases := []struct {
		sourceText string
		wantSource string
	}{
		{"var foo = /\\#/;", "var foo = /#/;"},
		{"var foo = /\\;/;", "var foo = /;/;"},
		{"var foo = \"\\'\";", "var foo = \"'\";"},
		{"var foo = \"\\#/\";", "var foo = \"#/\";"},
		{"var foo = \"\\a\"", "var foo = \"a\""},
		{"var foo = \"\\B\";", "var foo = \"B\";"},
		{"var foo = \"\\@\";", "var foo = \"@\";"},
		{"var foo = \"foo \\a bar\";", "var foo = \"foo a bar\";"},
		{"var foo = '\\\"';", "var foo = '\"';"},
		{"var foo = '\\#';", "var foo = '#';"},
		{"var foo = '\\$';", "var foo = '$';"},
		{"var foo = '\\p';", "var foo = 'p';"},
		// Upstream expects `p\a@` here, and the difference is its fixer rather than its rule. oxc
		// treats boundary-adjacent fixes as overlapping to match ESLint, so of the three fixes this
		// input produces it applies the first and third and leaves the middle escape standing until a
		// second pass. Our engine states the opposite policy at internal/fix/apply.go: adjacent edits
		// compose safely and are admitted. Both agree on the three findings; only the number of passes
		// needed to land them differs, and one pass is the better answer.
		{"var foo = '\\p\\a\\@';", "var foo = 'pa@';"},
		{"<foo attr={\"\\d\"}/>", "<foo attr={\"d\"}/>"},
		{"var foo = '\\`';", "var foo = '`';"},
		{"var foo = `\\\"`;", "var foo = `\"`;"},
		{"var foo = `\\'`;", "var foo = `'`;"},
		{"var foo = `\\#`;", "var foo = `#`;"},
		{"var foo = '\\`foo\\`';", "var foo = '`foo`';"},
		{"var foo = `\\\"${foo}\\\"`;", "var foo = `\"${foo}\"`;"},
		{"var foo = `\\'${foo}\\'`;", "var foo = `'${foo}'`;"},
		{"var foo = `\\#${foo}`;", "var foo = `#${foo}`;"},
		{"let foo = '\\ ';", "let foo = ' ';"},
		{"let foo = /\\ /;", "let foo = / /;"},
		{"var foo = `\\$\\{{${foo}`;", "var foo = `$\\{{${foo}`;"},
		{"\"use\\ strict\";", "\"use strict\";"},
		{"({ foo() { \"foo\"; \"bar\"; \"ba\\z\" } })", "({ foo() { \"foo\"; \"bar\"; \"baz\" } })"},
		{"/[^\\^]/", "/[^^]/"},
		{"/[^\\^]/u", "/[^^]/u"},
		{"/[\\$]/v", "/[$]/v"},
		{"/[\\&\\&]/v", "/[&\\&]/v"},
		{"/[\\!\\!]/v", "/[!\\!]/v"},
		{"/[\\#\\#]/v", "/[#\\#]/v"},
		{"/[\\%\\%]/v", "/[%\\%]/v"},
		{"/[\\*\\*]/v", "/[*\\*]/v"},
		{"/[\\+\\+]/v", "/[+\\+]/v"},
		{"/[\\,\\,]/v", "/[,\\,]/v"},
		{"/[\\.\\.]/v", "/[.\\.]/v"},
		{"/[\\:\\:]/v", "/[:\\:]/v"},
		{"/[\\;\\;]/v", "/[;\\;]/v"},
		{"/[\\<\\<]/v", "/[<\\<]/v"},
		{"/[\\=\\=]/v", "/[=\\=]/v"},
		{"/[\\>\\>]/v", "/[>\\>]/v"},
		{"/[\\?\\?]/v", "/[?\\?]/v"},
		{"/[\\@\\@]/v", "/[@\\@]/v"},
		{"/[\\`\\`]/v", "/[`\\`]/v"},
		{"/[\\~\\~]/v", "/[~\\~]/v"},
		{"/[^\\^\\^]/v", "/[^^\\^]/v"},
		{"/[_\\^\\^]/v", "/[_^\\^]/v"},
		{"/[\\&\\&&\\&]/v", "/[&\\&&\\&]/v"},
		{"/[\\p{ASCII}--\\.]/v", "/[\\p{ASCII}--.]/v"},
		{"/[\\p{ASCII}&&\\.]/v", "/[\\p{ASCII}&&.]/v"},
		{"/[\\.--[.&]]/v", "/[.--[.&]]/v"},
		{"/[\\.&&[.&]]/v", "/[.&&[.&]]/v"},
		{"/[\\.--\\.--\\.]/v", "/[.--.--.]/v"},
		{"/[\\.&&\\.&&\\.]/v", "/[.&&.&&.]/v"},
		{"/[[\\.&]--[\\.&]]/v", "/[[.&]--[.&]]/v"},
		{"/[[\\.&]&&[\\.&]]/v", "/[[.&]&&[.&]]/v"},
	}

	for _, testCase := range cases {
		test.Run(testCase.sourceText, func(test *testing.T) {
			test.Parallel()
			rule_testing.ExpectFixedSource(test,
				rule_testing.Run(test, NoUselessEscape, uselessEscapeFile, testCase.sourceText),
				testCase.wantSource)
		})
	}
}
