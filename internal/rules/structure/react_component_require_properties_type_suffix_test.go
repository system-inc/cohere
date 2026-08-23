package structure

import (
	"testing"

	"github.com/system-inc/verify/internal/ruletest"
)

const typeSuffixFile = "/repository/source/components/Button.tsx"

func TestReactComponentRequirePropertiesTypeSuffixFires(t *testing.T) {
	cases := []struct {
		name       string
		sourceText string
		wantIds    []string
	}{
		{
			"a component whose type is declared elsewhere",
			"import type { ButtonProps } from './Types';\n" +
				"export function Button(properties: ButtonProps) {\n    return <button>{properties.label}</button>;\n}\n",
			[]string{"useComponentPropertyTypeSuffix"},
		},
		{
			"an arrow component",
			"import type { ButtonProps } from './Types';\n" +
				"export const Button = (properties: ButtonProps) => <button>{properties.label}</button>;\n",
			[]string{"useComponentPropertyTypeSuffix"},
		},
		// Two findings for one wrong name, at the use and at the declaration. A reader fixing this
		// touches both, and seeing only one would leave the rename half done.
		{
			"an interface declared after the component",
			"export function Button(properties: ButtonInterface) {\n" +
				"    return <button>{properties.label}</button>;\n}\n" +
				"interface ButtonInterface { label: string }\n",
			[]string{"useComponentPropertyTypeSuffix", "useInterfaceSuffix"},
		},
		{
			"a type alias declared after the component",
			"export function Button(properties: ButtonProps) {\n" +
				"    return <button>{properties.label}</button>;\n}\n" +
				"type ButtonProps = { label: string };\n",
			[]string{"useComponentPropertyTypeSuffix", "useTypeAliasSuffix"},
		},
		// A name holding Properties as a substring rather than a suffix is still wrong.
		{
			"a type with Properties in the middle",
			"import type { ButtonPropertiesBag } from './Types';\n" +
				"export function Button(properties: ButtonPropertiesBag) {\n" +
				"    return <button>{properties.label}</button>;\n}\n",
			[]string{"useComponentPropertyTypeSuffix"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			ruletest.ExpectFindings(t, ruletest.Run(t, ReactComponentRequirePropertiesTypeSuffix,
				typeSuffixFile, testCase.sourceText), testCase.wantIds...)
		})
	}
}

func TestReactComponentRequirePropertiesTypeSuffixStaysSilent(t *testing.T) {
	cases := []struct {
		name       string
		fileName   string
		sourceText string
	}{
		// The shape the rule asks for.
		{
			"a correctly suffixed type",
			typeSuffixFile,
			"interface ButtonProperties { label: string }\n" +
				"export function Button(properties: ButtonProperties) {\n" +
				"    return <button>{properties.label}</button>;\n}\n",
		},
		// The component-name gate. A lowercase function's parameter type is nobody's business.
		{
			"a lowercase function taking a badly named type",
			typeSuffixFile,
			"import type { ButtonProps } from './Types';\n" +
				"export function build(properties: ButtonProps) {\n    return <button />;\n}\n",
		},
		// An inline type has no name to judge, and a destructured parameter has no type reference.
		{
			"an inline object type",
			typeSuffixFile,
			"export function Button(properties: { label: string }) {\n" +
				"    return <button>{properties.label}</button>;\n}\n",
		},
		{
			"a destructured parameter",
			typeSuffixFile,
			"export function Button({ label }: { label: string }) {\n    return <button>{label}</button>;\n}\n",
		},
		// A destructured parameter with a badly named type reference. This is the shape the tree
		// actually has, in MenuItem.tsx, and the rule reported it until the identifier check was
		// added: a genuine violation of the convention that the gate does not report, which would
		// have become a second only-verify divergence arrived at by accident rather than a ruling.
		//
		// Destructuring is `react-component-no-destructuring`'s business, and a destructured
		// parameter's type is often composed (`TypeA & { ... }`) rather than the single name this
		// rule renames.
		{
			"a destructured parameter with a badly named type",
			typeSuffixFile,
			"import type { MenuItemInterface } from './Types';\n" +
				"export function MenuItem({ label, ...buttonProperties }: MenuItemInterface) {\n" +
				"    return <button {...buttonProperties}>{label}</button>;\n}\n",
		},
		{
			"a component with no parameters",
			typeSuffixFile,
			"export function Button() {\n    return <button />;\n}\n",
		},
		// An interface not used as a component's property type is not this rule's business, even
		// when it is badly named.
		{
			"an unrelated interface",
			typeSuffixFile,
			"interface ButtonProps { label: string }\n" +
				"export function Button(properties: { label: string }) {\n" +
				"    return <button>{properties.label}</button>;\n}\n",
		},
		// Outside a React file the rule declines before looking.
		{
			"a component-shaped function in a ts file",
			"/repository/source/api/Thing.ts",
			"import React from 'react';\nimport type { ButtonProps } from './Types';\n" +
				"export function Button(properties: ButtonProps) {\n" +
				"    React.useEffect(() => {}, []);\n    return properties;\n}\n",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			ruletest.ExpectClean(t, ruletest.Run(t, ReactComponentRequirePropertiesTypeSuffix,
				testCase.fileName, testCase.sourceText))
		})
	}
}

