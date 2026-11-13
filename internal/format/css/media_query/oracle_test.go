package media_query

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
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

// mediaQueryScript is the oracle: postcss-media-query-parser 0.2.3 from the fork's node_modules. Each
// input line is a fixture's params, or a corpus file, which the script parses with postcss 8 and walks
// for the at-rules Prettier's parser-postcss.js sends to parseMediaQuery: @media and @custom-media
// (case-insensitively), with params rebuilt as parseNestedCSS rebuilds them (raws.params, a
// non-blank afterName and between, trimmed), non-empty and without "#{". Every other at-rule's params
// go to parseValue or stay a string, so @supports, @container, @custom-variant, @import and the rest do
// not reach this parser. A file postcss refuses yields no params (Prettier would not get this far on it
// either). Each node dumps with its own keys sorted, parent left out; undefined dumps as
// "<undefined>" so presence is compared too. sourceIndex is UTF-16 there; the Go side converts.
const mediaQueryScript = `
import { createRequire } from "node:module";
const require = createRequire(process.cwd() + "/package.json");
const parseMedia = require("postcss-media-query-parser").default;
const postcssParse = require("postcss/lib/parse");
const fs = require("node:fs");
function dump(v) {
  if (v === undefined) return "<undefined>";
  if (v === null) return null;
  if (Array.isArray(v)) return v.map(dump);
  if (typeof v !== "object") return v;
  const o = {};
  for (const k of Object.keys(v).sort()) { if (k === "parent") continue; o[k] = dump(v[k]); }
  return o;
}
function run(name, params) {
  try {
    return { name, params, tree: dump(parseMedia(params)) };
  } catch (e) {
    return { name, params, error: String(e && e.message) };
  }
}
let input = ""; process.stdin.setEncoding("utf8"); process.stdin.on("data", (c) => input += c);
process.stdin.on("end", () => {
  for (const line of input.split("\n").filter(Boolean)) {
    const { name, params, file } = JSON.parse(line);
    if (file === undefined) { process.stdout.write(JSON.stringify(run(name, params)) + "\n"); continue; }
    let root;
    try { root = postcssParse(fs.readFileSync(file, "utf8"), { map: false }); } catch { continue; }
    let count = 0;
    root.walkAtRules((node) => {
      if (!["media", "custom-media"].includes(node.name.toLowerCase())) return;
      let p = node.raws.params ? (node.raws.params.scss ?? node.raws.params.raw) : node.params;
      if (node.raws.afterName && node.raws.afterName.trim().length > 0) p = node.raws.afterName + p;
      if (node.raws.between && node.raws.between.trim().length > 0) p += node.raws.between;
      p = p.trim();
      if (p.length === 0 || p.includes("#{")) return;
      process.stdout.write(JSON.stringify(run(name + " @" + node.name + " #" + (++count), p)) + "\n");
    });
  }
});
`

// mediaQueryFixture is an inline params string. throws says which way the library must go on it, so an
// oracle that throws on a fixture meant to parse, or parses one meant to throw, fails the test rather
// than quietly agreeing.
type mediaQueryFixture struct {
	name       string
	parameters string
	throws     bool
}

