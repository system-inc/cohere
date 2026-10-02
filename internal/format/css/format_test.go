package css

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/format/doc"
	"github.com/system-inc/cohere/internal/format/prettier"
)

// Each snippet goes through this printer and through the embedded Prettier fork, and the two outputs must
// be equal. Snippets are written unformatted, so the oracle rewrites each one and an identity printer
// cannot pass; contains spells out what the case is about, so a printer and an oracle that both lost it
// cannot agree their way past it. Every case runs under every option set in formatOptionSets.

type formatCase struct {
	name     string
	source   string
	contains []string
}

var formatCases = []formatCase{
	{
		name:     "rules: selectors, declarations, empty rules, semicolons",
		source:   "a{color:red;background:blue}\nb { }\nc{color:red;;}\n\n\n\nd{COLOR:Red}\ne{margin:0}",
		contains: []string{"a {\n", "color: red;", "b {\n}", "color: Red;"},
	},
	{
		name: "selectors: combinators, pseudo-classes, pseudo-elements, attributes, namespaces, lists",
		source: "a>b~c+d   e{x:y}\nA:HOVER::BEFORE,.Class:not(.b,.c):nth-child(2N+1){x:y}\n" +
			"[data-foo=bar],[data-foo='bar' i],[ data-x ],svg|a,*|*,|b,*{x:y}\na:is(.b , .c) > d:where(.e){x:y}\n" +
			".a.b#c::after,ul li:first-child{x:y}\n:root{x:y}\na >>> b{x:y}\n.a\n.b{x:y}",
		contains: []string{"a > b ~ c + d e", "A:hover::before,\n", ".Class:not(.b, .c):nth-child(2n + 1)", "[data-foo='bar' i]"},
	},
	{
		name: "a long selector list and a long compound selector break",
		source: ".a-very-long-selector-name-number-one .another-long-descendant-selector > .child-selector-name ~ .sibling-selector-name-here{x:y}\n" +
			".first-selector-in-the-list,.second-selector-in-the-list,.third-selector-in-the-list{x:y}\n" +
			"a:not(.a-very-long-class-name-number-one,.a-very-long-class-name-number-two,.a-very-long-class-name-number-three){x:y}",
		contains: []string{".first-selector-in-the-list,\n"},
	},
	{
		name: "@media: types, features, ranges, and, or, not, only, lists",
		source: "@media screen and (min-width:100px) , print and (orientation:LANDSCAPE){a{x:y}}\n" +
			"@media (min-width:640PX) and (max-width:1023.50px){a{x:y}}\n@media not all and (monochrome){a{x:y}}\n" +
			"@media only screen   and (max-width : 100px){a{x:y}}\n@media (width>=600px){a{x:y}}\n@MEDIA print{a{x:y}}\n" +
			"@media tv2.50,(min-resolution:2.00DPPX){a{x:y}}",
		contains: []string{"@media screen and (min-width: 100px), print and (orientation: LANDSCAPE) {", "@media print {", "tv2.50,", "2dppx"},
	},
	{
		name:     "@supports, @layer, @container and nesting",
		source:   "@supports (display:grid) and (not (display:inline-grid)){a{x:y}}\n@supports (-webkit-touch-callout:none){a{x:y}}\n@layer base,components,utilities;\n@layer base{a{x:y}@media (min-width:1px){b{x:y}}}\n@container sidebar (min-width:400px){a{x:y}}",
		contains: []string{"@supports (display: grid) and (not (display: inline-grid)) {", "@layer base,components,utilities;", "@layer base {\n"},
	},
	{
		name: "Tailwind v4: @theme, @utility, @apply, @custom-variant, @config, @source, @import with source()",
		source: "@config '../../TailwindConfiguration.ts';\n@import 'tailwindcss' source(none);\n@source '../../app/';\n" +
			"@theme{--color-primary:#FFF;--font-sans:'Inter',sans-serif;--spacing:0.25REM;--breakpoint-xs:30rem}\n" +
			"@theme inline{--color-x:var(--x)}\n@utility tab-*{tab-size:--value(integer)}\n@utility typing-dots{@apply flex   gap-1;}\n" +
			"@custom-variant dark (&:where(.scheme-dark,.scheme-dark *));\n@custom-variant hover{&:hover{@media (hover:hover){@slot;}}}\n" +
			".btn{@apply px-4 py-2 rounded-lg hover:bg-blue-500 focus:outline-none;}",
		contains: []string{"@theme {\n", "--color-primary: #fff;", "@utility tab-* {", "@apply flex   gap-1;"},
	},
	{
		name: "@keyframes with from, to, percentages, and @font-face",
		source: "@keyframes Bounce{FROM{transform:translateY(0)}50.0%{transform:translateY(-25%)}TO{transform:none}}\n" +
			"@-webkit-keyframes spin{0%{x:y}100%{x:y}}\n" +
			"@font-face{font-family:\"Inter\";font-style:normal;font-weight:100 900;font-display:swap;src:url(\"Inter.woff2\") format(\"woff2\"),url(Inter.woff) format('woff');unicode-range:U+0000-00FF,U+0131}",
		contains: []string{"from {", "to {", "50.0% {", "font-family: 'Inter';", "unicode-range: U+0000-00FF, U+0131;"},
	},
	{
		name: "custom properties: plain, empty, json-ish, nested braces, var() fallbacks",
		source: ":root{--a:1px;--b: ;--c:{a:b};--d:  calc( 1px+2px );--E:Value;--f:var(--g,10px);--h:var(--i , );" +
			"--j:rgb(0 0 0/50%);--k:'quoted';--l:12px/1.5 Inter}\na{color:VAR(--A)}",
		contains: []string{"--a: 1px;", "--E: Value;", "var(--g, 10px)"},
	},
	{
		name: "comments: top level, in rules, between declarations, inline after declarations, in values, in selectors",
		source: "/* top */\n/* second */a{/* first */color:red;/* between */\nbackground:blue; /* trailing */\n}\n" +
			"a /* in selector */ b{x:y}\nb{margin:1px /* in value */ 2px}\n\n/* before at-rule */@media print{/* inside */}\n" +
			"c{x:y}/* after rule */\n/* last */",
		contains: []string{"/* top */\n/* second */\na {", "/* between */", "background: blue; /* trailing */", "/* last */\n"},
	},
	{
		name:     "prettier-ignore keeps the next node as written",
		source:   "/* prettier-ignore */\na{color:RED;   margin:0 0 0 0}\nb{\n/* prettier-ignore */\ncolor  :  RED;\nmargin:0}\n",
		contains: []string{"a{color:RED;   margin:0 0 0 0}", "color  :  RED;"},
	},
	{
		name: "long comma lists and space lists break at the print width",
		source: "a{transition:opacity 0.3s cubic-bezier(0.25,0.46,0.45,0.94),transform 0.3s cubic-bezier(0.25,0.46,0.45,0.94),visibility 0s linear 0.3s;" +
			"font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,'Helvetica Neue',Arial,sans-serif,'Apple Color Emoji','Segoe UI Emoji';" +
			"box-shadow:0 0 0 1px rgba(0,0,0,0.05),0 1px 2px 0 rgba(0,0,0,0.1),0 2px 4px -1px rgba(0,0,0,0.06),inset 0 1px 0 rgba(255,255,255,0.1);" +
			"grid-template-columns:[full-start] minmax(1rem,1fr) [content-start] minmax(0,60rem) [content-end] minmax(1rem,1fr) [full-end] repeat(auto-fill,minmax(10rem,1fr));" +
			"mask-image:linear-gradient(to right,transparent 0%,black 10%,black 90%,transparent 100%),linear-gradient(to bottom,transparent,black)}",
		contains: []string{"transition:\n"},
	},
	{
		name: "a long space-separated value fills its lines",
		source: "a{mask:linear-gradient(black,transparent) center center / contain no-repeat border-box content-box alpha luminance add subtract intersect exclude;" +
			"margin:var(--a-long-custom-property-name) var(--another-long-custom-property) var(--yet-another-long-property) var(--and-the-last-one-here)}",
		contains: []string{"mask: linear-gradient(black, transparent) center center / contain no-repeat border-box content-box alpha luminance\n"},
	},
	{
		name: "grid templates keep their rows on their lines",
		source: "a{grid-template-areas:\"header header\"\n\"sidebar main\";grid-template-columns:\n[full-start] 1fr\n[full-end];" +
			"grid:auto-flow / 1fr 1fr;grid-area:1/2/3/4}",
		contains: []string{"grid-template-areas:\n"},
	},
	{
		name:     "!important in every spelling",
		source:   "a{color:red!important;margin:0 ! important;padding:0 !IMPORTANT;top:0   !important}",
		contains: []string{"color: red !important;", "margin: 0 !important;", "padding: 0 !important;"},
	},
	{
		name: "numbers and units: zeros, signs, exponents, unit case, unknown units",
		source: "a{margin:0.50px +.5em -0.0PX 10.0E3px;width:calc(100% - 10PX);line-height:1.50;transform:rotate(45DEG) scale(1.0);" +
			"top:00.5Q;x:1e+3;y:1E-03;z:10Hz 2KHZ;opacity:.0;flex:1 1 0%;w:10furlongs;h:+.5;q:1.}",
		contains: []string{"0.5px", "+0.5em", "rotate(45deg)", "line-height: 1.5;"},
	},
	{
		name: "strings and quotes: preference, escapes, mixed quotes",
		source: "a{content:\"double\";b:'single';c:\"it's\";d:'say \"hi\"';e:\"\\\"escaped\\\"\";f:'\\'esc\\'';" +
			"font-family:\"Helvetica Neue\",Arial}\n[title=\"x\"]{x:y}\na::after{content:\"\\201C\"}",
		contains: []string{"content: 'double';", "c: \"it's\";"},
	},
	{
		name: "url(): unquoted, quoted, data URIs, with spaces, in @import",
		source: "a{background:url(image.png);b:url( 'image.png' );c:url(\"image.png\");d:url(data:image/svg+xml;base64,AAAA);" +
			"e:url(  spaced.png  ) no-repeat,url(two.png)}\n@import url(\"foo.css\") screen and (orientation:landscape);\n@import 'bar.css';\n@import \"baz.css\" layer(base);",
		contains: []string{"url(image.png)", "@import url('foo.css') screen and (orientation: landscape);"},
	},
	{
		name: "colors, functions, operators, wide keywords",
		source: "a{color:#FFF;background:#AbCdEf;border-color:RGB(0,0,0);x:INHERIT;y:Initial;z:hsl(120DEG 100% 50%/0.5);" +
			"w:calc(1px+2px);v:calc( (100% - 2*10px)/3 );u:min(10px,5vw) max(1px , 2px);t:clamp(1rem,2.5vw,2rem)}",
		contains: []string{"color: #fff;", "x: inherit;"},
	},
	{
		name:     "at-rules without blocks and with odd params",
		source:   "@charset \"utf-8\";\n@namespace svg url(http://www.w3.org/2000/svg);\n@page :first{margin:1in}\n@custom-media --small-viewport (max-width:30em);\n@media (--small-viewport){a{x:y}}",
		contains: []string{"@charset \"utf-8\";", "@page :first {"},
	},
	{
		name:     "CSS nesting with &",
		source:   ".a{color:red;&:hover{color:blue}& .b{x:y}.c &{x:y}> .d{x:y}@media (min-width:1px){x:y}}",
		contains: []string{"&:hover {", "& .b {", "> .d {"},
	},
	{
		name:     "blank lines between nodes are kept, runs collapse to one",
		source:   "a{x:y}\n\n\n\nb{x:y;\n\n\n\nz:w}\n@media print{\n\na{x:y}\n\n\nb{x:y}\n}",
		contains: []string{"}\n\nb {", "x: y;\n\n"},
	},
}

