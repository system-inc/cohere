package next

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
)

// stringAttributeValue returns the text of a JSX attribute whose value is a plain string literal.
//
// The second return reports whether such an attribute was found at all, which callers need in order
// to tell "absent" apart from "present and empty".
//
// Only a string literal answers. `href={someExpression}` and a spread (`<link {...props} />`) both
// decline, and that is upstream's judgment rather than a simplification: a rule that decides from
// the text of an attribute cannot decide anything about a value it would have to evaluate, so
// guessing would report on code whose real value is unknown. oxc expresses the same rule as
// `get_prop_value(...).as_string_literal()`.
//
// Shared across this package because several `@next/next` rules read an attribute exactly this way,
// and a second implementation of it would be free to drift on the spread case, which is the one a
// porter is most likely to miss.
func stringAttributeValue(attributes *ast.Node, name string) (string, bool) {
	if attributes == nil || attributes.Kind != ast.KindJsxAttributes {
		return "", false
	}
	properties := attributes.AsJsxAttributes().Properties
	if properties == nil {
		return "", false
	}

	for _, property := range properties.Nodes {
		// A spread carries no name to match and no literal to read. It is skipped rather than
		// treated as absent, so an element that spreads its attributes simply has nothing to say
		// about this one.
		if property.Kind != ast.KindJsxAttribute {
			continue
		}

		attribute := property.AsJsxAttribute()
		attributeName := attribute.Name()
		if attributeName == nil || attributeName.Kind != ast.KindIdentifier {
			continue
		}
		if attributeName.Text() != name {
			continue
		}
		if attribute.Initializer == nil || attribute.Initializer.Kind != ast.KindStringLiteral {
			return "", false
		}
		return attribute.Initializer.Text(), true
	}

	return "", false
}

// jsxElementParts returns the tag name and attributes of either JSX opening form.
//
// A self-closing `<link />` parses as a JsxSelfClosingElement and never produces a
// JsxOpeningElement, and it is the shape most of these elements are written in, so a rule that
// reads only one form is silent on the common case.
func jsxElementParts(node *ast.Node) (tagName *ast.Node, attributes *ast.Node) {
	switch node.Kind {
	case ast.KindJsxOpeningElement:
		element := node.AsJsxOpeningElement()
		return element.TagName, element.Attributes
	case ast.KindJsxSelfClosingElement:
		element := node.AsJsxSelfClosingElement()
		return element.TagName, element.Attributes
	}
	return nil, nil
}

// isIntrinsicElementNamed reports whether a tag name is the given lowercase HTML element.
//
// A member-expression name (`<Foo.link />`) or a namespaced one is a component reference that never
// emits the HTML element, so comparing text alone would flag code that renders nothing of the kind.
func isIntrinsicElementNamed(tagName *ast.Node, name string) bool {
	return tagName != nil && tagName.Kind == ast.KindIdentifier && tagName.Text() == name
}

// hasAttributeNamed reports whether a JSX element writes an attribute with this name.
//
// Presence rather than value, which is a different question from stringAttributeValue and is the
// one some rules actually ask: `<script src=... async>` is exempt because `async` is written at
// all, whatever it is set to. Reading the value here would make a bare `async` (no initializer, the
// idiomatic spelling) look absent.
//
// A spread declines for the same reason as above: it carries no name to match, so an element that
// spreads its attributes reports nothing rather than falsely reporting the attribute missing.
func hasAttributeNamed(attributes *ast.Node, name string) bool {
	if attributes == nil || attributes.Kind != ast.KindJsxAttributes {
		return false
	}
	properties := attributes.AsJsxAttributes().Properties
	if properties == nil {
		return false
	}

	for _, property := range properties.Nodes {
		if property.Kind != ast.KindJsxAttribute {
			continue
		}
		attributeName := property.AsJsxAttribute().Name()
		if attributeName == nil || attributeName.Kind != ast.KindIdentifier {
			continue
		}
		if attributeName.Text() == name {
			return true
		}
	}

	return false
}
