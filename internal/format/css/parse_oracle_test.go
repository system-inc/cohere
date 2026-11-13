package css

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/system-inc/cohere/internal/corpus"
	"github.com/system-inc/cohere/internal/format/estree"
	"github.com/system-inc/cohere/internal/json_value"
	"github.com/system-inc/cohere/internal/needs"
)

// glueParseScript is the oracle: the fork's own parse with parser "css" (prettier.__debug.parse, which
// runs Prettier's core normalization and then parsers.css.parse from src/language-css/parser-postcss.js),
// dumped whole: every own enumerable key of every object, sorted, with parent and source.input left out.
// undefined and null both dump as null, so a key written with undefined (loc.js's `??=`) still counts as
// present. Offsets are UTF-16 there; the Go side converts its byte offsets to match.
const glueParseScript = `
import * as prettier from "./src/index.js";
function dump(v) {
  if (v === undefined || v === null) return null;
  if (Array.isArray(v)) return v.map(dump);
  if (typeof v !== "object") return v;
  const o = {};
  for (const k of Object.keys(v).sort()) { if (k === "parent" || k === "input") continue; o[k] = dump(v[k]); }
  return o;
}
let input = ""; process.stdin.setEncoding("utf8"); process.stdin.on("data", (c) => input += c);
process.stdin.on("end", async () => {
  for (const line of input.split("\n").filter(Boolean)) {
    const { name, text } = JSON.parse(line);
    try {
      const { ast } = await prettier.__debug.parse(text, { parser: "css" });
      process.stdout.write(JSON.stringify({ name, ast: dump(ast) }) + "\n");
    } catch (e) {
      process.stdout.write(JSON.stringify({ name, error: String(e?.message ?? e).split("\n")[0] }) + "\n");
    }
  }
});
`

// glueParseFixture is an inline input; malformed says which way the fork must go on it.
type glueParseFixture struct {
	name      string
	text      string
	malformed bool
}

