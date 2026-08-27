package structure

import (
	"testing"

	"github.com/system-inc/verify/internal/rule_testing"
)

const separateExportFile = "/repository/source/components/Button.tsx"

func TestReactComponentNoSeparateNamedExportFires(t *testing.T) {
	cases := []struct {
		name       string
		sourceText string
	}{
		{
			"a function component exported at the bottom",
			"function Button() {\n    return <button />;\n}\nexport { Button };\n",
		},
		{
			"an arrow component exported at the bottom",
			"const Button = () => <button />;\nexport { Button };\n",
		},
		// Reported once per statement rather than per specifier, since deleting the statement is
		// one edit however many names it holds.
		{
			"two components in one export list",
			"function Button() {\n    return <button />;\n}\nfunction Panel() {\n    return <div />;\n}\n" +
				"export { Button, Panel };\n",
		},
		{
			"a component among other exported names",
			"function Button() {\n    return <button />;\n}\nconst value = 1;\nexport { value, Button };\n",
		},
		// A component detected by its hook calls rather than by JSX.
		{
			"a component that calls hooks and returns nothing",
			"import React from 'react';\nfunction Button() {\n    React.useEffect(() => {}, []);\n    return null;\n}\n" +
				"export { Button };\n",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			rule_testing.ExpectFindings(t, rule_testing.Run(t, ReactComponentNoSeparateNamedExport,
				separateExportFile, testCase.sourceText), "noSeparateNamedExport")
		})
	}
}

func TestReactComponentNoSeparateNamedExportStaysSilent(t *testing.T) {
	cases := []struct {
		name       string
		fileName   string
		sourceText string
	}{
		// The shape the rule asks for.
		{
			"an inline export",
			separateExportFile,
			"export function Button() {\n    return <button />;\n}\n",
		},
		// Every case below declares a component, deliberately.
		//
		// The rule returns no listeners at all when a file declares none, so a clean fixture
		// without one is silenced by that early return before reaching the check it was written
		// for. Six mutants survived the first sweep for exactly this reason: the guard subsumed
		// every branch the fixtures meant to exercise, and from a green suite that is
		// indistinguishable from the branches working.
		//
		// So each of these carries an unrelated component that is correctly exported inline,
		// which satisfies the guard and lets the case reach the branch it is actually about.

		// A re-export has somewhere else to be, so there is nothing to move the export onto. This
		// is the shape an index file uses and it is the one the tree actually contains.
		// The re-exported name must match a declared component for the exemption to be reached at
		// all. With an unrelated name the declared-component check silences it first, which is why
		// the second case here shadows the declaration's own name.
		{
			"a re-export from another module",
			separateExportFile,
			"export function Panel() {\n    return <div />;\n}\nexport { Button } from './Button';\n",
		},
		{
			"a re-export naming a component this file also declares",
			separateExportFile,
			"function Button() {\n    return <button />;\n}\nexport { Button } from './Other';\n",
		},
		{
			"several re-exports",
			separateExportFile,
			"export function Panel() {\n    return <div />;\n}\n" +
				"export { PaginationButton } from './PaginationButton';\n" +
				"export { PaginationItem } from './PaginationItem';\n",
		},
		// Only components. Export style for types and constants is a different question this rule
		// has no opinion on.
		{
			"a constant exported at the bottom",
			separateExportFile,
			"export function Panel() {\n    return <div />;\n}\nconst value = 1;\nexport { value };\n",
		},
		// A capitalized function that renders nothing is not a component, so exporting it from a
		// list is not this rule's business. Needs the same name in both places to reach the render
		// check rather than being stopped by the name check.
		{
			"a capitalized function that renders nothing",
			separateExportFile,
			"export function Panel() {\n    return <div />;\n}\nfunction Build() {\n    return 1;\n}\nexport { Build };\n",
		},
		// A lowercase name is not a component whatever it renders, in both the function and the
		// variable branch. The variable one is the case that measures the name check inside
		// componentNamesDeclaredIn: the function branch has its own.
		{
			"a lowercase function returning JSX",
			separateExportFile,
			"export function Panel() {\n    return <div />;\n}\nfunction build() {\n    return <div />;\n}\nexport { build };\n",
		},
		{
			"a lowercase arrow returning JSX",
			separateExportFile,
			"export function Panel() {\n    return <div />;\n}\nconst build = () => <div />;\nexport { build };\n",
		},
		{
			"a capitalized constant that is not a component",
			separateExportFile,
			"export function Panel() {\n    return <div />;\n}\nconst Colors = 'red';\nexport { Colors };\n",
		},
		{
			"a type exported at the bottom",
			separateExportFile,
			"export function Panel() {\n    return <div />;\n}\n" +
				"type ButtonProperties = { label: string };\nexport type { ButtonProperties };\n",
		},
		// A name the file does not declare cannot have its export moved onto a declaration here.
		{
			"a name declared elsewhere",
			separateExportFile,
			"export function Panel() {\n    return <div />;\n}\n" +
				"import { Button } from './Button';\nexport { Button };\n",
		},
		// A star re-export names nothing, so there is no specifier to match.
		{
			"a star re-export",
			separateExportFile,
			"export function Panel() {\n    return <div />;\n}\nexport * from './Other';\n",
		},
		// Outside a React file the rule declines before looking.
		{
			"a component-shaped export in a ts file",
			"/repository/source/api/Thing.ts",
			"import React from 'react';\nfunction Button() {\n    React.useEffect(() => {}, []);\n    return null;\n}\n" +
				"export { Button };\n",
		},
		{
			"a file with no export list",
			separateExportFile,
			"export function Button() {\n    return <button />;\n}\n",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			rule_testing.ExpectClean(t, rule_testing.Run(t, ReactComponentNoSeparateNamedExport,
				testCase.fileName, testCase.sourceText))
		})
	}
}

// The fix deletes the export statement and nothing else.
//
// Asserted against the resulting source rather than against the message id, using the assertion
// `@system_verify` added after finding that an id-only fixture let a rewrite corrupt every import
// it touched while the suite stayed green. A fix is the one part of a rule that changes source, so
// the only honest test of it is what the source becomes.
//
// The blank line the deletion leaves behind is real and expected. `RemoveNode` takes the node's
// range, which ends at the semicolon rather than at the newline after it, so the line the statement
// occupied becomes empty. The formatter collapses it on the next pass.
//
// Worth writing down because I expected the clean text and this assertion is what told me
// otherwise, on the first fixable rule I have written since it existed. An id-only fixture would
// have agreed with me.
func TestReactComponentNoSeparateNamedExportFixDeletesTheStatement(t *testing.T) {
	source := "function Button() {\n    return <button />;\n}\nexport { Button };\n"
	rule_testing.ExpectFixedSource(t,
		rule_testing.Run(t, ReactComponentNoSeparateNamedExport, separateExportFile, source),
		"function Button() {\n    return <button />;\n}\n\n")

	// A list holding several names goes as one statement, which is what makes the single report
	// per statement the right shape.
	multiple := "function Button() {\n    return <button />;\n}\nfunction Panel() {\n    return <div />;\n}\n" +
		"export { Button, Panel };\n"
	rule_testing.ExpectFixedSource(t,
		rule_testing.Run(t, ReactComponentNoSeparateNamedExport, separateExportFile, multiple),
		"function Button() {\n    return <button />;\n}\nfunction Panel() {\n    return <div />;\n}\n\n")
}
