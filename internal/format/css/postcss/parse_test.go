package postcss

import (
	"errors"
	"testing"

	"github.com/system-inc/cohere/internal/format/estree"
	"github.com/system-inc/cohere/internal/format/printing"
)

// parseFixture is an inline input. malformed says whether Parse refuses the whole text, as postcss
// 8.5.16 does, so TestParseNeverPanics holds each fixture to the verdict as well as to never panicking.
type parseFixture struct {
	name      string
	text      string
	malformed bool
}

var parseFixtures = []parseFixture{
	// Rules and nesting.
	{name: "one rule", text: "a{b:c}"},
	{
		name: "nested rules",
		text: ".a {\n  color: red;\n  .b { color: blue }\n  &:hover { x: y; }\n  > li { a: b }\n}\n",
	},
	{name: "deep nesting with at-rules", text: ".a { @media (x) { .b { @supports (y) { c: d } } } }"},
	{name: "empty rules", text: "a{} b { } c {\n}\n{}\n{ a: b }"},
	{name: "selector lists", text: "a,\nb , c>d~e+f, [x=\"y\"], :is(a, b)::before {}"},
	{name: "selector with brackets and parens", text: "a[title=\"x}y\"], a:not([x]) , a:nth-child(2n+1){b:c}"},
	{name: "hacks", text: "a{_b:c;*d:e}"},
	{
		name: "progid",
		text: "a{filter:progid:DXImageTransform.Microsoft.gradient(startColorstr='#1', endColorstr='#2')}",
	},

	// At-rules.
	{
		name: "import",
		text: "@import url(foo.css);\n@import 'a.css' screen;\n@import \"b.css\" layer(x) supports(display: grid);",
	},
	{name: "charset", text: "@charset \"utf-8\";"},
	{
		name: "media",
		text: "@media (min-width: 100px) and (max-width:200px) { a { b: c } }\n@media screen{}\n@media print,screen{a{b:c}}",
	},
	{
		name: "font-face",
		text: "@font-face{font-family:x;src:url(a.woff2) format('woff2'),url(\"b.woff\") format(\"woff\");font-display:swap}",
	},
	{
		name: "layer statements and blocks",
		text: "@layer base, components, utilities;\n@layer base { html { a: b } }\n@layer { }",
	},
	{
		name: "tailwind at-rules",
		text: "@import \"tailwindcss\";\n@theme inline {\n  --color-x: red;\n  --font-*: initial;\n}\n@custom-variant dark (&:where(.dark, .dark *));\n@utility tab-4 { tab-size: 4; }\n.a { @apply flex items-center; }\n@source \"../node_modules\";",
	},
	{
		name: "keyframes",
		text: "@keyframes spin { from { transform: rotate(0deg) } 50.5% { opacity: .5 } to { transform: rotate(360deg) } }",
	},
	{name: "supports", text: "@supports (display:grid) and (not (display:inline-grid)){a{b:c}}"},
	{name: "at-rule closed by brace", text: "a { @apply x y }\nb { @apply z   }\nc { @foo /* c */ }"},
	{name: "at-rule at end of file", text: "@foo bar"},
	{name: "bare at-rule at end of file", text: "@foo"},
	{name: "at-rule with trailing spaces at end of file", text: "@foo bar   \n"},
	{name: "at-rule name then spaces at end of file", text: "@foo   "},
	{name: "at-rule with comment params", text: "@foo /* c */;\n@foo bar /* c */ ;\n@foo/* c */bar;"},
	{name: "at-rule params with brackets", text: "@foo [a;b] (c{d}) ;\n@bar (a;{) {}"},
	{name: "at-rule params with braces in brackets", text: "@foo ({ x }) { a: b }"},
	{name: "at-rule then a free semicolon", text: "@foo;;"},
	{name: "at-rule directly followed", text: "@media(x){}@media{}"},
	{name: "at-word ended by each delimiter", text: "@a\"b\";@c'd';@e#f;@g/h;@i\\j;@k[l];"},

	// Declarations and !important.
	{name: "important", text: "a{b:c!important}"},
	{name: "important with spaces", text: "a{b:c ! important}"},
	{name: "important uppercase", text: "a{b:c !IMPORTANT}"},
	{name: "important then semicolon", text: "a{b:c! important ;}"},
	{name: "important then comment", text: "a{b:c !important /* x */}"},
	{name: "comment then important", text: "a{b:c /* x */ !important}"},
	{name: "important then spaces", text: "a{b:c!important  }"},
	{name: "important alone", text: "a{b: !important}"},
	{name: "important without bang", text: "a{b:c important}"},
	{name: "important split by a comment", text: "a{b:c ! /* x */ important}"},
	{name: "important twice", text: "a{b:c !important !important}"},
	{name: "important with tabs and newlines", text: "a{b:c\t!\n\timportant\n}"},
	{name: "important on a custom property", text: "a{--x: y !important}"},
	{name: "between with spaces and comments", text: "a{b /* x */ : /* y */ c}"},
	{name: "value spacing", text: "a{  b  :  c  d  ;  e:f  }"},
	{name: "colon in value after another word", text: "a{b:c url(x:y)}"},

	// Comments everywhere.
	{
		name: "comments everywhere",
		text: "/* a */ a /* b */ , c /* c */ { /* d */ b /* e */ : /* f */ c /* g */ ; /* h */ } /* i */",
	},
	{name: "comment inside a value", text: "a{b:c/* x */d; e:f, /* y */ g; h:i /* z */; j:/* k */l}"},
	{name: "comment with one safe neighbor", text: "a /* x */b, c/* y */ d{e:f /* z */g; h:i/* w */ j}"},
	{name: "comments in selectors", text: "a/* x */,b{} a /* x */{} a,/* x */b{} a/* x */b{}"},
	{name: "empty and blank comments", text: "/**/ /*   */ /*\n\n*/"},
	{name: "comment padding", text: "/*  text  */ /*\ttext\n*/ /* \xc2\xa0text\xe3\x80\x80 */ /*\xef\xbb\xbfx*/"},
	{name: "comment only", text: "/* only */"},
	{name: "comment in at-rule params", text: "@media /* a */ screen /* b */ and (x) /* c */ {}"},
	{name: "comment star slash adjacent", text: "a{b:c/**/}/**//**/"},

	// Strings.
	{name: "strings with escapes", text: "a{content:\"a\\\"b\";b:'c\\'d';c:\"\\\\\";d:\"\\\\\\\"\";}"},
	{name: "string with escaped newline", text: "a{b:\"multi\\\nline\"}"},
	{name: "strings with braces", text: "a{content:\"}\"; b: '{'; c: \";\"}"},

	// url() and parentheses.
	{name: "url unquoted", text: "a{b:url(foo.png)}"},
	{name: "url with spaces", text: "a{b:url( foo.png )}"},
	{name: "url single quoted", text: "a{b:url('a b.png')}"},
	{name: "url double quoted", text: "a{b:url(\"x\")}"},
	{
		name: "url data",
		text: "a{b:url(data:image/svg+xml;charset=utf8,%3Csvg%20xmlns='http://www.w3.org/2000/svg'%3E)}",
	},
	{name: "url with escaped paren", text: "a{b:url(a\\)b)}"},
	{name: "url empty", text: "a{b:url()}"},
	{name: "url with newline", text: "a { b: url(foo\n) }"},
	{name: "url uppercase", text: "a{b:URL(x;y)}"},
	{name: "url after a word elsewhere", text: "a{b:url x (y)}"},
	{name: "bad paren content", text: "a{b:x(a\n);c:y(\"q\");d:z(a/b);e:w(a\\b)}"},
	{name: "line terminator before a bad character", text: "a{b:x(\xe2\x80\xa8\n)}"},
	// Whether the first parenthesis is one brackets token or loose tokens decides whether "url" reaches
	// the tokenizer's buffer, so which of two errors the second, unclosed one raises.
	{name: "line terminator then url", text: "a{b:x(\xe2\x80\xa8\n\nurl) (y}", malformed: true},
	{name: "nested parens", text: "a{b:calc(1px + (2px * var(--x, 3px)))}"},
	{
		name: "square brackets",
		text: "a{grid-template-areas: [a] \"b\"; grid-template-columns: [full-start] 1fr [full-end]}",
	},

	// Custom properties.
	{name: "custom property json-like", text: "a{--x:{a:b}}"},
	{name: "custom property json", text: "a{--x: {\"a\": [1, 2], \"b\": {\"c\": null}}; --y: [1,{\"a\":2}]}"},
	{name: "custom property empty", text: "a{--empty:;}"},
	{name: "custom property space", text: "a{--empty: ;}"},
	{name: "custom property trailing spaces", text: "a{--x:   }"},
	{name: "custom property value trailing spaces", text: "a{--x: 1px  }"},
	{name: "custom property comment", text: "a{--x:a/* c */b; --y: /* only */; --z:a /* c */}"},
	{name: "custom property empty block", text: ":root{--a:{}}"},
	{name: "custom property block with semicolons", text: ":root{--a:{b:c;d:e};--f:g}"},
	{name: "custom property at end of file", text: "--x: y  "},

	// Semicolons.
	{name: "semicolon missing at block end", text: "a{b:c;d:e}"},
	{name: "doubled semicolons", text: "a{b:c;;d:e;;}"},
	{name: "leading semicolon", text: "a{;b:c}"},
	{name: "free semicolon after rule", text: "a{};b{}  ;  c{} ; ; d{}"},
	{name: "free semicolon past the end", text: "a{}  ;"},
	{name: "free semicolon before non-ASCII", text: "a{}   ;\xc3\xa9{}"},
	{name: "semicolon after at-rule block", text: "@media x{};"},
	{name: "declaration at end of file", text: "a: b"},
	{name: "declaration at end of file with trailing space", text: "a: b  \n"},

	// Byte order marks, line endings, non-ASCII.
	{name: "byte order mark", text: "\xef\xbb\xbfa{b:c}"},
	{name: "reversed byte order mark", text: "\xef\xbf\xbea{}"},
	{name: "byte order mark later", text: "a{}\xef\xbb\xbfb{}"},
	{name: "CRLF", text: "a {\r\n  b: c;\r\n}\r\n@media x {\r\n  d { e: f }\r\n}\r\n"},
	{name: "CR only", text: "a{\rb:c\r}\r"},
	{name: "form feed", text: "a{\fb:c\f}"},
	{
		name: "non-ASCII",
		text: ".\xc3\xa9 { content: \"\xe6\x97\xa5\xe6\x9c\xac\xe8\xaa\x9e \xf0\x9f\x98\x80\"; --x: \xf0\x9f\x98\x80; } /* \xe2\x9c\x93 */ \xf0\x9f\x98\x80 { a: b }",
	},
	{
		name: "non-ASCII positions across lines",
		text: "a{b:c}\n\xf0\x9f\x98\x80{x:y}\n@media \xf0\x9f\x98\x80{}\n\xf0\x9f\x98\x80\xf0\x9f\x98\x80 { z: \xf0\x9f\x98\x80 }",
	},
	{name: "escapes", text: "a\\:b{c:d} .\\31 0{} a{b:\\\\} .a\\\\b{} .x\\{{}"},
	{name: "escaped astral character", text: ".a\\\xf0\x9f\x98\x80{b:c}"},
	{name: "hex escape then space", text: ".\\E9 x{}"},

	// Odd shapes postcss accepts.
	{name: "colon first in a block", text: "a{:b}"},
	{name: "string first in a block", text: "a{\"x\":b}"},
	{name: "brackets first", text: "a{(x):b}"},
	{name: "progid before a colon", text: "a{b:progid:c}"},
	{name: "semicolon inside brackets at top", text: "a(b;c){}"},
	{name: "custom property with brace before colon", text: "--x{a:b}"},
	{name: "important inside a custom property block", text: "a{--x:{b:c !important}}"},
	{name: "important lookalikes", text: "a{b:important;c:!important;d:x!imp;e:x !important!important}"},
	{name: "between with a symbol word", text: "a{b + :c}"},

	// Empty input.
	{name: "empty", text: ""},
	{name: "whitespace only", text: "  \n\t "},

	// Malformed: each must be refused by both sides with the same error.
	{name: "unclosed block", text: "a{", malformed: true},
	{name: "unclosed block after a declaration", text: "a{b:c", malformed: true},
	{name: "unclosed nested block", text: "a{b{c:d}", malformed: true},
	{name: "unclosed block on a later line", text: "\xf0\x9f\x98\x80 {\r\n x { y: z }", malformed: true},
	{name: "unexpected close", text: "}", malformed: true},
	{name: "unexpected close later", text: "a{b:c}\n}", malformed: true},
	{name: "unclosed string", text: "a{b:\"c}", malformed: true},
	{name: "unclosed single quoted string", text: "a{b:'c\\'}", malformed: true},
	{name: "unclosed comment", text: "/* x", malformed: true},
	{name: "unclosed url", text: "a{b:url(x}", malformed: true},
	{name: "unclosed paren", text: "a{b:c(d}", malformed: true},
	{name: "unclosed paren with semicolon", text: "a{b:(c;}", malformed: true},
	{name: "unclosed square bracket", text: "a{b:[c}", malformed: true},
	{name: "unknown word at top", text: "a b", malformed: true},
	{name: "unknown word in block", text: "a{b c}", malformed: true},
	{name: "unknown word before colon", text: "a{b c:d}", malformed: true},
	{name: "unknown word non-ASCII", text: "a{\xf0\x9f\x98\x80\xf0\x9f\x98\x80 x}", malformed: true},
	{name: "double colon", text: "a{b::c}", malformed: true},
	{name: "missed semicolon", text: "a{b:c d:e}", malformed: true},
	{name: "missed semicolon after non-ASCII", text: "a{b:\xf0\x9f\x98\x80 c:d}", malformed: true},
	{name: "missed semicolon after a string", text: "a{b:\"x\" c:d}", malformed: true},
	{name: "at-rule without name", text: "@{}", malformed: true},
	{name: "at-rule without name before space", text: "a{} @ b{}", malformed: true},
	{name: "semicolon with no colon", text: "a{b;}", malformed: true},
	{name: "backslash at end", text: "a{b:c}\\", malformed: true},
	{name: "word then brace at top", text: "a b}", malformed: true},
	{name: "unclosed at-rule block", text: "@media x{a{b:c}", malformed: true},
	{name: "unclosed bracket in a custom property block", text: "a{--x:{a:b}", malformed: true},
	{name: "important word with a word character between", text: "a{b x:c}", malformed: true},
}