var glueParseFixtures = []glueParseFixture{
	// Rules, selectors and declarations.
	{name: "rule and declarations", text: "a { color: red; background: blue }\nb{c:d}"},
	{
		name: "selector forms",
		text: "a > b + c ~ d e, .x#y[z=\"1\" i]:hover::before, *|*, ns|a, :not(.a, .b) :is(a) :where(b), a:nth-child(2n + 1 of .x), :root {}",
	},
	{name: "css nesting", text: "a { &:hover { b: c } .d & { e: f } & > g { h: i } }"},
	{name: "selector with a comment", text: "a /* c */ b { x: y }"},
	{name: "selector with a line comment", text: "a // c\nb { x: y }"},
	{name: "selector with slashes in quotes", text: "a[href=\"//x\"], a:not([x='/*']) { x: y }"},
	{name: "comment between selector and brace", text: "a /* c */ { x: y }\nb\n/* d */\n{ }"},
	{name: "escaped quotes in a selector", text: "a[title=\"a\\\"b\"] {}"},
	{name: "empty selector", text: "{ a: b }"},
	{name: "declaration at the root", text: "color: red;\na {}"},
	{name: "semicolons and hacks", text: "a{;b:c;;*zoom:1;_height:1px}"},
	{name: "declaration value comments", text: "a { margin: 1px /* c */ 2px; color: red/* x */; padding:/* y */0 }"},
	{name: "important", text: "a { color: red !important; margin: 0 ! important; padding: 0!IMPORTANT }"},
	{
		name: "scss directives read by the css parser",
		text: "a { x: 1 !default; y: 2!global; z: 3 !default !global; w: 4 !default\n ; v: 5 !defaults }",
	},
	{
		name: "progid",
		text: "a { filter: progid:DXImageTransform.Microsoft.gradient(startColorstr='#000', endColorstr='#fff'); }",
	},
	{
		name: "multi-line values",
		text: "a {\n  grid-template-columns:\n    [full-start] minmax(1em, 1fr)\n    [main-start] minmax(0, 40em);\n  box-shadow:\n    0 0 0 1px red,\n    0 1px 2px blue;\n}",
	},

	// Custom properties.
	{
		name: "custom properties",
		text: ":root { --a: 1px; --b: { color: red; margin: 0 }; --c: {}; --d:{a:b}; --e: ; --f: {/* c */ a: b}; --g: [x] }",
	},
	{name: "custom property block nested in rules", text: "a { b { --x: { c: d } } }\n:root{--y:{}}"},
	{name: "custom property that is not a block", text: ":root { --x: {a} b }", malformed: true},
	{name: "custom property block that does not parse", text: ":root { --x: {a} }", malformed: true},
	{name: "custom property block with a comment that does not parse", text: ":root { --x: {/* c */ a} }"},

	// At-rules.
	{
		name: "import and namespace",
		text: "@charset \"utf-8\";\n@import url(foo.css) screen;\n@import \"bar.css\" layer(x);\n@IMPORT url(x);\n@import url(\"a.css\") supports(display: grid) screen;\n@namespace svg url(http://www.w3.org/2000/svg);",
	},
	{
		name: "font face",
		text: "@font-face { font-family: X; src: url(a.woff2) format(\"woff2\"), url('b.woff'); unicode-range: U+0000-00FF, U+0131, u+4??; font-display: swap }",
	},
	{
		name: "page and layer",
		text: "@page :first { margin: 1in }\n@layer base, components;\n@layer base { a { b: c } }",
	},
	{
		name: "media",
		text: "@media screen and (min-width: 100px), print and (orientation: landscape) { a { b: c } }\n@media (100px <= width <= 200px) {}\n@custom-media --small (max-width: 30em);\n@media not all and (monochrome) {}\n@MEDIA screen {}\n@media (min-width:100px) and (max-width:200px){}",
	},
	{name: "media with interpolation", text: "@media screen, \"#{\" { a { b: c } }"},
	{name: "media interpolation that postcss refuses", text: "@media #{$q} { a { b: c } }", malformed: true},
	{name: "media with a comment", text: "@media /* c */ screen { }\n@media screen /* d */ { }"},
	{
		name: "supports",
		text: "@supports (display: grid) and (not (display: inline-grid)) { a { b: c } }\n@supports selector(a > b) { }\n@supports not selector(:is(a, b)) {}\n@Supports (x: y) {}",
	},
	{
		name: "custom selector",
		text: "@custom-selector :--heading h1, h2, h3;\n@custom-selector :--enter  :hover,:focus ;",
	},
	{name: "custom selector without a name", text: "@custom-selector foo;", malformed: true},
	{
		name: "extend nest at-root warn error",
		text: "a { @extend .b; @nest & > c { d: e } }\n@at-root (without: media) { }\n@at-root .x { }\n@at-root (with: rule) {}\n@warn \"x\";\n@error \"y\";",
	},
	{
		name: "scss-like directives",
		text: "@include foo (1, 2);\n@mixin bar ($a...) {}\n@include baz($args ...);\n@if $a == 1 { }\n@else if ($b) { }\n@each $x in a, b { }\n@function f($a) { @return $a; }\n@debug \"x\";\n@define-mixin m $a { }\n@add-mixin m 1;",
	},
	{
		name: "tailwind",
		text: "@import \"tailwindcss\";\n@theme { --color-x: oklch(0.5 0.1 200); --font-sans: \"Inter\", sans-serif; --spacing: 0.25rem }\n@theme inline { --x: var(--y) }\n@utility tab-4 { tab-size: 4; }\n@utility scroll-* { --x: --value(--spacing-*) }\n@custom-variant dark (&:where(.dark, .dark *));\n@layer components { .btn { @apply flex items-center px-4 hover:bg-red-500; } }\n@variant hover { a { b: c } }\n@source \"../node_modules\";\n@plugin \"x\";\n@config \"./t.js\";",
	},
	{
		name: "keyframes",
		text: "@keyframes spin { from { transform: rotate(0deg) } 50% { opacity: .5 } to { transform: rotate(360deg) } }",
	},
	{name: "at-rule without params", text: "a { @foo; }\n@bar;\n@baz { }"},
	{name: "at-rule at the end of the file", text: "a {}\n@foo"},
	{name: "at-rule with a colon", text: "@foo:bar;\n@baz :qux;"},
	{name: "unknown at-rule with params", text: "@foo bar baz { a: b }\n@foo   spaced   ;"},

	// Values.
	{
		name: "value words and numbers",
		text: "a { margin: -1px -2px --3px; b: --x; c: -; d: --; e: -webkit-calc(1px); f: 10PX 1.5e-3em +.5 1e3 +2 -.5 }",
	},
	{
		name: "value functions",
		text: "a { width: calc(100% - (2 * var(--x, 10px))); transition: all .3s cubic-bezier(.1,.2,.3,.4); color: rgb(0 0 0 / 50%); x: a,b , c; y: f(a, /* c */); z: f(1, /* c */) }",
	},
	{
		name: "value strings and operators",
		text: "a { grid-template-areas: \"a b\" \"c d\"; font: 12px/1.5 a, b; color: #fff; content: \"\\201C\"; w: @foo d; v: U+26; filter: alpha(opacity: 50) }",
	},
	{
		name: "url forms",
		text: "a { b: url(); c: url( x.png ); d: url(\"q.png\"); e: url(a b); f: url(var(--x)); g: url(data:image/png;base64,AAA=); i: URL(x); j: url(a, b) }",
	},
	{
		name:      "url with an escaped parenthesis, which Prettier cannot balance",
		text:      "a { h: url(a\\)) }",
		malformed: true,
	},
	{name: "unbalanced value", text: "a { b: f(1; c: 1) }"},
	{name: "value with braces that postcss refuses", text: "a { b: { c } }", malformed: true},
	{
		name: "value with interpolation, which only a custom property lets through",
		text: ":root { --d: a #{e} f; --f: url(#{g}) url(x#{h}y); --g: url(\"a\" #{b}); --h: url(f(1) x#{y}) }",
	},
	{name: "empty comma groups", text: "a { b: f(,a); c: f(a,,b); d: f(a,); e: (,) }"},

	// Front matter.
	{name: "yaml front matter", text: "---\ntitle: x\n---\na { b: c }"},
	{name: "toml front matter", text: "+++\na = 1\n+++\n\na{}"},
	{name: "front matter with a custom property after it", text: "---\nx: y\n---\n:root { --a: { b: c } }"},

	// Text shapes.
	{name: "empty", text: ""},
	{name: "whitespace only", text: "  \n\t\n"},
	{name: "comment only", text: "/* a */"},
	{name: "comments around rules", text: "/* a */\na { /* b */ c: d; /* e */ }\n/* f */"},
	{
		name: "non-ASCII",
		text: "a::before { content: \"\xc3\xa9\xe2\x86\x92\xf0\x9f\x98\x80\"; }\n.\xc3\xa9\xf0\x9f\x98\x80 > b { font-family: \"\xc3\x9cn\xc3\xafcode\", \xf0\x9f\x98\x80; margin: 1px \xc3\xa9 2px }\n/* \xe6\x97\xa5\xe6\x9c\xac */ b { c: url(\xc3\xa9.png) }\n@media (min-width: 1px) and \xc3\xa9 { }",
	},
	{name: "non-ASCII before a custom property block", text: "/* \xf0\x9f\x98\x80 */\n:root { --x: { a: \xc3\xa9 } }"},

	// Malformed: postcss refuses each.
	{name: "unclosed block", text: "a {", malformed: true},
	{name: "unclosed string", text: "a { b: \"c }", malformed: true},
	{name: "unexpected close", text: "a { } }", malformed: true},
	{name: "unknown word", text: "a { b }", malformed: true},
	{name: "unclosed at-rule block", text: "@media screen {", malformed: true},
}