// formatOptionSets are the options every case runs under: ahra's defaults (printWidth 120, tabWidth 4,
// singleQuote), printWidth 80 with Prettier's tabWidth 2, double quotes, and tabs.
func formatOptionSets() map[string]prettier.Options {
	narrow := prettier.DefaultOptions()
	narrow.PrintWidth = 80
	narrow.TabWidth = 2
	doubleQuotes := prettier.DefaultOptions()
	doubleQuotes.SingleQuote = false
	tabs := prettier.DefaultOptions()
	tabs.UseTabs = true
	return map[string]prettier.Options{
		"defaults":          prettier.DefaultOptions(),
		"printWidth 80":     narrow,
		"singleQuote false": doubleQuotes,
		"useTabs":           tabs,
	}
}

func TestFormatMatchesTheFork(t *testing.T) {
	for optionsName, options := range formatOptionSets() {
		oracle, err := prettier.New(options)
		if err != nil {
			t.Fatal(err)
		}
		for _, testCase := range formatCases {
			expected, err := oracle.Format("Probe.css", testCase.source)
			if err != nil {
				t.Fatalf("%s (%s): the oracle failed: %v", testCase.name, optionsName, err)
			}
			if expected == testCase.source {
				t.Fatalf("%s (%s): the oracle left the snippet unchanged, so an identity printer would pass", testCase.name, optionsName)
			}
			if optionsName == "defaults" {
				for _, substring := range testCase.contains {
					if !strings.Contains(expected, substring) {
						t.Errorf("%s (%s): the fork's output lacks %q:\n%s", testCase.name, optionsName, substring, expected)
					}
				}
			}
			actual, err := Format(testCase.source, options)
			if err != nil {
				t.Errorf("%s (%s): %v", testCase.name, optionsName, err)
				continue
			}
			if actual != expected {
				t.Errorf("%s (%s):\n--- fork\n%s--- native\n%s", testCase.name, optionsName, expected, actual)
			}
		}
	}
}