// TestParseTree checks one small tree field by field without node: types, fields, raws, and byte
// offsets past non-ASCII text, with column left in UTF-16 units.
func TestParseTree(test *testing.T) {
	test.Parallel()
	// U+00E9 is 2 bytes and 1 unit, the emoji 4 bytes and 2 units.
	root, parseError := Parse("\xc3\xa9{b:c ! important}\n\xf0\x9f\x98\x80 {/* x */--d:{e}}")
	if parseError != nil {
		test.Fatal(parseError)
	}
	if root.Type() != "root" || root.Range != [2]int{0, 41} {
		test.Fatalf("root %s %v", root.Type(), root.Range)
	}
	rules := root.List("nodes")
	if len(rules) != 2 {
		test.Fatalf("%d rules", len(rules))
	}

	first := rules[0]
	if first.Type() != "rule" || first.String("selector") != "\xc3\xa9" || first.Range != [2]int{0, 19} {
		test.Errorf("first rule %s %q %v", first.Type(), first.String("selector"), first.Range)
	}
	declaration := first.List("nodes")[0]
	raws, rawsIsObject := declaration.Get("raws").(estree.Record)
	if !rawsIsObject {
		test.Fatalf("declaration raws is %#v, want an object", declaration.Get("raws"))
	}
	if declaration.String("prop") != "b" ||
		declaration.String("value") != "c" ||
		!declaration.Bool("important") ||
		raws["important"] != estree.String(" ! important") ||
		raws["between"] != estree.String(":") {
		test.Errorf("declaration %v %v", declaration.Keys(), raws)
	}
	if keys := declaration.Keys(); len(keys) != 5 ||
		keys[0] != "raws" ||
		keys[1] != "source" ||
		keys[2] != "prop" ||
		keys[3] != "important" ||
		keys[4] != "value" {
		test.Errorf("declaration keys %v", keys)
	}

	second := rules[1]
	secondSource, secondSourceIsObject := second.Get("source").(estree.Record)
	if !secondSourceIsObject {
		test.Fatalf("second rule source is %#v, want an object", second.Get("source"))
	}
	start, startIsObject := secondSource["start"].(estree.Record)
	if !startIsObject {
		test.Fatalf("second rule source start is %#v, want an object", secondSource["start"])
	}
	if start["offset"] != estree.Integer(20) || start["line"] != estree.Integer(2) ||
		start["column"] != estree.Integer(1) {
		test.Errorf("second rule starts at %v", start)
	}
	children := second.List("nodes")
	comment := children[0]
	commentSource, commentSourceIsObject := comment.Get("source").(estree.Record)
	if !commentSourceIsObject {
		test.Fatalf("comment source is %#v, want an object", comment.Get("source"))
	}
	commentStart, commentStartIsObject := commentSource["start"].(estree.Record)
	if !commentStartIsObject {
		test.Fatalf("comment source start is %#v, want an object", commentSource["start"])
	}
	// The comment starts at column 5 of line 2 (the emoji is two units), 26 bytes into the text.
	if comment.Type() != "comment" ||
		comment.String("text") != "x" ||
		commentStart["column"] != estree.Integer(5) ||
		commentStart["offset"] != estree.Integer(26) {
		test.Errorf("comment %q at %v", comment.String("text"), commentStart)
	}
	custom := children[1]
	if custom.String("prop") != "--d" || custom.String("value") != "{e}" {
		test.Errorf("custom property %q %q", custom.String("prop"), custom.String("value"))
	}
}