// Declaration order decides whether the declaration itself is reported.
//
// The declaration visitors read a set the parameter visitor fills, so an interface declared before
// the component that uses it is not in the set when its own visitor runs. That is the original's
// behavior, reproduced rather than corrected, since the use site is reported either way and the
// alternative walks the file twice for a second finding about the same rename.
//
// Pinned as a test rather than left as a comment because it is the one behavior depending on
// traversal order rather than on any single node.
func TestReactComponentRequirePropertiesTypeSuffixDependsOnDeclarationOrder(t *testing.T) {
	after := "export function Button(properties: ButtonInterface) {\n" +
		"    return <button>{properties.label}</button>;\n}\n" +
		"interface ButtonInterface { label: string }\n"
	ruletest.ExpectFindings(t, ruletest.Run(t, ReactComponentRequirePropertiesTypeSuffix, typeSuffixFile, after),
		"useComponentPropertyTypeSuffix", "useInterfaceSuffix")

	before := "interface ButtonInterface { label: string }\n" +
		"export function Button(properties: ButtonInterface) {\n" +
		"    return <button>{properties.label}</button>;\n}\n"
	ruletest.ExpectFindings(t, ruletest.Run(t, ReactComponentRequirePropertiesTypeSuffix, typeSuffixFile, before),
		"useComponentPropertyTypeSuffix")
}

// The fix derives the corrected name rather than appending to the wrong one.
func TestReactComponentRequirePropertiesTypeSuffixFixDerivesTheName(t *testing.T) {
	ruletest.ExpectFixedSource(t,
		ruletest.Run(t, ReactComponentRequirePropertiesTypeSuffix, typeSuffixFile,
			"import type { ButtonProps } from './Types';\n"+
				"export function Button(properties: ButtonProps) {\n    return <button />;\n}\n"),
		"import type { ButtonProps } from './Types';\n"+
			"export function Button(properties: ButtonProperties) {\n    return <button />;\n}\n")

	// An Interface suffix is replaced rather than appended to, which is the case that separates a
	// derived rename from a blind one: appending gives ButtonInterfaceProperties.
	ruletest.ExpectFixedSource(t,
		ruletest.Run(t, ReactComponentRequirePropertiesTypeSuffix, typeSuffixFile,
			"import type { ButtonInterface } from './Types';\n"+
				"export function Button(properties: ButtonInterface) {\n    return <button />;\n}\n"),
		"import type { ButtonInterface } from './Types';\n"+
			"export function Button(properties: ButtonProperties) {\n    return <button />;\n}\n")

	// A name with no known suffix gets one appended.
	ruletest.ExpectFixedSource(t,
		ruletest.Run(t, ReactComponentRequirePropertiesTypeSuffix, typeSuffixFile,
			"import type { ButtonBag } from './Types';\n"+
				"export function Button(properties: ButtonBag) {\n    return <button />;\n}\n"),
		"import type { ButtonBag } from './Types';\n"+
			"export function Button(properties: ButtonBagProperties) {\n    return <button />;\n}\n")
}
