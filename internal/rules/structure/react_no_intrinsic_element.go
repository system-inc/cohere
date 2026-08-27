package structure

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/verify/internal/rule"
	"github.com/system-inc/verify/internal/utilities/jsx"
)

var messageNoAnchorElement = rule.Message{
	Id: "noAnchorElement",
	Description: "This is a raw <a> element. Use the Link component from " +
		"@structure/source/components/navigation/Link instead. Link is where client-side " +
		"navigation, prefetching, and the external-link handling live, so an <a> written directly " +
		"does a full page load and drops all of it, and it does so without failing anything.",
}

var messageNoHorizontalRuleElement = rule.Message{
	Id: "noHrElement",
	Description: "This is a raw <hr> element. Use the HorizontalRule component from " +
		"@structure/source/components/layout/HorizontalRule instead, so a change to how a divider " +
		"looks lands everywhere at once rather than in the files somebody remembered.",
}

// ReactNoAnchorElement flags a raw <a> element outside the Link component's own implementation.
//
//	valid:   <Link href="/about">About</Link>
//	valid:   an <a> inside components/navigation/Link.tsx
//	invalid: <a href="/about">About</a>
var ReactNoAnchorElement = rule.Rule{
	Name: "structure/react-no-anchor-element",
	Run: func(ctx rule.Context, options any) rule.Listeners {
		fileContext := FileContextFor(ctx.SourceFile.FileName())
		if fileContext.IsLinkComponentFile {
			return nil
		}
		return intrinsicElementListeners(ctx, "a", messageNoAnchorElement)
	},
}

// ReactNoHorizontalRuleElement flags a raw <hr> element outside HorizontalRule's implementation.
//
//	valid:   <HorizontalRule />
//	valid:   an <hr> inside components/layout/HorizontalRule.tsx
//	invalid: <hr />
var ReactNoHorizontalRuleElement = rule.Rule{
	Name: "structure/react-no-horizontal-rule-element",
	Run: func(ctx rule.Context, options any) rule.Listeners {
		fileContext := FileContextFor(ctx.SourceFile.FileName())
		if fileContext.IsHorizontalRuleComponentFile {
			return nil
		}
		return intrinsicElementListeners(ctx, "hr", messageNoHorizontalRuleElement)
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
func intrinsicElementListeners(ctx rule.Context, tagName string, message rule.Message) rule.Listeners {
	report := func(node *ast.Node, name *ast.Node) {
		// The name is compared as text and required to be a plain identifier. An intrinsic element
		// is lowercase and unqualified by definition, so a member-expression name (`<Foo.a />`) or
		// a namespaced one is a component reference rather than the HTML tag, and reporting it
		// would flag code that never emits an <a> at all.
		if name == nil || name.Kind != ast.KindIdentifier || name.Text() != tagName {
			return
		}
		ctx.ReportNode(node, message)
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