// TestParseAgreesWithPrettierCssParser is the glue's acceptance test: for every .css file in the
// corpora, and every inline fixture, the Go tree must equal what the fork's parsers.css.parse returns,
// key for key, Prettier's source.startOffset and endOffset included, and a refusal must be a refusal on
// both sides. Off unless COHERE_PRETTIER_FORK names the fork and COHERE_CSS_CORPORA lists files or
// directories to walk (colon-separated, node_modules skipped).
//
// On a text with a non-ASCII character, sourceIndex is left out on both sides: it is relative to a
// value, selector or params string, not to the file, and only absolute offsets are converted from bytes
// here. startOffset and endOffset, which upstream derives from it, are still compared.
//
// A corpus file the fork cannot parse is counted, and the test fails if that count passes a tenth of the
// corpus: an oracle that rejects everything would otherwise read as perfect agreement. The fixtures each
// say which way the fork must go.
func TestParseAgreesWithPrettierCssParser(test *testing.T) {
	test.Parallel()
	corpora := needs.OptIn(
		test,
		"COHERE_CSS_CORPORA",
		"a differential of the parser glue against the fork over a corpus",
	)
	root := corpus.PrettierFork.Root(test)

	type input struct {
		Name string `json:"name"`
		Text string `json:"text"`
	}
	var inputs []input
	malformed := map[string]bool{}
	corpusFiles := 0
	seen := map[string]bool{}
	for _, corpus := range strings.Split(corpora, ":") {
		corpus = strings.TrimSpace(corpus)
		if strings.HasPrefix(corpus, "~/") {
			home, homeError := os.UserHomeDir()
			if homeError != nil {
				test.Fatalf("expanding %s: %v", corpus, homeError)
			}
			corpus = filepath.Join(home, corpus[2:])
		}
		walkError := filepath.WalkDir(corpus, func(path string, entry fs.DirEntry, pathError error) error {
			if pathError != nil {
				return pathError
			}
			if entry.IsDir() && (entry.Name() == "node_modules" || entry.Name() == ".git") {
				return filepath.SkipDir
			}
			if entry.IsDir() || filepath.Ext(path) != ".css" || seen[path] {
				return nil
			}
			seen[path] = true
			source, readError := os.ReadFile(path)
			if readError != nil {
				return readError
			}
			if !utf8.Valid(source) {
				return fmt.Errorf("%s is not UTF-8, which the comparison cannot carry through JSON", path)
			}
			inputs = append(inputs, input{Name: path, Text: string(source)})
			corpusFiles++
			return nil
		})
		if walkError != nil {
			test.Fatalf("walking %s: %v", corpus, walkError)
		}
	}
	if corpusFiles == 0 {
		test.Fatalf("no .css files under %s", corpora)
	}
	for _, fixture := range glueParseFixtures {
		name := "fixture: " + fixture.name
		inputs = append(inputs, input{Name: name, Text: fixture.text})
		malformed[name] = fixture.malformed
	}

	var stdin bytes.Buffer
	for _, each := range inputs {
		encoded, encodeError := json.Marshal(each)
		if encodeError != nil {
			test.Fatal(encodeError)
		}
		stdin.Write(encoded)
		stdin.WriteByte('\n')
	}
	command := exec.Command("node", "--input-type=module", "-e", glueParseScript)
	command.Dir = root
	command.Stdin = &stdin
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if runError := command.Run(); runError != nil {
		test.Fatalf("node: %v\n%s", runError, stderr.String())
	}
	oracle := map[string]json_value.Object{}
	for _, line := range strings.Split(stdout.String(), "\n") {
		if line == "" {
			continue
		}
		parsed, parseError := json_value.Parse([]byte(line))
		if parseError != nil {
			test.Fatal(parseError)
		}
		record, isObject := parsed.(json_value.Object)
		if !isObject {
			test.Fatalf("the oracle printed a record that is not an object: %s", line)
		}
		name, isString := record["name"].(json_value.String)
		if !isString {
			test.Fatalf("the oracle printed a record without a name: %s", line)
		}
		oracle[string(name)] = record
	}

	identical, refusedByBoth, corpusRefused, different, nullEnds := 0, 0, 0, 0, 0
	fail := func(format string, arguments ...any) {
		different++
		if different <= 20 {
			test.Errorf(format, arguments...)
		}
	}
	for _, each := range inputs {
		want, present := oracle[each.Name]
		if !present {
			test.Fatalf("the oracle printed nothing for %s", each.Name)
		}
		wantError, oracleRefused := want["error"].(json_value.String)
		if expectMalformed, isFixture := malformed[each.Name]; isFixture && expectMalformed != oracleRefused {
			fail("%s: the fixture says malformed=%v and the fork says %v", each.Name, expectMalformed, want["error"])
			continue
		}
		text := normalizeGlueInput(each.Text)
		got, dumpGlueParseError := dumpGlueParse(text)
		switch {
		case oracleRefused && dumpGlueParseError != nil:
			refusedByBoth++
			if _, isFixture := malformed[each.Name]; !isFixture {
				corpusRefused++
			}
		case oracleRefused:
			fail("%s: the fork refuses it (%s) and Go parsed it", each.Name, wantError)
		case dumpGlueParseError != nil:
			fail("%s: the fork parses it and Go refused: %v", each.Name, dumpGlueParseError)
		default:
			wantTree := want["ast"]
			if !isAscii(text) {
				wantTree = withoutKey(wantTree, "sourceIndex")
				got = withoutKey(got, "sourceIndex")
			}
			if difference := firstGlueDifference(wantTree, got, "ast"); difference != "" {
				fail("%s: %s", each.Name, difference)
				continue
			}
			nullEnds += countNullEndOffsets(wantTree)
			identical++
		}
	}
	test.Logf(
		"%d compared (%d corpus files, %d fixtures): %d identical, %d refused by both, %d different; %d corpus files refused; %d nodes with a null endOffset",
		len(inputs),
		corpusFiles,
		len(glueParseFixtures),
		identical,
		refusedByBoth,
		different,
		corpusRefused,
		nullEnds,
	)
	if corpusRefused > corpusFiles/10 {
		test.Errorf(
			"the fork refused %d of %d corpus files; the comparison is thinner than it looks",
			corpusRefused,
			corpusFiles,
		)
	}
}