// The option sets have to change the output somewhere, or running under them proves nothing.
func TestOptionSetsChangeTheOutput(t *testing.T) {
	for optionsName, options := range formatOptionSets() {
		if optionsName == "defaults" {
			continue
		}
		changed := false
		for _, testCase := range formatCases {
			withDefaults, defaultsErr := Format(testCase.source, prettier.DefaultOptions())
			withOptions, optionsErr := Format(testCase.source, options)
			if defaultsErr == nil && optionsErr == nil && withDefaults != withOptions {
				changed = true
				break
			}
		}
		if !changed {
			t.Errorf("%s printed every case as the defaults do", optionsName)
		}
	}
}

// What the fork refuses, Format refuses, as a syntax error.
func TestFormatRefusesWhatTheForkRefuses(t *testing.T) {
	oracle, err := prettier.New(prettier.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"a { color: red;\n", "a { color: \"unclosed; }\n", "}\n"} {
		if _, err := oracle.Format("Probe.css", source); err == nil {
			t.Fatalf("the oracle accepted %q", source)
		}
		if formatted, err := Format(source, prettier.DefaultOptions()); err == nil {
			t.Errorf("%q was printed instead of refused:\n%s", source, formatted)
		}
	}
}

// PrintToDoc is the same doc without its trailing hardline, which an embedding printer lays out.
func TestPrintToDocIsFormatWithoutTheTrailingHardline(t *testing.T) {
	options := prettier.DefaultOptions()
	for _, testCase := range formatCases {
		formatted, err := Format(testCase.source, options)
		if err != nil {
			t.Fatalf("%s: %v", testCase.name, err)
		}
		document, err := PrintToDoc(testCase.source, options)
		if err != nil {
			t.Fatalf("%s: %v", testCase.name, err)
		}
		printed := doc.Print(document, doc.Options{PrintWidth: options.PrintWidth, TabWidth: options.TabWidth, UseTabs: options.UseTabs})
		if printed != strings.TrimSuffix(formatted, "\n") {
			t.Errorf("%s:\n--- Format\n%s--- PrintToDoc\n%s", testCase.name, formatted, printed)
		}
	}
}

