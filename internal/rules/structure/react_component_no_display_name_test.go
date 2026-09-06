package structure

import (
	"testing"

	"github.com/system-inc/cohere/internal/rule_testing"
)

const displayNameFile = "/repository/source/components/Field.tsx"

func TestReactComponentNoDisplayNameFires(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		sourceText string
		wantCount  int
	}{
		{
			"a function component assigning its own name",
			"export function Field() { return null; }\nField.displayName = 'Field';\n",
			1,
		},
		{
			"an arrow component assigning its name",
			"export const Field = () => null;\nField.displayName = 'Field';\n",
			1,
		},
		// The exemption is for wrappers that produce something anonymous. An ordinary call does
		// not, so a binding initialized from one is not exempt.
		{
			"a binding initialized from an unrelated call",
			"declare function build(): unknown;\nexport const Field = build();\nField.displayName = 'Field';\n",
			1,
		},
		// A wrapper name reached off something other than React is not the React wrapper.
		{
			"memo reached off a receiver that is not React",
			"declare const Other: { memo(component: unknown): unknown };\n" +
				"export const Field = Other.memo(function () { return null; });\nField.displayName = 'Field';\n",
			1,
		},
		{
			"two assignments report twice",
			"export function A() { return null; }\nexport function B() { return null; }\n" +
				"A.displayName = 'A';\nB.displayName = 'B';\n",
			2,
		},
		{
			"an assignment to a member of an object",
			"declare const components: { Field: { displayName?: string } };\ncomponents.Field.displayName = 'Field';\n",
			1,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := rule_testing.Run(t, ReactComponentNoDisplayName, displayNameFile, testCase.sourceText)
			ids := result.MessageIds()
			if len(ids) != testCase.wantCount {
				t.Fatalf("expected %d findings, got %d: %v", testCase.wantCount, len(ids), ids)
			}
		})
	}
}

func TestReactComponentNoDisplayNameStaysSilent(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		sourceText string
	}{
		// The shape the rule asks for: no assignment at all, because the name is already there.
		{
			"a component that does not assign displayName",
			"export function Field() { return null; }\n",
		},
		// The exemption, in each of the four wrapper spellings. A component built by one of these
		// has no name of its own, so the assignment is the only way to label it.
		{
			"a bare memo wrapper",
			"declare function memo(component: unknown): unknown;\n" +
				"export const Field = memo(function () { return null; });\nField.displayName = 'Field';\n",
		},
		{
			"a React.memo wrapper",
			"import React from 'react';\n" +
				"export const Field = React.memo(function () { return null; });\nField.displayName = 'Field';\n",
		},
		{
			"a bare forwardRef wrapper",
			"declare function forwardRef(component: unknown): unknown;\n" +
				"export const Field = forwardRef(function () { return null; });\nField.displayName = 'Field';\n",
		},
		{
			"a React.forwardRef wrapper",
			"import React from 'react';\n" +
				"export const Field = React.forwardRef(function () { return null; });\nField.displayName = 'Field';\n",
		},
		// A compound assignment is not establishing a name, and reporting it would be a claim about
		// string arithmetic rather than about naming.
		{
			"a compound assignment",
			"declare const Field: { displayName: string };\nField.displayName += 'x';\n",
		},
		// A different property entirely.
		{
			"an assignment to a property that is not displayName",
			"export function Field() { return null; }\nField.propertyTypes = {};\n",
		},
		// Reading displayName is not assigning it.
		{
			"reading displayName",
			"declare const Field: { displayName: string };\nexport const name = Field.displayName;\n",
		},
		{
			"a file with no assignments",
			"export const value = 1;\n",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			rule_testing.ExpectClean(t, rule_testing.Run(t, ReactComponentNoDisplayName, displayNameFile, testCase.sourceText))
		})
	}
}

// Declaration order decides the exemption, and this pins which way.
//
// The rule collects wrapper-initialized bindings during the walk and reads that set at the
// assignment, so a binding declared after the assignment does not exempt it. Worth a test of its
// own rather than a comment, because it is the one behavior that depends on the traversal order
// rather than on any single node, and a change to how listeners are dispatched would break it
// silently.
//
// The limit is the right one: an assignment above `const Field = memo(...)` is a temporal dead
// zone error at runtime, so exempting it would excuse code that cannot run.
func TestReactComponentNoDisplayNameDependsOnDeclarationOrder(t *testing.T) {
	t.Parallel()

	declaredFirst := "declare function memo(component: unknown): unknown;\n" +
		"export const Field = memo(function () { return null; });\nField.displayName = 'Field';\n"
	rule_testing.ExpectClean(t, rule_testing.Run(t, ReactComponentNoDisplayName, displayNameFile, declaredFirst))

	assignedFirst := "declare function memo(component: unknown): unknown;\n" +
		"Field.displayName = 'Field';\nexport const Field = memo(function () { return null; });\n"
	rule_testing.ExpectFindings(t, rule_testing.Run(t, ReactComponentNoDisplayName, displayNameFile, assignedFirst),
		"noDisplayNameAssignment")
}