// normalizeGlueInput is what Prettier's core does to the text before the parser sees it
// (normalizeInputAndOptions): a byte order mark dropped, and \r\n and \r turned into \n.
func normalizeGlueInput(text string) string {
	withoutByteOrderMark := strings.TrimPrefix(text, "\xef\xbb\xbf")
	withoutCarriageReturnLineFeeds := strings.ReplaceAll(withoutByteOrderMark, "\r\n", "\n")
	return strings.ReplaceAll(withoutCarriageReturnLineFeeds, "\r", "\n")
}

// dumpGlueParse parses one text and renders it the way the oracle script does, as the JSON value the
// script prints.
func dumpGlueParse(text string) (json_value.Value, error) {
	root, parseError := parse(text)
	if parseError != nil {
		return nil, parseError
	}
	return dumpGlueValue(root, utf16OffsetTable(text)), nil
}

// dumpGlueValue renders a value in the oracle's shape, as the JSON value json.Marshal would have written
// for it. The absolute offsets (source.startOffset and endOffset, postcss's source.start.offset and
// end.offset, and the front matter's start.index and end.index) are converted from bytes to UTF-16. Each
// node's Range must agree with its source offsets. A float64 JSON cannot carry, which json.Marshal would
// refuse, becomes a description no oracle prints, so the comparison still fails on it.
func dumpGlueValue(value estree.Value, toUtf16 func(int) json_value.Value) json_value.Value {
	switch typed := value.(type) {
	case nil:
		return json_value.Null{}
	case *estree.Node:
		if typed == nil {
			return json_value.Null{}
		}
		result := json_value.Object{}
		if typed.Type() != "" {
			result["type"] = textThroughJson(typed.Type())
		}
		for _, key := range typed.Keys() {
			child := typed.Get(key)
			switch {
			case key == "source":
				result[key] = dumpGlueSource(child, toUtf16)
			case typed.Type() == "front-matter" && (key == "start" || key == "end"):
				dumped := dumpGlueValue(child, toUtf16)
				position, isPosition := dumped.(json_value.Object)
				if !isPosition {
					// Not an object: kept as dumped, so the comparison with the fork names it.
					result[key] = dumped
					continue
				}
				// The index is converted where the value dumped is an Integer, which only the Go value tells.
				var index estree.Value
				switch original := child.(type) {
				case estree.Record:
					index = original["index"]
				case *estree.Node:
					index = original.Get("index")
				}
				if integer, isInteger := index.(estree.Integer); isInteger {
					position["index"] = toUtf16(int(integer))
				}
				result[key] = position
			default:
				result[key] = dumpGlueValue(child, toUtf16)
			}
		}
		if source := sourceOf(typed); source != nil {
			if typed.Range != [2]int{locStart(typed), locEnd(typed)} {
				result["<range>"] = json_value.String(
					fmt.Sprintf("Range %v disagrees with the source offsets", typed.Range),
				)
			}
		}
		return result
	case estree.Nodes:
		if typed == nil {
			return json_value.String("a nil list, which no JavaScript array is")
		}
		result := make(json_value.Array, len(typed))
		for index, child := range typed {
			result[index] = dumpGlueValue(child, toUtf16)
		}
		return result
	case estree.Values:
		result := make(json_value.Array, len(typed))
		for index, child := range typed {
			result[index] = dumpGlueValue(child, toUtf16)
		}
		return result
	case estree.Record:
		result := json_value.Object{}
		for key, child := range typed {
			result[key] = dumpGlueValue(child, toUtf16)
		}
		return result
	case estree.Number:
		// JSON.stringify writes NaN (postcss-values-parser's column for a paren at the start) as null.
		if math.IsNaN(float64(typed)) {
			return json_value.Null{}
		}
		if math.IsInf(float64(typed), 0) {
			return json_value.String(fmt.Sprintf("a float64 of %v, which JSON cannot carry", float64(typed)))
		}
		return json_value.Number(strconv.FormatFloat(float64(typed), 'g', -1, 64))
	case estree.String:
		return textThroughJson(string(typed))
	case estree.Boolean:
		return json_value.Boolean(typed)
	case estree.Integer:
		return json_value.Number(strconv.Itoa(int(typed)))
	default:
		return json_value.String(fmt.Sprintf("an unexpected %T", typed))
	}
}

