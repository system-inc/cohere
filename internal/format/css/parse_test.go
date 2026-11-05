package css

import (
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/format/estree"
	"github.com/system-inc/cohere/internal/format/printing"
)

// The always-run checks on the parser glue, without node: the shapes the printer relies on and byte
// offsets worked out by hand. parse_oracle_test.go is the full comparison against the fork.

func mustParse(test *testing.T, text string) *estree.Node {
	test.Helper()
	root, parseError := parse(text)
	if parseError != nil {
		test.Fatalf("parse(%q): %v", text, parseError)
	}
	return root
}

// firstDeclaration is the first css-decl under the root's first rule.
func firstDeclaration(test *testing.T, root *estree.Node) *estree.Node {
	test.Helper()
	rule := root.List("nodes")[0]
	declaration := rule.List("nodes")[0]
	if !declaration.Is("css-decl") {
		test.Fatalf("want a css-decl, got %s", declaration.Type())
	}
	return declaration
}

// valueChain is the value-root's group, the value-value's group: the single node of a one-word value.
func valueChain(declaration *estree.Node) []*estree.Node {
	root := declaration.Child("value")
	value := root.Child("group")
	return []*estree.Node{root, value, value.Child("group")}
}

func TestParseGlueTypesAndOffsets(test *testing.T) {
	test.Parallel()
	text := "a { b: x --c }"
	root := mustParse(test, text)
	if !root.Is("css-root") || locStart(root) != 0 || locEnd(root) != len(text) {
		test.Fatalf("root: %s [%d, %d]", root.Type(), locStart(root), locEnd(root))
	}
	rule := root.List("nodes")[0]
	if !rule.Is("css-rule") || !rule.Child("selector").Is("selector-root") || rawsIn(rule)["selector"] != "a" {
		test.Fatalf(
			"rule: %s, selector %s, raws.selector %v",
			rule.Type(),
			rule.Child("selector").Type(),
			rawsIn(rule)["selector"],
		)
	}

	declaration := firstDeclaration(test, root)
	valueRoot := declaration.Child("value")
	if !valueRoot.Is("value-root") || valueRoot.String("text") != "x --c" {
		test.Fatalf("value root: %s %q", valueRoot.Type(), valueRoot.String("text"))
	}
	// The value-root starts after the prop and raws.between: "b" + ": ".
	if want := strings.Index(text, "x --c"); locStart(valueRoot) != want {
		test.Errorf("value-root starts at %d, want %d", locStart(valueRoot), want)
	}
	words := valueRoot.Child("group").Child("group").List("groups")
	if len(words) != 2 || !words[1].Is("value-word") || words[1].String("value") != "--c" {
		test.Fatalf("words: %v", words)
	}
	// fixValueWordLoc moves a word that starts with -- back over its dashes.
	if want := strings.Index(text, "--c"); locStart(words[1]) != want || locEnd(words[1]) != want+3 {
		test.Errorf("--c is [%d, %d], want [%d, %d]", locStart(words[1]), locEnd(words[1]), want, want+3)
	}
	for _, node := range []*estree.Node{root, rule, declaration, valueRoot, words[1]} {
		if node.Range != [2]int{locStart(node), locEnd(node)} {
			test.Errorf("%s: Range %v, source offsets [%d, %d]", node.Type(), node.Range, locStart(node), locEnd(node))
		}
	}
}

func TestParseGlueOffsetsAreBytes(test *testing.T) {
	test.Parallel()
	// "\xc3\xa9" is one UTF-16 unit and two bytes, "\xf0\x9f\x98\x80" two units and four bytes.
	text := "/* \xf0\x9f\x98\x80 */\na { b: \"\xc3\xa9\" c }\n.\xc3\xa9 { d: e }"
	root := mustParse(test, text)
	rule := root.List("nodes")[1]
	words := rule.List("nodes")[0].Child("value").Child("group").Child("group").List("groups")
	if len(words) != 2 || !words[1].Is("value-word") {
		test.Fatalf("words: %v", words)
	}
	if want := strings.Index(text, " c ") + 1; locStart(words[1]) != want || locEnd(words[1]) != want+1 {
		test.Errorf("c is [%d, %d], want [%d, %d]", locStart(words[1]), locEnd(words[1]), want, want+1)
	}
	class := root.List("nodes")[2].Child("selector").List("nodes")[0].List("nodes")[0]
	if want := strings.Index(
		text,
		".\xc3\xa9",
	); !class.Is("selector-class") || locStart(class) != want || locEnd(class) != want+3 {
		test.Errorf("%s is [%d, %d], want [%d, %d]", class.Type(), locStart(class), locEnd(class), want, want+3)
	}
}

