package structure

import (
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/verify/internal/rule"
)

const propertiesTypeSuffixReasoning = "A component's property type is the one type a reader looks " +
	"up by guessing its name from the component's, so the suffix is what makes that guess work. " +
	"Mixed suffixes mean every search for one spelling misses the other."

var messageUseComponentPropertyTypeSuffix = rule.Message{
	Id: "useComponentPropertyTypeSuffix",
	Description: "This component's property type does not end in Properties. " +
		propertiesTypeSuffixReasoning,
}

var messageUseInterfaceSuffix = rule.Message{
	Id: "useInterfaceSuffix",
	Description: "This interface is used as a component's property type and does not end in " +
		"Properties. " + propertiesTypeSuffixReasoning + " Reported separately from the use site " +
		"because renaming the declaration is the edit, and the use site follows.",
}

var messageUseTypeAliasSuffix = rule.Message{
	Id: "useTypeAliasSuffix",
	Description: "This type alias is used as a component's property type and does not end in " +
		"Properties. " + propertiesTypeSuffixReasoning,
}

// ReactComponentRequirePropertiesTypeSuffix flags a component property type not named `*Properties`.
//
//	valid:   function Button(properties: ButtonProperties) { ... }
//	invalid: function Button(properties: ButtonProps) { ... }
//	invalid: interface ButtonInterface {} function Button(properties: ButtonInterface) { ... }
//
// Three message ids, and they are three sites rather than three defects. One wrong name produces a
// finding at the parameter and another at the declaration, because a reader fixing this has to
// touch both and seeing only one would leave the rename half done.
//
// The declaration findings depend on a set the parameter visitor fills, so declaration order
// decides: an interface declared after the component that uses it is reported, one declared before
// is not seen yet when its own visitor runs. That is the original's behavior and it is reproduced
// rather than corrected, since the alternative walks the file twice for a finding whose use site is
// already reported.
//
// The rename is derived rather than appended blindly. `ButtonProps` becomes `ButtonProperties` and
// `ButtonInterface` becomes `ButtonProperties`, because appending would produce `ButtonPropsProperties`,
// which nobody wants and which the author would have to fix by hand anyway.
var ReactComponentRequirePropertiesTypeSuffix = rule.Rule{
	Name: "react-component-require-properties-type-suffix",
	Run: func(ctx rule.Context, options any) rule.Listeners {
		if !FileContextFor(ctx.SourceFile.FileName()).IsReactFile {
			return nil
		}

		// Type names seen at a component's first parameter that lack the suffix. Filled by the
		// component visitors and read by the declaration ones.
		componentPropertyTypes := map[string]bool{}

		checkParameterType := func(parameters []*ast.Node) {
			if len(parameters) == 0 {
				return
			}

			// A plain identifier parameter only, matching the original. A destructured parameter is
			// `react-component-no-destructuring`'s business, and its type is often a composed one
			// (`TypeA & { ... }`) rather than the single name this rule renames.
			//
			// Caught by the tree rather than by review: without this the rule reported
			// `MenuItem.tsx`, whose parameter destructures and is typed `MenuItemInterface`. That
			// is a genuine violation of the convention and the gate does not report it, so
			// reporting it here would have been a second only-verify divergence, arrived at by
			// accident rather than by a ruling.
			parameterName := parameters[0].AsParameterDeclaration().Name()
			if parameterName == nil || parameterName.Kind != ast.KindIdentifier {
				return
			}

			typeName := typeReferenceName(parameterTypeNode(parameters[0]))
			if typeName == "" || strings.HasSuffix(typeName, "Properties") {
				return
			}

			componentPropertyTypes[typeName] = true

			// Reported on the type reference rather than the parameter, so the finding underlines
			// the name that has to change.
			typeNode := parameterTypeNode(parameters[0])
			ctx.ReportNodeWithFixes(typeNode.AsTypeReferenceNode().TypeName,
				messageUseComponentPropertyTypeSuffix,
				ctx.ReplaceNode(typeNode.AsTypeReferenceNode().TypeName, propertiesTypeNameFor(typeName)))
		}

		reportDeclaration := func(name *ast.Node, message rule.Message) {
			if name == nil || name.Kind != ast.KindIdentifier {
				return
			}
			if !componentPropertyTypes[name.Text()] || strings.HasSuffix(name.Text(), "Properties") {
				return
			}
			ctx.ReportNodeWithFixes(name, message, ctx.ReplaceNode(name, propertiesTypeNameFor(name.Text())))
		}

		return rule.Listeners{
			ast.KindFunctionDeclaration: func(node *ast.Node) {
				declaration := node.AsFunctionDeclaration()
				name := declaration.Name()
				if name == nil || !IsLikelyComponentName(name.Text()) {
					return
				}
				checkParameterType(parameterNodes(declaration.Parameters))
			},

			ast.KindVariableDeclaration: func(node *ast.Node) {
				declaration := node.AsVariableDeclaration()
				name := declaration.Name()
				if name == nil || name.Kind != ast.KindIdentifier || !IsLikelyComponentName(name.Text()) {
					return
				}
				initializer := ast.SkipParentheses(declaration.Initializer)
				if initializer == nil {
					return
				}
				switch initializer.Kind {
				case ast.KindArrowFunction:
					checkParameterType(parameterNodes(initializer.AsArrowFunction().Parameters))
				case ast.KindFunctionExpression:
					checkParameterType(parameterNodes(initializer.AsFunctionExpression().Parameters))
				}
			},

			ast.KindInterfaceDeclaration: func(node *ast.Node) {
				reportDeclaration(node.AsInterfaceDeclaration().Name(), messageUseInterfaceSuffix)
			},

			ast.KindTypeAliasDeclaration: func(node *ast.Node) {
				reportDeclaration(node.AsTypeAliasDeclaration().Name(), messageUseTypeAliasSuffix)
			},
		}
	},
}

// propertiesTypeNameFor derives the corrected name rather than appending to the wrong one.
//
// `ButtonProps` and `ButtonInterface` both become `ButtonProperties`. Appending would produce
// `ButtonPropsProperties`, which nobody wants and which the author would have to fix by hand,
// making the fix worse than no fix.
func propertiesTypeNameFor(name string) string {
	for _, suffix := range []string{"Interface", "Props"} {
		if strings.HasSuffix(name, suffix) {
			return strings.TrimSuffix(name, suffix) + "Properties"
		}
	}
	return name + "Properties"
}
