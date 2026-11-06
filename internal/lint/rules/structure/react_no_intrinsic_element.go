package structure

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/ecmascript/jsx"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/policy"
)

// The two rules' messages, whose wording lives in `policy/messages/react-element-no-anchor.json` and
// `policy/messages/react-element-no-horizontal-rule.json`.
var (
	reactElementNoAnchorText         = policy.MessageOf("structure/react-element-no-anchor", "noAnchorElement")
	reactElementNoHorizontalRuleText = policy.MessageOf("structure/react-element-no-horizontal-rule", "noHrElement")
)

func messageNoAnchorElement() rule.Message {
	return rule.Message{Id: reactElementNoAnchorText.Id, Description: reactElementNoAnchorText.Render(nil)}
}

func messageNoHorizontalRuleElement() rule.Message {
	return rule.Message{
		Id:          reactElementNoHorizontalRuleText.Id,
		Description: reactElementNoHorizontalRuleText.Render(nil),
	}
}

// ReactElementNoAnchor flags a raw <a> element outside the Link component's own implementation.
//
//	valid:   <Link href="/about">About</Link>
//	valid:   an <a> inside components/navigation/Link.tsx
//	invalid: <a href="/about">About</a>
var ReactElementNoAnchor = rule.Rule[rule.NoOptions]{
	Name: "structure/react-element-no-anchor",
	Run: func(ruleContext rule.Context, options rule.Given[rule.NoOptions]) rule.Listeners {
		fileContext := FileContextFor(ruleContext.SourceFile.FileName().AsString())
		if fileContext.IsLinkComponentFile {
			return nil
		}
		return intrinsicElementListeners(ruleContext, "a", messageNoAnchorElement())
	},
}

// ReactElementNoHorizontalRule flags a raw <hr> element outside HorizontalRule's implementation.
//
//	valid:   <HorizontalRule />
//	valid:   an <hr> inside components/layout/HorizontalRule.tsx
//	invalid: <hr />
var ReactElementNoHorizontalRule = rule.Rule[rule.NoOptions]{
	Name: "structure/react-element-no-horizontal-rule",
	Run: func(ruleContext rule.Context, options rule.Given[rule.NoOptions]) rule.Listeners {
		fileContext := FileContextFor(ruleContext.SourceFile.FileName().AsString())
		if fileContext.IsHorizontalRuleComponentFile {
			return nil
		}
		return intrinsicElementListeners(ruleContext, "hr", messageNoHorizontalRuleElement())
	},
}

// intrinsicElementListeners reports every opening element with the given intrinsic tag name.
//
// Two rules share this because they are the same rule with two nouns, and the shared version is
// what stops them drifting: the next intrinsic element somebody bans should get the same treatment
// of self-closing elements and the same handling of the name node rather than a third
// implementation that handles one of them differently.
//
// Both JSX opening forms are listened to. A `<hr />` is a JsxSelfClosingElement and never produces
// a JsxOpeningElement, so a rule listening only to the latter is silent on the shape `<hr>` is
// almost always written in. That asymmetry is the same class of miss as no-var's loop forms.
func intrinsicElementListeners(ruleContext rule.Context, tagName string, message rule.Message) rule.Listeners {
	report := func(node *ast.Node, name *ast.Node) {
		// The name is compared as text and required to be a plain identifier. An intrinsic element
		// is lowercase and unqualified by definition, so a member-expression name (`<Foo.a />`) or
		// a namespaced one is a component reference rather than the HTML tag, and reporting it
		// would flag code that never emits an <a> at all.
		if name == nil || name.Kind != ast.KindIdentifier || name.Text() != tagName {
			return
		}
		ruleContext.ReportNode(node, message)
	}

	return rule.Listeners{
		ast.KindJsxOpeningElement: func(node *ast.Node) {
			tagName, _ := jsx.ElementParts(node)
			report(node, tagName)
		},
		ast.KindJsxSelfClosingElement: func(node *ast.Node) {
			tagName, _ := jsx.ElementParts(node)
			report(node, tagName)
		},
	}
}
