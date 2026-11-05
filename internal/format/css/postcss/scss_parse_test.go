package postcss

import (
	"errors"
	"reflect"
	"testing"

	"github.com/system-inc/cohere/internal/format/printing"
)

// TestParseSCSSTree checks what postcss-scss adds, without node: an inline comment's raws and rewritten
// text, a nested declaration's fields and their order, raws.value.scss, and an at-rule name joined with
// the interpolation after it.
func TestParseSCSSTree(test *testing.T) {
	test.Parallel()
	root, parseSCSSError := ParseSCSS("// a /* b\n.x { margin: 0 // c\n { left: 1px } }\n@media#{$q} {}")
	if parseSCSSError != nil {
		test.Fatal(parseSCSSError)
	}
	nodes := root.List("nodes")
	if len(nodes) != 3 {
		test.Fatalf("%d nodes", len(nodes))
	}

	comment := nodes[0]
	commentRaws, commentRawsIsObject := comment.Get("raws").(map[string]any)
	if !commentRawsIsObject {
		test.Fatalf("comment raws is %#v, want an object", comment.Get("raws"))
	}
	if comment.Type() != "comment" ||
		comment.String("text") != "a *//* b" ||
		commentRaws["inline"] != true ||
		commentRaws["text"] != "a /* b" ||
		commentRaws["left"] != " " ||
		commentRaws["right"] != "" {
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
	nestedRaws, nestedRawsIsObject := nested.Get("raws").(map[string]any)
	if !nestedRawsIsObject {
		test.Fatalf("nested declaration raws is %#v, want an object", nested.Get("raws"))
	}
	value := nestedRaws["value"]
	if want := map[string]any{
		"value": "0 ",
		"raw":   "0 /* c*/\n ",
		"scss":  "0 // c\n ",
	}; !reflect.DeepEqual(value, want) {
		test.Errorf("raws.value %#v", value)
	}

	media := nodes[2]
	if media.String("name") != "media#{$q}" || media.String("params") != "" {
		test.Errorf("at-rule %q %q", media.String("name"), media.String("params"))
	}
}

// TestParseSCSSErrors checks the refusals postcss-scss adds, and that its TypeError is marked too.
func TestParseSCSSErrors(test *testing.T) {
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
		_, parseSCSSError := ParseSCSS(each.text)
		var syntaxError *CssSyntaxError
		if !printing.IsSyntax(parseSCSSError) || !errors.As(parseSCSSError, &syntaxError) {
			test.Errorf("%q: %v is not a marked CssSyntaxError", each.text, parseSCSSError)
			continue
		}
		if syntaxError.Reason != each.reason || syntaxError.Line != 1 || syntaxError.Column != each.column {
			test.Errorf("%q: %+v", each.text, *syntaxError)
		}
	}

	_, parseSCSSError := ParseSCSS("a { :\"x\" {} }")
	var typeError *TypeError
	if !printing.IsSyntax(parseSCSSError) || !errors.As(parseSCSSError, &typeError) {
		test.Errorf("a nested property with no word: %v is not a marked TypeError", parseSCSSError)
	}

	// The css parser reads none of it: `//` is a word there and #{ is not interpolation.
	if _, parseError := Parse("a { b: #{c }"); parseError == nil {
		test.Errorf("Parse accepted an scss-only input")
	}
}

// TestParseSCSSNeverPanics parses every prefix of every scss fixture: each must parse or be refused with
// a marked error, never fail any other way.
func TestParseSCSSNeverPanics(test *testing.T) {
	test.Parallel()
	for _, fixture := range scssParseFixtures {
		for end := 0; end <= len(fixture.text); end++ {
			_, parseSCSSError := ParseSCSS(fixture.text[:end])
			if parseSCSSError != nil && !printing.IsSyntax(parseSCSSError) {
				test.Errorf("%s, first %d bytes: %v", fixture.name, end, parseSCSSError)
			}
		}
	}
}