func TestParseGlueUrlArgumentsAreAString(test *testing.T) {
	test.Parallel()
	root := mustParse(test, "a { b: url( x.png ) }")
	function := valueChain(firstDeclaration(test, root))[2]
	if !function.Is("value-func") || function.String("value") != "url" {
		test.Fatalf("want the url function, got %s", function.Type())
	}
	groups, isList := function.Child("group").Get("groups").([]any)
	if !isList || len(groups) != 1 || groups[0] != "x.png" {
		test.Fatalf("url groups: %#v", function.Child("group").Get("groups"))
	}
}

func TestParseGlueCustomPropertyBlock(test *testing.T) {
	test.Parallel()
	text := ":root { --x: { a: b } }"
	declaration := firstDeclaration(test, mustParse(test, text))
	value := declaration.Child("value")
	if !value.Is("css-rule") || len(value.List("nodes")) != 1 {
		test.Fatalf("value: %s with %d nodes", value.Type(), len(value.List("nodes")))
	}
	inner := value.List("nodes")[0]
	if want := strings.Index(
		text,
		"a: b",
	); !inner.Is("css-decl") || locStart(inner) != want || locStart(value) != want {
		test.Errorf("inner %s at %d, rule at %d, want %d", inner.Type(), locStart(inner), locStart(value), want)
	}
}

func TestParseGlueFrontMatter(test *testing.T) {
	test.Parallel()
	text := "---\ntitle: x\n---\na { b: c }"
	root := mustParse(test, text)
	frontMatter := root.Child("frontMatter")
	if !frontMatter.Is("front-matter") ||
		frontMatter.String("raw") != "---\ntitle: x\n---" ||
		frontMatter.String("value") != "title: x" {
		test.Fatalf(
			"front matter: %s raw %q value %q",
			frontMatter.Type(),
			frontMatter.String("raw"),
			frontMatter.String("value"),
		)
	}
	if want := strings.Index(text, "b: c"); locStart(firstDeclaration(test, root)) != want {
		test.Errorf("declaration at %d, want %d", locStart(firstDeclaration(test, root)), want)
	}
}

func TestParseGlueErrors(test *testing.T) {
	test.Parallel()
	if _, parseError := parse("a {"); parseError == nil || !printing.IsSyntax(parseError) {
		test.Errorf("an unclosed block: want a syntax error, got %v", parseError)
	}
	// Upstream throws a TypeError reading node.raws.value.raw: a crash, not a parse failure.
	if _, parseError := parse(":root { --x: {a} b }"); parseError == nil || printing.IsSyntax(parseError) {
		test.Errorf("a custom property upstream cannot read: want a plain error, got %v", parseError)
	}
	if _, parseError := parse("@custom-selector foo;"); parseError == nil || printing.IsSyntax(parseError) {
		test.Errorf("a custom selector without a name: want a plain error, got %v", parseError)
	}
}

func TestParseGlueVisitorKeys(test *testing.T) {
	test.Parallel()
	if keys := visitorKeys(estree.New("css-root", 0, 0)); !slices.Equal(keys, []string{"frontMatter", "nodes"}) {
		test.Errorf("css-root: %v", keys)
	}
	if keys := visitorKeys(
		estree.New("value-paren_group", 0, 0),
	); !slices.Equal(keys, []string{"open", "groups", "close"}) {
		test.Errorf("value-paren_group: %v", keys)
	}
	if _, known := lookupVisitorKeys(estree.New("front-matter", 0, 0)); known {
		test.Errorf("front-matter has no visitor keys upstream")
	}
}
