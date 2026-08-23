package structure

import (
	"testing"

	"github.com/system-inc/verify/internal/ruletest"
)

const propertiesParameterFile = "/repository/source/components/Button.tsx"

func TestReactComponentRequirePropertiesParameterFires(t *testing.T) {
	cases := []struct {
		name       string
		sourceText string
	}{
		{
			"a function component",
			"export function Button(props: { label: string }) {\n    return <button>{props.label}</button>;\n}\n",
		},
		{
			"an arrow component",
			"export const Button = (props: { label: string }) => <button>{props.label}</button>;\n",
		},
		{
			"a function expression component",
			"export const Button = function(props: { label: string }) {\n" +
				"    return <button>{props.label}</button>;\n};\n",
		},
		// Detected by hook calls rather than JSX, which is the other half of what makes a
		// component.
		{
			"a component that calls hooks and renders nothing",
			"import React from 'react';\n" +
				"export function Button(props: { label: string }) {\n" +
				"    React.useEffect(() => {}, []);\n    return props.label;\n}\n",
		},
		// A lowercase name is not judged by this rule at all, since it never checks the name: what
		// makes something a component here is that it renders.
		{
			"a lowercase function that renders",
			"export function renderButton(props: { label: string }) {\n" +
				"    return <button>{props.label}</button>;\n}\n",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			ruletest.ExpectFindings(t, ruletest.Run(t, ReactComponentRequirePropertiesParameter,
				propertiesParameterFile, testCase.sourceText), "usePropertiesNotProps")
		})
	}
}

func TestReactComponentRequirePropertiesParameterStaysSilent(t *testing.T) {
	cases := []struct {
		name       string
		fileName   string
		sourceText string
	}{
		// The shape the rule asks for.
		{
			"a component named properties",
			propertiesParameterFile,
			"export function Button(properties: { label: string }) {\n" +
				"    return <button>{properties.label}</button>;\n}\n",
		},
		// A destructured parameter has no single name to judge, and is another rule's business.
		{
			"a destructured parameter",
			propertiesParameterFile,
			"export function Button({ label, ...buttonProperties }: any) {\n" +
				"    return <button {...buttonProperties}>{label}</button>;\n}\n",
		},
		// Any other name is the author's choice. The rule is about the abbreviation specifically,
		// not about enforcing one name.
		{
			"a differently named parameter",
			propertiesParameterFile,
			"export function Button(input: { label: string }) {\n    return <button>{input.label}</button>;\n}\n",
		},
		// A name holding props as a substring is not the abbreviation.
		{
			"a parameter named propsBag",
			propertiesParameterFile,
			"export function Button(propsBag: { label: string }) {\n" +
				"    return <button>{propsBag.label}</button>;\n}\n",
		},
		// The render gate. A function that renders nothing is not a component whatever its
		// parameter is called.
		{
			"a plain function taking props",
			propertiesParameterFile,
			"export function build(props: { label: string }) {\n    return props.label.length;\n}\n",
		},
		// Only the first parameter. A second one named props is not the properties object.
		{
			"props as a second parameter",
			propertiesParameterFile,
			"export function Button(properties: { label: string }, props: unknown) {\n" +
				"    return <button>{properties.label}</button>;\n}\n",
		},
		{
			"a component with no parameters",
			propertiesParameterFile,
			"export function Button() {\n    return <button />;\n}\n",
		},
		// Outside a React file the rule declines before looking at anything.
		//
		// The body has to render for this to measure the file gate. My first version returned a
		// plain string, which the render check stops first, so the case passed whether or not the
		// gate existed. Same shared-guard shape that silenced six fixtures on the previous rule.
		{
			"a component-shaped function in a ts file",
			"/repository/source/api/Thing.ts",
			"import React from 'react';\n" +
				"export function Button(props: { label: string }) {\n" +
				"    React.useEffect(() => {}, []);\n    return props.label;\n}\n",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			ruletest.ExpectClean(t, ruletest.Run(t, ReactComponentRequirePropertiesParameter,
				testCase.fileName, testCase.sourceText))
		})
	}
}

// The fix renames the declaration and stops there.
//
// Asserted against the resulting source. The uses inside the body are deliberately not rewritten,
// which the assertion makes visible rather than leaving to a reader's assumption: the result does
// not compile, and that is the intended failure mode. A rename that guesses which `props` in a body
// are the parameter fails quietly and in the wrong place; one that stops at the declaration fails
// immediately and says exactly where.
func TestReactComponentRequirePropertiesParameterFixRenamesTheDeclaration(t *testing.T) {
	ruletest.ExpectFixedSource(t,
		ruletest.Run(t, ReactComponentRequirePropertiesParameter, propertiesParameterFile,
			"export function Button(props: { label: string }) {\n    return <button>{props.label}</button>;\n}\n"),
		"export function Button(properties: { label: string }) {\n    return <button>{props.label}</button>;\n}\n")
}