// textThroughJson is text as it comes back from json.Marshal and json.Unmarshal, the trip the whole tree
// used to make: each byte of invalid UTF-8 becomes U+FFFD, and every other byte is kept. A string sliced
// at a byte offset inside a character holds such bytes. Keys are left as they are: they are the
// libraries' own ASCII property names.
func textThroughJson(text string) json_value.String {
	if utf8.ValidString(text) {
		return json_value.String(text)
	}
	var builder strings.Builder
	for index := 0; index < len(text); {
		character, size := utf8.DecodeRuneInString(text[index:])
		if character == utf8.RuneError && size == 1 {
			builder.WriteRune(utf8.RuneError)
		} else {
			builder.WriteString(text[index : index+size])
		}
		index += size
	}
	return json_value.String(builder.String())
}

func dumpGlueSource(value estree.Value, toUtf16 func(int) json_value.Value) json_value.Value {
	source, isMap := value.(estree.Record)
	if !isMap {
		return dumpGlueValue(value, toUtf16)
	}
	result := json_value.Object{}
	for key, child := range source {
		switch key {
		case "startOffset", "endOffset":
			if offset, isNumber := child.(estree.Integer); isNumber {
				result[key] = toUtf16(int(offset))
				continue
			}
			result[key] = dumpGlueValue(child, toUtf16)
		case "start", "end":
			position, isPosition := child.(estree.Record)
			if !isPosition {
				result[key] = dumpGlueValue(child, toUtf16)
				continue
			}
			dumped := dumpGlueValue(position, toUtf16)
			converted, isConverted := dumped.(json_value.Object)
			if !isConverted {
				// Not an object: kept as dumped, so the comparison with the fork names it.
				result[key] = dumped
				continue
			}
			if offset, isNumber := position["offset"].(estree.Integer); isNumber {
				converted["offset"] = toUtf16(int(offset))
			}
			result[key] = converted
		default:
			result[key] = dumpGlueValue(child, toUtf16)
		}
	}
	return result
}