// The corpus is the 26 CSS files the brief names, read from the machine's projects. They are not part of
// this repository, so the test runs only when COHERE_CSS_CORPUS names the directories to search,
// separated by the path list separator, and fails when that finds nothing. Each file runs as written and
// with every line's indentation stripped: the files are already formatted, so only the stripped copy
// makes the oracle rewrite anything.
func TestCorpusMatchesTheFork(t *testing.T) {
	roots := os.Getenv("COHERE_CSS_CORPUS")
	if roots == "" {
		t.Skip("COHERE_CSS_CORPUS is not set")
	}
	var files []string
	for _, root := range filepath.SplitList(roots) {
		_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if entry.IsDir() && (entry.Name() == "node_modules" || entry.Name() == ".git" || entry.Name() == "conversations") {
				return filepath.SkipDir
			}
			if !entry.IsDir() && strings.HasSuffix(path, ".css") &&
				(strings.Contains(path, "theme/styles") || strings.HasSuffix(path, "OsKingdomGraph.css") || strings.HasSuffix(path, "fonts/inter/inter.css")) {
				files = append(files, path)
			}
			return nil
		})
	}
	if len(files) == 0 {
		t.Fatalf("no corpus files under %s", roots)
	}
	rewritten := 0
	for optionsName, options := range formatOptionSets() {
		oracle, err := prettier.New(options)
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			source, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			for _, variant := range []string{string(source), stripIndentation(string(source))} {
				expected, err := oracle.Format("Probe.css", variant)
				if err != nil {
					t.Fatalf("%s (%s): the oracle failed: %v", file, optionsName, err)
				}
				if expected != variant {
					rewritten++
				}
				actual, err := Format(variant, options)
				if err != nil {
					t.Errorf("%s (%s): %v", file, optionsName, err)
					continue
				}
				if actual != expected {
					t.Errorf("%s (%s): differs from the fork\n%s", file, optionsName, firstDifference(expected, actual))
				}
			}
		}
	}
	if rewritten < len(files)*len(formatOptionSets()) {
		t.Errorf("the oracle rewrote only %d of %d inputs, so the corpus barely tests the printer", rewritten, 2*len(files)*len(formatOptionSets()))
	}
	t.Logf("%d corpus files, two variants each, under %d option sets; the oracle rewrote %d", len(files), len(formatOptionSets()), rewritten)
}

