package postcss

import (
	"errors"
	"maps"
	"reflect"
	"testing"

	"github.com/system-inc/cohere/internal/format/estree"
	"github.com/system-inc/cohere/internal/format/printing"
)

// scssParseFixtures are inline scss inputs, with malformed as in parseFixtures: whether ParseScss refuses
// the whole text, as postcss-scss 4.0.9 does.
var scssParseFixtures = []parseFixture{
	// Inline comments in every position.
	{name: "inline comment at top", text: "// top\na{}"},
	{name: "inline comment ending the file", text: "a{}\n// last"},
	{name: "inline comment ending the file after a rule", text: "a{b:c}//x"},
	{name: "inline comment before a declaration", text: "a {\n  // c\n  b: c;\n}"},
	{name: "inline comment after a declaration", text: "a { b: c; // trailing\n}"},
	{name: "inline comment after a value with no semicolon", text: "a { b: c // x\n}"},
	{name: "inline comment inside a value", text: "a { b: c // x\n d; }"},
	{name: "inline comment between words of a value", text: "a{b:c// x\nd}"},
	{name: "inline comment in a selector list", text: "a, // x\nb { }"},
	{name: "inline comment before a brace", text: "a // x\n{ }"},
	{name: "inline comment inside a selector", text: "a// x\nb{}"},
	{name: "inline comment in at-rule params", text: "@include foo( // x\n  $a\n);"},
	{name: "inline comment before an at-rule block", text: "@media screen // x\n{}"},
	{name: "inline comment inside at-rule params", text: "@media screen// x\nand (y){}"},
	{name: "blank inline comments", text: "//\n// \t\na{}"},
	{name: "inline comment holding block comment delimiters", text: "// a /* b */ c\n//*/\n///*\na{}"},
	{name: "inline comment with delimiters in a value", text: "a { b: c /* x */ // y */ z\n; }"},
	{name: "inline comment with delimiters inside a value", text: "a { b: c // /* y */ \nd; }"},
	{name: "slashes then an unclosed comment", text: "a { b: c// /* y\nd; }", malformed: true},
	{name: "inline comment padding", text: "//   padded  \t\n//\xc2\xa0x\xe3\x80\x80\na{}"},
	{name: "inline comment with CRLF", text: "// x\r\na{}\r\n// y\r\n"},
	{name: "inline comment ended by a form feed", text: "// x\fa{}"},
	{name: "inline comment ended by CR", text: "// x\ra{}"},
	{name: "slashes inside a word", text: "a { b: x//y }"},
	{name: "slashes in url", text: "a { b: url(http://x.com/a); c: url( //x) }"},
	{name: "slashes in a string", text: "a { b: 'http://x' }"},
	{name: "inline comment in a custom property", text: "a { --x: a // b\n c; }"},
	{name: "inline comment after an at-rule statement", text: "@import 'a'; // x\n@import 'b' // y\n;"},
	{name: "inline comment in a map", text: "$map: (\n  a: 1, // one\n  b: 2 // two\n);"},
	{name: "inline comment non-ASCII", text: "// \xc3\xa9\xf0\x9f\x98\x80\na { b: c // \xf0\x9f\x98\x80\n}"},

	// Interpolation.
	{name: "interpolation in selectors", text: ".a-#{$b} { c: d }\n#{$sel} > a {}\n.a #{&}__b {}\n#{'.x'} {}"},
	{name: "interpolation in property names", text: "a { #{$prop}: 1px; border-#{$side}: 0; #{$a}-#{$b}: c; }"},
	{
		name: "interpolation in values",
		text: "a { width: calc(100% - #{$x}); content: \"#{$a}\"; b: #{$c}px; d: a#{$e}b; }",
	},
	{
		name: "interpolation in at-rule params",
		text: "@media #{$q} and (min-width: #{$w}) {}\n@include x(#{$y});\n@if #{$a} == b {}",
	},
	{name: "interpolation in at-rule names", text: "@#{$name} foo;\n@media#{$q}{}\n@a#{$b}#{$c} d;\n@a #{$b};"},
	{name: "nested interpolation", text: "a { b: #{ #{$x} } }"},
	{name: "interpolation holding a brace in a string", text: "a { b: #{'}'}; c: #{\"a\\\"}\"}; }"},
	{name: "string holding interpolation with a quote", text: "a { b: \"x #{'\"'} y\" }"},
	{name: "string with escaped quote and interpolation", text: "a { b: 'it\\'s #{$x}' }"},
	{name: "interpolation in url", text: "a { b: url(#{$x}/a.png); c: url(\"#{$y}\") }"},
	{name: "interpolation in a placeholder selector", text: "%#{$x}-y { a: b }"},
	{name: "interpolation in a comment", text: "/* #{$x} */ a{}"},
	{name: "hash without a brace", text: "a { color: #fff; b: c#d; }"},
	{name: "interpolation non-ASCII", text: "a { b: \"#{\xf0\x9f\x98\x80}\" }\n.\xc3\xa9-#{$x} {}"},

	// url() as postcss-scss reads it.
	{name: "url with nested parens", text: "a { b: url(foo(bar)) }"},
	{name: "url with escaped paren", text: "a { b: url(a\\)b) }"},
	{name: "url with spaces and newline", text: "a { b: url( x\n) }"},
	{name: "url empty", text: "a { b: url() }"},
	{name: "url after another word", text: "a{b:url x (y)}"},
	{name: "bad paren content", text: "a{b:x(a\n);c:y(\"q\");d:z(a/b);e:w(a\\b)}"},

	// Variables.
	{
		name: "variables with flags",
		text: "$a: 1px;\n$b: red !default;\n$c: 2 !global;\n$d: 3 !default !global;\n.x { $e: 1 !global; $f : 2; }",
	},
	{name: "maps", text: "$map: (key1: value1, key2: value2, key3: value3);"},
	{name: "multiline nested map", text: "$map: (\n  key1: value1,\n  key2: (a: b, c: d),\n  'key3': value3,\n);"},
	{name: "lists", text: "$list: 1px 2px, 3px;\n$l2: (a, b, c);\n$l3: [a b];"},
	{name: "variable at end of file", text: "$a: b"},
	{name: "variables in values", text: "a { width: $a + $b; height: -$x; c: $a+$b; d: math.div(10px, 2); e: 1 - 2 }"},

	// Nested properties.
	{name: "nested property with no value", text: "a { font: { family: x; size: 1px; } }"},
	{name: "nested property with a value", text: "a { font: 12px/30px { family: x; } }"},
	{name: "nested property margin", text: "a { margin: 0 { left: 1px } }"},
	{name: "nested property with a word value", text: "a { font: bold { family: x } }"},
	{name: "nested property without space", text: "a { font:bold{ family: x } }"},
	{name: "nested property important", text: "a { margin: 0 !important { left: 1px } }"},
	{name: "nested property important with spaces", text: "a { margin: 0 ! important { left: 1px } }"},
	{name: "nested property important alone", text: "a { margin: !important { left: 1px } b: x !important { c: d } }"},
	{name: "nested property important uppercase", text: "a { margin: 0 !IMPORTANT { left: 1px } }"},
	{
		name: "nested property important word",
		text: "a { margin: 0 important { left: 1px } b: 0 ! /* c */ important { d: e } }",
	},
	{name: "nested property hack", text: "a { _margin: 0 { left: 1px } *b: 1 { c: d } }"},
	{name: "nested property with comments", text: "a { margin /* c */ : /* d */ 0 { left: 1px } }"},
	{name: "nested property with an inline comment", text: "a { margin: 0 // c\n { left: 1px } }"},
	{name: "nested property spaced colon", text: "a { b : c { d: e } }"},
	{name: "nested property value on a new line", text: "a { b: c\n { d: e } }"},
	{name: "nested property in parens", text: "a { b: (c) { d: e } }"},
	{name: "nested property at top", text: "b: 1px { c: d }"},
	{name: "nested property at offset zero", text: "b:1{c:d}"},
	{name: "nested property ending in a comma", text: "a { b: c, { d: e } }"},
	{name: "selector broken by a newline before the colon", text: "a,\nb:hover {}\na\n:b {}"},
	{name: "pseudo selectors", text: "a { &:hover { } &:not(.b):hover { } a:nth-child(2n + 1) { } :is(a) {} }"},
	{name: "colon inside parens", text: "a { b(c: d) { } }"},

	// Mixins, functions and control directives.
	{
		name: "mixin and include",
		text: "@mixin foo($a, $b: 10px) { width: $a; }\n.x { @include foo(1px, $b: 2px); @include bar; @include baz { color: red; } @include qux($args...); }",
	},
	{
		name: "include with a block and arguments",
		text: ".a { @include breakpoint(md) { b: c } @include x() using ($y) { z: $y } }",
	},
	{name: "function and return", text: "@function double($n) { @return $n * 2; }"},
	{
		name: "content, use, forward, extend",
		text: "@use 'sass:math';\n@use \"a\" as b with ($c: d);\n@forward 'src/list' hide list-reset;\n@mixin m { @content; @content(1); }\n.a { @extend %placeholder; @extend .b !optional; }",
	},
	{name: "debug, warn, error", text: "@debug 'x';\n@warn \"y #{$z}\";\n@error 'bad';"},
	{
		name: "if and else",
		text: "@if $a == 1 { b: c } @else if $a == 2 { d: e } @else { f: g }\n@if not index($list, $x) {}\n.a{@if $x{b:c}@else{d:e}}",
	},
	{
		name: "each",
		text: "@each $name, $glyph in $icons { .icon-#{$name}:before { content: $glyph; } }\n@each $key, $value in (a: 1, b: 2) {}",
	},
	{
		name: "for and while",
		text: "@for $i from 1 through 3 { .item-#{$i} { width: 2em * $i; } }\n@while $i > 0 { .item-#{$i} { width: 2em * $i; } $i: $i - 2; }",
	},
	{name: "at-root and nested media", text: ".a { @at-root .b { c: d } @media (min-width: $md) { e: f } }"},
	{name: "import list", text: "@import 'a', 'b';\n@import url(foo.css);"},

	// Selectors.
	{name: "placeholder selectors", text: "%message-shared { border: 1px; }\n.message { @extend %message-shared; }"},
	{name: "parent selector suffixes", text: ".a { &-b { } &__c { } & + & {} &.d {} .e & {} &:hover {} }"},
	{name: "escapes", text: ".a\\:b {}\n.\\31 0{}"},
	{name: "keyframes", text: "@keyframes x { 0% { a: b } 100% { c: d } }"},

	// Commas, which postcss-scss makes their own word tokens ending one past them.
	{name: "trailing comma in a declaration", text: "a{b:c,}"},
	{name: "trailing comma after a space", text: "a{b:c ,}"},
	{name: "trailing comma in an at-rule", text: "a{@include x,}"},
	{name: "trailing comma in a selector", text: "a,{}"},
	{name: "comma in media params", text: "@media screen,print{}"},
	{name: "comma first", text: ",a{}"},
	{name: "comma at end of file", text: "a: b,"},
	{name: "commas in values", text: "a { b: c,d , e; f: g(h,i) }"},

	// Prettier's css-in-js placeholders.
	{name: "placeholder as a selector", text: "@prettier-placeholder-0-id {\n  color: red;\n}"},
	{
		name: "placeholder with a pseudo class",
		text: "@prettier-placeholder-0-id:hover { }\n@prettier-placeholder-1-id @prettier-placeholder-2-id {}",
	},
	{
		name: "placeholder inside a selector",
		text: ".a @prettier-placeholder-0-id { }\n.b@prettier-placeholder-1-id, .c { }",
	},
	{
		name: "placeholder as a value",
		text: "a { color: @prettier-placeholder-0-id; margin: @prettier-placeholder-0-id @prettier-placeholder-1-id; width: calc(100% - @prettier-placeholder-2-id); transition: opacity @prettier-placeholder-3-idms; }",
	},
	{
		name: "placeholder as a property",
		text: "@prettier-placeholder-0-id: 1px;\na { @prettier-placeholder-1-id: 1px; @prettier-placeholder-2-id-x: y }",
	},
	{
		name:      "placeholder after a word in a property",
		text:      "a { border-@prettier-placeholder-1-id: 1px; }",
		malformed: true,
	},
	{name: "placeholder standalone", text: "@prettier-placeholder-0-id;\n@prettier-placeholder-1-id\n"},
	{
		name: "placeholder standalone in a block",
		text: "a { @prettier-placeholder-0-id; color: red; @prettier-placeholder-1-id }",
	},
	{name: "placeholder in media params", text: "@media (max-width: @prettier-placeholder-0-id) { a { b: c } }"},
	{name: "placeholder in a comment", text: "/* @prettier-placeholder-0-id */\n// @prettier-placeholder-1-id\n"},
	{
		name: "placeholder template",
		text: "\n  display: flex;\n  @prettier-placeholder-0-id;\n  color: @prettier-placeholder-1-id;\n  &:hover { @prettier-placeholder-2-id }\n",
	},

	// Malformed.
	{name: "unclosed interpolation", text: "a { b: #{c", malformed: true},
	{name: "interpolation closing the block brace", text: "a { b: #{c }", malformed: true},
	{name: "unclosed interpolation in a string", text: "a { b: \"#{c\" }", malformed: true},
	{name: "unclosed string", text: "a { b: \"c }", malformed: true},
	{name: "unclosed string at the last character", text: "a{b:\"", malformed: true},
	{name: "unclosed url", text: "a{b:url(x}", malformed: true},
	{name: "unclosed block", text: "a {", malformed: true},
	{name: "unclosed comment", text: "/* x", malformed: true},
	{name: "nested property missing a semicolon", text: "a { b: c d:e { } }", malformed: true},
	{name: "nested property with no word", text: "a { :\"x\" {} }", malformed: true},
	{name: "unknown word", text: "a { b c }", malformed: true},
	{name: "unexpected close", text: "a{}}", malformed: true},
}

