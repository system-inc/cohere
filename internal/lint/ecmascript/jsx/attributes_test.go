package jsx

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/microsoft/TypeScript/tsc/shim/parser"
)

// These fixtures did not exist before the lift.
//
// The code was proven only through the five `@next/next` rules that called it, which is proof that
// it works for what they ask and no statement at all about what it promises to a caller that has
// not been written yet. A shared package with weak fixtures is worse than a duplicated helper,
// because the weakness is invisible from every rule that depends on it, and eighteen `react` ports
// are about to depend on this one.

// firstElement parses source text and returns its first JSX opening or self-closing element.
func firstElement(test *testing.T, sourceText string) *ast.Node {
	test.Helper()
	file := parser.ParseSourceFile(ast.SourceFileParseOptions{
		FileName: "/repository/source/Thing.tsx",
		PathKey:  "/repository/source/Thing.tsx",
	}, sourceText, core.ScriptKindTSX)
	if file == nil {
		test.Fatal("the parser returned no source file")
	}

	var found *ast.Node
	var walk func(node *ast.Node) bool
	walk = func(node *ast.Node) bool {
		if found != nil || node == nil {
			return true
		}
		if node.Kind == ast.KindJsxOpeningElement || node.Kind == ast.KindJsxSelfClosingElement {
			found = node
			return true
		}
		node.ForEachChild(walk)
		return found != nil
	}
	file.AsNode().ForEachChild(walk)

	if found == nil {
		test.Fatalf("no JSX element in %q", sourceText)
	}
	return found
}

// A self-closing element never produces a JsxOpeningElement, and it is the shape most of these
// elements are written in. A helper that reads only one form is silent on the common case, which is
// the failure that reads as a rule finding nothing.
func TestElementPartsReadsBothOpeningForms(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name       string
		sourceText string
		wantTag    string
	}{
		{"a self-closing element", "export const A = <link rel='x' />;\n", "link"},
		{"an element with children", "export const A = <div id='x'>text</div>;\n", "div"},
		{"a component", "export const A = <Thing id='x' />;\n", "Thing"},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			tagName, attributes := ElementParts(firstElement(test, testCase.sourceText))
			if tagName == nil {
				test.Fatal("want a tag name, got none")
			}
			if tagName.Text() != testCase.wantTag {
				test.Fatalf("want tag %q, got %q", testCase.wantTag, tagName.Text())
			}
			if attributes == nil {
				test.Fatal("want attributes, got none")
			}
		})
	}
}

// A node that is not a JSX element has no parts, and saying so is different from returning something
// empty that a caller then reads as an element with no attributes.
func TestElementPartsDeclinesEverythingElse(test *testing.T) {
	test.Parallel()
	tagName, attributes := ElementParts(firstElement(test, "export const A = <div />;\n").Parent)
	if tagName != nil || attributes != nil {
		test.Fatalf("want no parts for a non-element node, got tag=%v attributes=%v", tagName, attributes)
	}
}

// The two matchers exist because upstream matches two ways and which one a rule uses is part of what
// that rule decides. Unifying them would mean choosing for upstream in one direction or the other.
func TestStringAttributeValueHonoursBothMatchers(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name       string
		sourceText string
		attribute  string
		matches    NameMatch
		wantValue  string
		wantFound  bool
	}{
		{"an exact match", "export const A = <link rel='stylesheet' />;\n", "rel", MatchExactly, "stylesheet", true},
		{"exact matching rejects a case difference", "export const A = <link REL='stylesheet' />;\n", "rel", MatchExactly, "", false},
		{"insensitive matching accepts it", "export const A = <link REL='stylesheet' />;\n", "rel", MatchIgnoringCase, "stylesheet", true},
		{"an absent attribute", "export const A = <link href='x' />;\n", "rel", MatchExactly, "", false},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			_, attributes := ElementParts(firstElement(test, testCase.sourceText))
			value, found := StringAttributeValue(attributes, testCase.attribute, testCase.matches)
			if found != testCase.wantFound {
				test.Fatalf("want found=%v, got %v", testCase.wantFound, found)
			}
			if value != testCase.wantValue {
				test.Fatalf("want value %q, got %q", testCase.wantValue, value)
			}
		})
	}
}