// utf16OffsetTable maps a byte offset into text to its UTF-16 index. An offset inside a character, or
// past the end, is reported as such, so it cannot pass for a real position.
func utf16OffsetTable(text string) func(int) json_value.Value {
	offsets := make([]int, len(text)+1)
	starts := make([]bool, len(text)+1)
	index := 0
	for position, character := range text {
		offsets[position] = index
		starts[position] = true
		index += len(utf16.Encode([]rune{character}))
	}
	offsets[len(text)] = index
	starts[len(text)] = true
	return func(byteOffset int) json_value.Value {
		if byteOffset < 0 || byteOffset > len(text) {
			return json_value.String(fmt.Sprintf("byte offset %d outside the text", byteOffset))
		}
		if !starts[byteOffset] {
			return json_value.String(fmt.Sprintf("byte offset %d inside a character", byteOffset))
		}
		return json_value.Number(strconv.Itoa(offsets[byteOffset]))
	}
}

func isAscii(text string) bool {
	for index := 0; index < len(text); index++ {
		if text[index] >= 0x80 {
			return false
		}
	}
	return true
}

// withoutKey drops a key everywhere in a JSON value.
func withoutKey(value json_value.Value, key string) json_value.Value {
	switch typed := value.(type) {
	case json_value.Object:
		result := json_value.Object{}
		for each, child := range typed {
			if each != key {
				result[each] = withoutKey(child, key)
			}
		}
		return result
	case json_value.Array:
		result := make(json_value.Array, len(typed))
		for index, child := range typed {
			result[index] = withoutKey(child, key)
		}
		return result
	}
	return value
}