// stripIndentation removes every line's leading spaces and tabs.
func stripIndentation(source string) string {
	lines := strings.Split(source, "\n")
	for index, line := range lines {
		lines[index] = strings.TrimLeft(line, " \t")
	}
	return strings.Join(lines, "\n")
}

// Prettier's own CSS fixtures, tests/format/css in the fork, are the widest set of edge cases there is:
// 157 files written to exercise the printer and parser. The fork's source tree is not part of this
// repository, so the test runs when it is at COHERE_PRETTIER_FORK or ~/Projects/system/prettier, and
// skips otherwise. Two kinds of file are set aside by name, each for a stated reason, and must still
// behave as described; everything else must match the fork byte for byte.
func TestPrettierFixturesMatchTheFork(t *testing.T) {
	root := os.Getenv("COHERE_PRETTIER_FORK")
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Skip("no home directory to find the Prettier fork in")
		}
		root = filepath.Join(home, "Projects", "system", "prettier")
	}
	fixtures := filepath.Join(root, "tests", "format", "css")
	if _, err := os.Stat(fixtures); err != nil {
		t.Skipf("the Prettier fork's CSS fixtures are not at %s; set COHERE_PRETTIER_FORK", fixtures)
	}
	var files []string
	_ = filepath.WalkDir(fixtures, func(path string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() && strings.HasSuffix(path, ".css") {
			files = append(files, path)
		}
		return nil
	})

	for optionsName, options := range formatOptionSets() {
		oracle, err := prettier.New(options)
		if err != nil {
			t.Fatal(err)
		}
		compared, refusedByBoth := 0, 0
		for _, file := range files {
			source, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			text := string(source)
			actual, nativeErr := Format(text, options)

			// A byte order mark is the shared layer's to strip, before any printer runs.
			if strings.HasPrefix(text, "\xef\xbb\xbf") {
				continue
			}
			// yaml front matter is formatted by the yaml printer, which Format cannot reach, so it refuses.
			if strings.HasPrefix(text, "---") && strings.Contains(text[3:], "\n---") {
				if frontMatterRoot, parseErr := parse(text); parseErr == nil &&
					frontMatterRoot.Child("frontMatter").String("language") == "yaml" &&
					strings.TrimSpace(frontMatterRoot.Child("frontMatter").String("value")) != "" {
					if nativeErr == nil {
						t.Errorf("%s (%s): yaml front matter was printed instead of refused", file, optionsName)
					}
					continue
				}
			}

			expected, oracleErr := oracle.Format("Probe.css", text)
			switch {
			case oracleErr != nil && nativeErr != nil:
				refusedByBoth++
			case oracleErr != nil:
				t.Errorf("%s (%s): the fork refused it (%v) and Format printed it", file, optionsName, oracleErr)
			case nativeErr != nil:
				t.Errorf("%s (%s): %v", file, optionsName, nativeErr)
			default:
				compared++
				if actual != expected {
					t.Errorf("%s (%s): differs from the fork\n%s", file, optionsName, firstDifference(expected, actual))
				}
			}
		}
		// The refusal threshold: an oracle or a printer that refuses everything cannot read as agreement.
		if compared < 130 {
			t.Errorf("%s: only %d fixtures were compared (%d refused by both)", optionsName, compared, refusedByBoth)
		}
		t.Logf("%s: %d fixtures compared, %d refused by both", optionsName, compared, refusedByBoth)
	}
}

// firstDifference shows the first line where two outputs part.
func firstDifference(expected string, actual string) string {
	expectedLines := strings.Split(expected, "\n")
	actualLines := strings.Split(actual, "\n")
	for index := 0; index < max(len(expectedLines), len(actualLines)); index++ {
		var expectedLine, actualLine string
		if index < len(expectedLines) {
			expectedLine = expectedLines[index]
		}
		if index < len(actualLines) {
			actualLine = actualLines[index]
		}
		if expectedLine != actualLine {
			return fmt.Sprintf("line %d\n--- fork\n%s\n--- native\n%s", index+1, expectedLine, actualLine)
		}
	}
	return "(no differing line)"
}