var mediaQueryFixtures = []mediaQueryFixture{
	// Media types, keywords and features.
	{name: "type and feature", parameters: "screen and (min-width: 768px)"},
	{name: "not", parameters: "not print"},
	{name: "only", parameters: "only screen"},
	{name: "bare type", parameters: "print"},
	{name: "bare feature", parameters: "(color)"},
	{name: "prefers color scheme", parameters: "(prefers-color-scheme: dark)"},
	{name: "prefers reduced motion", parameters: "(prefers-reduced-motion: reduce)"},
	{name: "vendor feature", parameters: "(-webkit-min-device-pixel-ratio: 2), (min-resolution: 192dpi)"},
	{name: "only with two features", parameters: "only screen and (min-width: 100px) and (max-width: 200px)"},
	{name: "not with feature", parameters: "not screen and (color), print and (orientation: landscape)"},
	{name: "features joined", parameters: "(min-width: 100px) and (max-width: 200px)"},
	{name: "upper case", parameters: "SCREEN AND (COLOR), NOT print, ONLY tv"},
	{name: "keywords alone", parameters: "and"},
	{name: "keywords in a row", parameters: "only and not"},

	// Comma lists.
	{name: "comma list", parameters: "screen, print"},
	{name: "comma list with spacing", parameters: "screen , print and (color)  ,tv"},
	{name: "empty queries in a list", parameters: "a,,b"},
	{name: "only commas", parameters: " , "},
	{name: "trailing comma", parameters: "screen,"},
	{name: "comma inside parens", parameters: "(a: f(1, 2)), print"},

	// Range syntax.
	{name: "range both sides", parameters: "(400px <= width <= 700px)"},
	{name: "range one side", parameters: "(width >= 600px)"},
	{name: "range strict", parameters: "screen and (400px < width < 700px)"},
	{name: "range with colon free value", parameters: "(height > 30em) and (width = 50em)"},

	// Spacing.
	{name: "no space inside", parameters: "screen and (max-width:100px)"},
	{name: "wide spacing", parameters: "  screen  and   (  max-width  :  100px  )   "},
	{name: "tabs and newlines", parameters: "screen\tand\n(color)\r\n,\n\tprint"},
	{name: "no space before paren", parameters: "screen and(color)"},
	{name: "no space after paren", parameters: "(color)and screen"},
	{name: "paren then word", parameters: "(a)b"},
	{name: "colon with empty value", parameters: "(a:)"},
	{name: "colon with spaces only", parameters: "(a:   )"},
	{name: "two colons", parameters: "(a: b: c)"},
	{name: "empty parens", parameters: "()"},

	// Comments (the library has no notion of them).
	{name: "comment between", parameters: "screen /* comment */ and (color)"},
	{name: "comment first", parameters: "/* c */ screen"},
	{name: "comment in feature", parameters: "(min-width: /* x */ 100px)"},

	// Custom media and functions.
	{name: "custom media", parameters: "--small-viewport (max-width: 30em)"},
	{name: "custom media list", parameters: "--narrow-window (max-width: 30em), print"},
	{name: "custom media reference", parameters: "(--small-viewport)"},
	{name: "calc value", parameters: "(max-width: calc(100px + 2em))"},
	{name: "var value", parameters: "screen and (min-width: var(--bp))"},
	{name: "function first", parameters: "fn(a, b) and (color)"},
	{name: "function alone", parameters: "fn(a b)"},
	{name: "nested parens", parameters: "(display: grid) and (not (display: inline-grid))"},
	{name: "style query", parameters: "style(--responsive: true)"},
	{name: "container params", parameters: "sidebar (min-width: 400px)"},
	{name: "custom variant params", parameters: "dark (&:where(.dark, .dark *))"},

	// The post-pass that types what the first pass left undefined.
	{name: "two words", parameters: "a b"},
	{name: "three words", parameters: "a b c"},
	{name: "four words", parameters: "a b c d"},
	{name: "five words", parameters: "a b c d e"},
	{name: "word paren word", parameters: "a (b) c"},
	{name: "word word paren", parameters: "a b (c)"},
	{name: "word word word paren", parameters: "a b c (d)"},
	{name: "word keyword word word", parameters: "a and b c"},
	{name: "paren then words", parameters: "(a) b c"},
	{name: "not then words", parameters: "not a b"},
	{name: "type then words", parameters: "only a b c"},

	// Interpolation and strings, which only the feature parser tracks.
	{name: "interpolation type", parameters: "#{$query} and (color)"},
	{name: "interpolation value", parameters: "screen and (max-width: #{$w})"},
	{name: "interpolation with colon", parameters: "(#{$a: b}: c)"},
	{name: "string in interpolation", parameters: "({'a}' : b}: c)"},
	{name: "escaped quote in interpolation", parameters: "({'a\\'b'}: c)"},
	{name: "double quote in interpolation", parameters: "({\"x:y\"}: z)"},
	{name: "quote outside interpolation", parameters: "(a: \"b:c\")"},
	{name: "braces first", parameters: "{a} b"},
	{name: "brace element", parameters: "{a b} (c)"},

	// url(), which the library peels off the front.
	{name: "url then type", parameters: "url(foo.css) screen"},
	{name: "url with spacing", parameters: "  url ( 'a(b)' )  print, tv"},
	{name: "url alone", parameters: "url(x)"},
	{name: "url not first", parameters: "screen url(x)"},
	{name: "url upper case", parameters: "URL(x) screen"},

	// Unbalanced parentheses: elements that never close are dropped.
	{name: "stray close paren", parameters: "screen)"},
	{name: "close paren mid word", parameters: "a)b c"},
	{name: "unclosed feature", parameters: "print and (color"},
	{name: "unclosed alone", parameters: "(a"},
	{name: "close paren first", parameters: ")"},
	{name: "open paren alone", parameters: "("},
	{name: "brace closing a feature", parameters: "(a} b", throws: true},
	{name: "brace closing a feature at the end", parameters: "screen and (color}", throws: true},
	{name: "brace closing a feature mid-word drops it", parameters: "(a}:b)"},
	{name: "interpolation then an extra brace", parameters: "(a{}}", throws: true},
	{name: "brace alone", parameters: "}"},
	{name: "brace pair element", parameters: "{a}"},

	// Empty and blank.
	{name: "empty", parameters: ""},
	{name: "blank", parameters: "   "},

	// Non-ASCII: offsets differ between UTF-16 and bytes, and JavaScript's \s is wider than ASCII.
	{name: "non-ASCII comment", parameters: "screen and (min-width: 1em) /* \xc3\xa9 */"},
	{name: "non-ASCII type", parameters: "\xc3\xa9cran and (color)"},
	// Before a feature's colon, where the feature is gathered byte by byte (@system_adamic's stream P2).
	{name: "no-break space before a feature", parameters: "(\u00a0x: 1px)"},
	{name: "non-ASCII in a feature", parameters: "(\xc3\xa9-width: 1px)"},
	{name: "non-ASCII after a feature's colon", parameters: "(x: \xc3\xa9)"},
	{name: "no-break space", parameters: "(max-width:\xc2\xa0100px)\xc2\xa0and\xc2\xa0print"},
	{name: "ideographic space", parameters: "\xe3\x80\x80screen\xe3\x80\x80and (color)"},
	{name: "emoji", parameters: "\xf0\x9f\x98\x80 and (a: \xf0\x9f\x98\x80), \xf0\x9f\x98\x80"},
	{name: "byte order mark", parameters: "\xef\xbb\xbfscreen"},
	{name: "next line is not whitespace", parameters: "a\xc2\x85b"},
	{name: "em space and line separator", parameters: "a\xe2\x80\x83b\xe2\x80\xa8c"},
}