// TestParseByteOrderMark checks that offsets count from after the byte order mark, as postcss's do.
func TestParseByteOrderMark(test *testing.T) {
	test.Parallel()
	root, parseError := Parse("\xef\xbb\xbfa{}")
	if parseError != nil {
		test.Fatal(parseError)
	}
	rule := root.List("nodes")[0]
	if rule.Range != [2]int{0, 3} {
		test.Errorf("rule range %v", rule.Range)
	}
}

// TestParseErrors checks that a refusal is marked as a syntax error and carries postcss's fields.
func TestParseErrors(test *testing.T) {
	test.Parallel()
	for _, each := range []struct {
		text      string
		reason    string
		line      int
		column    int
		endColumn int
	}{
		{"a{", "Unclosed block", 1, 1, 0},
		{"a{b:\"c}", "Unclosed string", 1, 5, 0},
		{"\xf0\x9f\x98\x80 b", "Unknown word \xf0\x9f\x98\x80", 1, 1, 3},
		{"a{b:c d:e}", "Missed semicolon", 1, 6, 0},
		{"}", "Unexpected }", 1, 1, 2},
	} {
		_, parseError := Parse(each.text)
		var syntaxError *CssSyntaxError
		if !printing.IsSyntax(parseError) || !errors.As(parseError, &syntaxError) {
			test.Errorf("%q: %v is not a marked CssSyntaxError", each.text, parseError)
			continue
		}
		if syntaxError.Reason != each.reason ||
			syntaxError.Line != each.line ||
			syntaxError.Column != each.column ||
			syntaxError.EndColumn != each.endColumn {
			test.Errorf("%q: %+v", each.text, *syntaxError)
		}
	}
}

// TestParseNeverPanics parses every prefix of every fixture: each must parse or be refused with a
// CssSyntaxError, never fail any other way, and the whole text must be refused exactly when the fixture
// says malformed.
func TestParseNeverPanics(test *testing.T) {
	test.Parallel()
	for _, fixture := range parseFixtures {
		if _, parseError := Parse(fixture.text); (parseError != nil) != fixture.malformed {
			test.Errorf(
				"%s: the fixture says malformed=%v and Parse returned %v",
				fixture.name,
				fixture.malformed,
				parseError,
			)
		}
		for end := 0; end <= len(fixture.text); end++ {
			_, parseError := Parse(fixture.text[:end])
			if parseError != nil && !printing.IsSyntax(parseError) {
				test.Errorf("%s, first %d bytes: %v", fixture.name, end, parseError)
			}
		}
	}
}