// countNullEndOffsets counts sources whose endOffset upstream left null.
func countNullEndOffsets(value json_value.Value) int {
	count := 0
	switch typed := value.(type) {
	case json_value.Object:
		if source, isObject := typed["source"].(json_value.Object); isObject {
			if _, isNull := source["endOffset"].(json_value.Null); isNull {
				count++
			}
		}
		for _, child := range typed {
			count += countNullEndOffsets(child)
		}
	case json_value.Array:
		for _, child := range typed {
			count += countNullEndOffsets(child)
		}
	}
	return count
}

// firstGlueDifference names the first path where two JSON values differ, or "".
func firstGlueDifference(want json_value.Value, got json_value.Value, path string) string {
	switch typed := want.(type) {
	case json_value.Object:
		other, isObject := got.(json_value.Object)
		if !isObject {
			return fmt.Sprintf("%s: want an object, got %v", path, got)
		}
		keys := map[string]bool{}
		for key := range typed {
			keys[key] = true
		}
		for key := range other {
			keys[key] = true
		}
		sorted := make([]string, 0, len(keys))
		for key := range keys {
			sorted = append(sorted, key)
		}
		sort.Strings(sorted)
		for _, key := range sorted {
			wantValue, wantPresent := typed[key]
			gotValue, gotPresent := other[key]
			if wantPresent != gotPresent {
				return fmt.Sprintf(
					"%s.%s: present in the fork %v (%v), in Go %v (%v); node type %v",
					path,
					key,
					wantPresent,
					wantValue,
					gotPresent,
					gotValue,
					typed["type"],
				)
			}
			if difference := firstGlueDifference(wantValue, gotValue, path+"."+key); difference != "" {
				return difference
			}
		}
		return ""
	case json_value.Array:
		other, isList := got.(json_value.Array)
		if !isList || len(other) != len(typed) {
			return fmt.Sprintf("%s: want %d elements, got %v", path, len(typed), got)
		}
		for index := range typed {
			if difference := firstGlueDifference(
				typed[index],
				other[index],
				fmt.Sprintf("%s[%d]", path, index),
			); difference != "" {
				return difference
			}
		}
		return ""
	}
	if glueLeafText(want) != glueLeafText(got) {
		return fmt.Sprintf("%s: want %s, got %s", path, glueLeafText(want), glueLeafText(got))
	}
	return ""
}

// glueLeafText is a JSON value as Go's untyped decode would hold it, its type and its value, so two
// leaves are equal exactly when they decode to the same thing: a number is read as a float64, so 1
// and 1.0 are one number, as they are in JavaScript.
func glueLeafText(value json_value.Value) string {
	switch typed := value.(type) {
	case json_value.Number:
		number, parseError := json.Number(typed).Float64()
		if parseError != nil {
			return fmt.Sprintf("a number that does not read as a float64, %q", string(typed))
		}
		return fmt.Sprintf("float64 %v", number)
	case json_value.String:
		return fmt.Sprintf("string %q", string(typed))
	case json_value.Boolean:
		return fmt.Sprintf("bool %v", bool(typed))
	case json_value.Null:
		return "null"
	}
	return fmt.Sprintf("%T %v", value, value)
}