// TestParseAgreesWithPostcssMediaQueryParser is the parser's acceptance test: for the params of every
// @media and @custom-media in the corpus, and every inline fixture, the Go tree must equal the
// library's, property for property, and a throw must be a refusal here with the same message. Off
// unless COHERE_PRETTIER_FORK names the fork (whose node_modules hold postcss-media-query-parser 0.2.3
// and postcss) and COHERE_CSS_CORPORA lists .css files or directories to walk (colon-separated,
// node_modules skipped).
//
// A corpus params string the library throws on is counted, and the test fails if that count passes a
// tenth of the corpus params, or if the corpus yields none: an oracle that rejects everything, or a
// corpus that reaches nothing, would otherwise read as agreement.
func TestParseAgreesWithPostcssMediaQueryParser(test *testing.T) {
	test.Parallel()
	corpora := needs.OptIn(
		test,
		"COHERE_CSS_CORPORA",
		"a differential of the parser against postcss-media-query-parser over a corpus",
	)
	root := corpus.PrettierFork.Root(test)

	type input struct {
		Name       string `json:"name"`
		Parameters string `json:"params"`
		File       string `json:"file,omitempty"`
	}
	var inputs []input
	corpusFiles := 0
	for _, corpus := range strings.Split(corpora, ":") {
		corpus = strings.TrimSpace(corpus)
		if corpus == "" {
			continue
		}
		if strings.HasPrefix(corpus, "~/") {
			home, homeError := os.UserHomeDir()
			if homeError != nil {
				test.Fatalf("reading %s: %v", corpus, homeError)
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
			if entry.IsDir() || filepath.Ext(path) != ".css" {
				return nil
			}
			source, readError := os.ReadFile(path)
			if readError != nil {
				return readError
			}
			if !utf8.Valid(source) {
				return fmt.Errorf("%s is not UTF-8, which the comparison cannot carry through JSON", path)
			}
			inputs = append(inputs, input{Name: path, File: path})
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
	throws := map[string]bool{}
	for _, fixture := range mediaQueryFixtures {
		name := "fixture: " + fixture.name
		inputs = append(inputs, input{Name: name, Parameters: fixture.parameters})
		throws[name] = fixture.throws
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
	command := exec.Command("node", "--input-type=module", "-e", mediaQueryScript)
	command.Dir = root
	command.Stdin = &stdin
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if runError := command.Run(); runError != nil {
		test.Fatalf("node: %v\n%s", runError, stderr.String())
	}

	var records []oracleRecord
	for _, line := range strings.Split(stdout.String(), "\n") {
		if line == "" {
			continue
		}
		var each oracleRecord
		if decodeError := json.Unmarshal([]byte(line), &each); decodeError != nil {
			test.Fatal(decodeError)
		}
		records = append(records, each)
	}

	identical, refusedByBoth, corpusParameters, corpusRefused, fixturesSeen, different := 0, 0, 0, 0, 0, 0
	fail := func(format string, arguments ...any) {
		different++
		if different <= 20 {
			test.Errorf(format, arguments...)
		}
	}
	for _, want := range records {
		if want.Name == nil || want.Parameters == nil {
			test.Fatalf("the oracle printed a record without a name and params: %+v", want)
		}
		name := *want.Name
		parameters := *want.Parameters
		wantError, oracleRefused := "", want.Error != nil
		if oracleRefused {
			wantError = *want.Error
		}
		expectThrow, isFixture := throws[name]
		if isFixture {
			fixturesSeen++
		} else {
			corpusParameters++
		}
		if isFixture && expectThrow != oracleRefused {
			fail(
				"%s: the fixture says throws=%v and the library says error %q, tree %s",
				name,
				expectThrow,
				wantError,
				want.Tree,
			)
			continue
		}
		got, dumpMediaQueryError := dumpMediaQuery(parameters)
		switch {
		case oracleRefused && dumpMediaQueryError != nil:
			if dumpMediaQueryError.Error() != wantError {
				fail(
					"%s: both refuse, with different messages:\n  library %s\n  Go      %s",
					name,
					wantError,
					dumpMediaQueryError.Error(),
				)
				continue
			}
			refusedByBoth++
			if !isFixture {
				corpusRefused++
			}
		case oracleRefused:
			fail("%s: the library throws (%s) on %q and Go parsed it", name, wantError, parameters)
		case dumpMediaQueryError != nil:
			fail("%s: the library parses %q and Go refused: %v", name, parameters, dumpMediaQueryError)
		default:
			wantTree, treeError := json_value.Parse(want.Tree)
			if treeError != nil {
				fail("%s (%q): the library's tree does not read as JSON: %v", name, parameters, treeError)
				continue
			}
			if difference := firstMediaQueryDifference(wantTree, got, "tree"); difference != "" {
				fail("%s (%q): %s", name, parameters, difference)
				continue
			}
			identical++
		}
	}
	test.Logf(
		"%d compared (%d params from %d corpus files, %d fixtures): %d identical, %d refused by both with the same message, %d different; %d corpus params refused",
		len(records),
		corpusParameters,
		corpusFiles,
		fixturesSeen,
		identical,
		refusedByBoth,
		different,
		corpusRefused,
	)
	if fixturesSeen != len(mediaQueryFixtures) {
		test.Errorf("the oracle answered %d of %d fixtures", fixturesSeen, len(mediaQueryFixtures))
	}
	if corpusParameters == 0 {
		test.Errorf("the corpus reached parseMediaQuery with no params; the comparison rests on fixtures alone")
	}
	if corpusRefused > corpusParameters/10 {
		test.Errorf(
			"the library refused %d of %d corpus params; the comparison is thinner than it looks",
			corpusRefused,
			corpusParameters,
		)
	}
}

// oracleRecord is one line of the oracle script's output: a params string with its tree, or the
// message the library threw. name and params are pointers so a record missing either is told apart.
type oracleRecord struct {
	Name       *string         `json:"name"`
	Parameters *string         `json:"params"`
	Error      *string         `json:"error"`
	Tree       json.RawMessage `json:"tree"`
}

// dumpMediaQuery parses params and renders the tree the way the oracle script does, as the JSON value
// the script prints.
func dumpMediaQuery(parameters string) (json_value.Value, error) {
	tree, parseError := Parse(parameters)
	if parseError != nil {
		return nil, parseError
	}
	return dumpMediaQueryValue(tree, utf16OffsetsOf(parameters)), nil
}

// dumpMediaQueryValue is one value of the tree as JSON. Every string the parser builds is a piece of
// params cut at an ASCII character or around whitespace, and params come from the oracle's JSON, so
// each is valid UTF-8 and reads in JSON as it is.
func dumpMediaQueryValue(value estree.Value, offsets []int) json_value.Value {
	switch typed := value.(type) {
	case nil:
		return json_value.String("<undefined>")
	case *estree.Node:
		result := json_value.Object{"type": json_value.String(typed.Type())}
		if typed.Type() == "" {
			result["type"] = json_value.String("<undefined>")
		}
		if typed.Range != [2]int{0, 0} {
			result["range"] = json_value.String("a range other than [0, 0], which the library has no use for")
		}
		for _, key := range typed.Keys() {
			result[key] = dumpMediaQueryValue(typed.Get(key), offsets)
		}
		return result
	case estree.Nodes:
		if typed == nil {
			return json_value.String("a nil list, which no library field holds")
		}
		result := make(json_value.Array, len(typed))
		for index, child := range typed {
			result[index] = dumpMediaQueryValue(child, offsets)
		}
		return result
	case estree.Integer:
		// Only sourceIndex is an Integer: a byte offset here, a UTF-16 index there.
		if typed < 0 || int(typed) >= len(offsets) {
			return json_value.String(fmt.Sprintf("an offset %d outside the input", typed))
		}
		return json_value.Number(strconv.Itoa(offsets[typed]))
	case estree.String:
		return json_value.String(typed)
	case estree.Boolean:
		return json_value.Boolean(typed)
	default:
		return json_value.String(fmt.Sprintf("an unexpected %T", typed))
	}
}

// utf16OffsetsOf maps every byte offset that starts a character (and the end) to its UTF-16 index.
func utf16OffsetsOf(text string) []int {
	offsets := make([]int, len(text)+1)
	index := 0
	for position, character := range text {
		size := utf8.RuneLen(character)
		if character == utf8.RuneError {
			size = 1
		}
		for byteIndex := position; byteIndex < position+size; byteIndex++ {
			offsets[byteIndex] = index
		}
		index += len(utf16.Encode([]rune{character}))
	}
	offsets[len(text)] = index
	return offsets
}

// firstMediaQueryDifference names the first path where two JSON values differ, or "".
func firstMediaQueryDifference(want json_value.Value, got json_value.Value, path string) string {
	switch typed := want.(type) {
	case json_value.Object:
		other, isMap := got.(json_value.Object)
		if !isMap {
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
					"%s.%s: present in the library %v, in Go %v (%v)",
					path,
					key,
					wantPresent,
					gotPresent,
					typed["type"],
				)
			}
			if difference := firstMediaQueryDifference(wantValue, gotValue, path+"."+key); difference != "" {
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
			if difference := firstMediaQueryDifference(
				typed[index],
				other[index],
				fmt.Sprintf("%s[%d]", path, index),
			); difference != "" {
				return difference
			}
		}
		return ""
	}
	if jsonLeafText(want) != jsonLeafText(got) {
		return fmt.Sprintf("%s: want %s, got %s", path, jsonLeafText(want), jsonLeafText(got))
	}
	return ""
}

// jsonLeafText is a JSON value as Go's untyped decode would hold it, its type and its value, so two
// leaves are equal exactly when they decode to the same thing: a number is read as a float64, so 1
// and 1.0 are one number, as they are in JavaScript.
func jsonLeafText(value json_value.Value) string {
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