// The empty string is a real value and must not read as absence, which is why the second return
// exists at all. A caller that collapsed them would treat `alt=""` as an element with no alt.
func TestStringAttributeValueSeparatesEmptyFromAbsent(test *testing.T) {
	test.Parallel()
	_, attributes := ElementParts(firstElement(test, "export const A = <img alt='' />;\n"))
	value, found := StringAttributeValue(attributes, "alt", MatchExactly)
	if !found {
		test.Fatal("want an empty attribute to be found")
	}
	if value != "" {
		test.Fatalf("want an empty value, got %q", value)
	}
}

// Only a string literal answers. An expression and a spread both decline, and that is upstream's
// judgment rather than a simplification: a rule deciding from an attribute's text cannot decide
// anything about a value it would have to evaluate.
func TestStringAttributeValueDeclinesValuesItCannotRead(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name       string
		sourceText string
	}{
		{"an expression value", "export const A = <link href={someExpression} />;\n"},
		{"a spread", "export const A = <link {...properties} />;\n"},
		{"a bare attribute with no initializer", "export const A = <script async />;\n"},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			_, attributes := ElementParts(firstElement(test, testCase.sourceText))
			name := "href"
			if testCase.name == "a bare attribute with no initializer" {
				name = "async"
			}
			if _, found := StringAttributeValue(attributes, name, MatchExactly); found {
				test.Fatalf("want %s to decline, got a value", testCase.name)
			}
		})
	}
}

// Presence is a different question from value, and it is the one some rules actually ask. Reading
// the value here would make a bare `async`, the idiomatic spelling, look absent.
func TestHasAttributeNamedAsksAboutPresence(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name       string
		sourceText string
		attribute  string
		matches    NameMatch
		want       bool
	}{
		{"a bare attribute", "export const A = <script async />;\n", "async", MatchExactly, true},
		{"an attribute with a value", "export const A = <script async='true' />;\n", "async", MatchExactly, true},
		{"an expression value still counts as present", "export const A = <script async={flag} />;\n", "async", MatchExactly, true},
		{"an absent attribute", "export const A = <script src='x' />;\n", "async", MatchExactly, false},
		{"case matters to the exact matcher", "export const A = <script ASYNC />;\n", "async", MatchExactly, false},
		{"and not to the insensitive one", "export const A = <script ASYNC />;\n", "async", MatchIgnoringCase, true},
		// A spread carries no name to match, so an element that spreads reports nothing rather than
		// falsely reporting the attribute missing.
		{"a spread alone", "export const A = <script {...properties} />;\n", "async", MatchExactly, false},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			_, attributes := ElementParts(firstElement(test, testCase.sourceText))
			if got := HasAttributeNamed(attributes, testCase.attribute, testCase.matches); got != testCase.want {
				test.Fatalf("want %v, got %v", testCase.want, got)
			}
		})
	}
}

// A member-expression or namespaced tag is a component reference that never emits the HTML element,
// so comparing text alone would flag code that renders nothing of the kind.
func TestIsIntrinsicElementNamedRejectsComponentReferences(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name       string
		sourceText string
		want       bool
	}{
		{"the intrinsic element", "export const A = <link />;\n", true},
		{"a component with the same spelling", "export const A = <Link />;\n", false},
		{"a member expression ending in the name", "export const A = <Foo.link />;\n", false},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			tagName, _ := ElementParts(firstElement(test, testCase.sourceText))
			if got := IsIntrinsicElementNamed(tagName, "link"); got != testCase.want {
				test.Fatalf("want %v, got %v", testCase.want, got)
			}
		})
	}
}