// TestParseScssTree checks what postcss-scss adds, without node: an inline comment's raws and rewritten
// text, a nested declaration's fields and their order, raws.value.scss, and an at-rule name joined with
// the interpolation after it.
func TestParseScssTree(test *testing.T) {
	test.Parallel()
	root, parseScssError := ParseScss("// a /* b\n.x { margin: 0 // c\n { left: 1px } }\n@media#{$q} {}")
	if parseScssError != nil {
		test.Fatal(parseScssError)
	}
	nodes := root.List("nodes")
	if len(nodes) != 3 {
		test.Fatalf("%d nodes", len(nodes))
	}

	comment := nodes[0]
	commentRaws, commentRawsIsObject := comment.Get("raws").(estree.Record)
	if !commentRawsIsObject {
		test.Fatalf("comment raws is %#v, want an object", comment.Get("raws"))
	}
	if comment.Type() != "comment" ||
		comment.String("text") != "a *//* b" ||
		commentRaws["inline"] != estree.Boolean(true) ||
		commentRaws["text"] != estree.String("a /* b") ||
		commentRaws["left"] != estree.String(" ") ||
		commentRaws["right"] != estree.String("") {
		test.Errorf("comment %q %v", comment.String("text"), commentRaws)
	}
	if comment.Range != [2]int{0, 9} {
		test.Errorf("comment range %v", comment.Range)
	}

	nested := nodes[1].List("nodes")[0]
	if nested.Type() != "decl" ||
		!nested.Bool("isNested") ||
		nested.String("prop") != "margin" ||
		len(nested.List("nodes")) != 1 {
		test.Errorf("nested declaration %s %v %q", nested.Type(), nested.Get("isNested"), nested.String("prop"))
	}
	if keys := nested.Keys(); !reflect.DeepEqual(
		keys,
		[]string{"raws", "isNested", "nodes", "source", "prop", "value"},
	) {
		test.Errorf("nested declaration keys %v", keys)
	}
	nestedRaws, nestedRawsIsObject := nested.Get("raws").(estree.Record)
	if !nestedRawsIsObject {
		test.Fatalf("nested declaration raws is %#v, want an object", nested.Get("raws"))
	}
	value := nestedRaws["value"]
	if record, isRecord := value.(estree.Record); !isRecord || !maps.Equal(record, estree.Record{
		"value": estree.String("0 "),
		"raw":   estree.String("0 /* c*/\n "),
		"scss":  estree.String("0 // c\n "),
	}) {
		test.Errorf("raws.value %#v", value)
	}

	media := nodes[2]
	if media.String("name") != "media#{$q}" || media.String("params") != "" {
		test.Errorf("at-rule %q %q", media.String("name"), media.String("params"))
	}
}