// Nil inputs answer rather than panic. A rule reaching this helper with a node it did not check is
// a bug in the rule, and a panic in a shared package takes the whole run down instead of that one
// rule's finding.
func TestHelpersSurviveNilInput(test *testing.T) {
	test.Parallel()
	if _, found := StringAttributeValue(nil, "rel", MatchExactly); found {
		test.Fatal("want nil attributes to report nothing found")
	}
	if HasAttributeNamed(nil, "rel", MatchExactly) {
		test.Fatal("want nil attributes to report absence")
	}
	if IsIntrinsicElementNamed(nil, "link") {
		test.Fatal("want a nil tag name to report false")
	}
}

// AttributeName is the guard that was written twice inside this file before it was one function,
// and it is what a rule reporting *on the attribute* needs: the element-level helpers answer about
// the element and hand back no node to anchor a finding on.
func TestAttributeNameReadsPlainIdentifiers(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name       string
		sourceText string
		want       string
		wantNamed  bool
	}{
		{"an attribute with a value", "export const A = <div id='x' />;\n", "id", true},
		// A bare attribute has a name and no initializer. StringAttributeValue declines it, which
		// is correct for its question and wrong for this one: `children` written bare is a corpus
		// fail case for no-children-prop, so the name has to be readable without a value.
		{"a bare attribute", "export const A = <div children />;\n", "children", true},
		{"an expression value", "export const A = <div id={value} />;\n", "id", true},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			_, attributes := ElementParts(firstElement(test, testCase.sourceText))
			properties := attributes.AsJsxAttributes().Properties
			if properties == nil || len(properties.Nodes) == 0 {
				test.Fatal("want at least one property")
			}
			got, named := AttributeName(properties.Nodes[0])
			if named != testCase.wantNamed {
				test.Fatalf("want named=%v, got %v", testCase.wantNamed, named)
			}
			if got != testCase.want {
				test.Fatalf("want name %q, got %q", testCase.want, got)
			}
		})
	}
}

// The three shapes that decline, each a real thing a rule meets rather than a defensive case.
func TestAttributeNameDeclinesWhatHasNoPlainName(test *testing.T) {
	test.Parallel()
	test.Run("a spread carries no name", func(test *testing.T) {
		test.Parallel()
		_, attributes := ElementParts(firstElement(test, "export const A = <div {...properties} />;\n"))
		nodes := attributes.AsJsxAttributes().Properties.Nodes
		if _, named := AttributeName(nodes[0]); named {
			test.Fatal("want a spread to decline")
		}
	})

	// A namespaced name is not an identifier, and oxc declines it everywhere by destructuring
	// JSXAttributeName::Identifier. Reading the local part instead would make `xlink:href` answer
	// to a rule asking about `href`.
	test.Run("a namespaced name declines", func(test *testing.T) {
		test.Parallel()
		_, attributes := ElementParts(firstElement(test, "export const A = <svg xlink:href='x' />;\n"))
		nodes := attributes.AsJsxAttributes().Properties.Nodes
		if _, named := AttributeName(nodes[0]); named {
			test.Fatal("want a namespaced name to decline")
		}
	})

	test.Run("a nil node declines", func(test *testing.T) {
		test.Parallel()
		if _, named := AttributeName(nil); named {
			test.Fatal("want nil to decline")
		}
	})
}

// The namespaced case has to decline through the element-level helpers too, since they now route
// through AttributeName. This is the assertion that would catch someone "fixing" the helper to read
// a namespaced local part.
func TestNamespacedAttributesAreInvisibleToTheElementHelpers(test *testing.T) {
	test.Parallel()
	_, attributes := ElementParts(firstElement(test, "export const A = <svg xlink:href='x' />;\n"))
	if HasAttributeNamed(attributes, "href", MatchExactly) {
		test.Fatal("want a namespaced attribute not to answer to its local part")
	}
	if _, found := StringAttributeValue(attributes, "href", MatchExactly); found {
		test.Fatal("want a namespaced attribute to yield no value")
	}
}