// TestParseScssErrors checks the refusals postcss-scss adds, and that its TypeError is marked too.
func TestParseScssErrors(test *testing.T) {
	test.Parallel()
	for _, each := range []struct {
		text   string
		reason string
		column int
	}{
		{"a { b: #{c", "Unclosed interpolation", 8},
		{"a { b: \"#{c\" }", "Unclosed interpolation", 8},
		{"a{b:\"", "Unclosed string", 5},
	} {
		_, parseScssError := ParseScss(each.text)
		var syntaxError *CssSyntaxError
		if !printing.IsSyntax(parseScssError) || !errors.As(parseScssError, &syntaxError) {
			test.Errorf("%q: %v is not a marked CssSyntaxError", each.text, parseScssError)
			continue
		}
		if syntaxError.Reason != each.reason || syntaxError.Line != 1 || syntaxError.Column != each.column {
			test.Errorf("%q: %+v", each.text, *syntaxError)
		}
	}

	_, parseScssError := ParseScss("a { :\"x\" {} }")
	var typeError *TypeError
	if !printing.IsSyntax(parseScssError) || !errors.As(parseScssError, &typeError) {
		test.Errorf("a nested property with no word: %v is not a marked TypeError", parseScssError)
	}

	// The css parser reads none of it: `//` is a word there and #{ is not interpolation.
	if _, parseError := Parse("a { b: #{c }"); parseError == nil {
		test.Errorf("Parse accepted an scss-only input")
	}
}

// TestParseScssNeverPanics parses every prefix of every scss fixture: each must parse or be refused with
// a marked error, never fail any other way, and the whole text must be refused exactly when the fixture
// says malformed.
func TestParseScssNeverPanics(test *testing.T) {
	test.Parallel()
	for _, fixture := range scssParseFixtures {
		if _, parseScssError := ParseScss(fixture.text); (parseScssError != nil) != fixture.malformed {
			test.Errorf(
				"%s: the fixture says malformed=%v and ParseScss returned %v",
				fixture.name,
				fixture.malformed,
				parseScssError,
			)
		}
		for end := 0; end <= len(fixture.text); end++ {
			_, parseScssError := ParseScss(fixture.text[:end])
			if parseScssError != nil && !printing.IsSyntax(parseScssError) {
				test.Errorf("%s, first %d bytes: %v", fixture.name, end, parseScssError)
			}
		}
	}
}
