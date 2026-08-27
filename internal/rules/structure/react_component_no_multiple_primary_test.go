package structure

import (
	"strings"
	"testing"

	"github.com/system-inc/verify/internal/rule_testing"
)

const componentFile = "/repository/source/components/Thing.tsx"

// component builds a component of a requested size, so a test can say "a 70-line component" and
// have the bands mean what they say rather than depending on the shape of a hand-written example.
//
// Every line carries code, since the rule skips blanks and comment lines when it counts.
func component(name string, codeLines int) string {
	var builder strings.Builder
	builder.WriteString("export function " + name + "(properties: { value: number }) {\n")
	// Two lines are spent on the signature and the closing brace, and two more on the return and
	// its closing paren, so the filler makes up the difference.
	for filler := 0; filler < codeLines-4; filler++ {
		builder.WriteString("    const value" + itoa(filler) + " = properties.value + " + itoa(filler) + ";\n")
	}
	builder.WriteString("    return (\n        <div>{properties.value}</div>\n    );\n}\n")
	return builder.String()
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}

func TestReactComponentNoMultiplePrimaryFires(t *testing.T) {
	cases := []struct {
		name       string
		sourceText string
		wantIds    []string
	}{
		// The core case: a large component and a medium one in the same file. The medium one is
		// what should move, so it is what gets reported.
		{
			"a large component and a medium one",
			component("Primary", 80) + component("Secondary", 30),
			[]string{"noMultiplePrimary"},
		},
		// No large component needed. Two mediums are still two things a reader has to find.
		{
			"two medium components with a large one present",
			component("Primary", 80) + component("First", 30) + component("Second", 40),
			[]string{"noMultiplePrimary", "noMultiplePrimary"},
		},
		// Every component after the first is reported, not just the second, because the finding is
		// attached to the thing that should move.
		{
			"three non-small components report twice",
			component("Primary", 80) + component("Second", 30) + component("Third", 40),
			[]string{"noMultiplePrimary", "noMultiplePrimary"},
		},
		// A component declared inside another one still counts. The original's visitor fires at any
		// depth, and four real findings on the ahra tree are exactly this shape.
		{
			"a component nested inside another",
			"export function Primary(properties: { value: number }) {\n" +
				"    function InnerCell(cellProperties: { row: number }) {\n" +
				"        const a = cellProperties.row;\n        const b = a + 1;\n" +
				"        const c = b + 1;\n        const d = c + 1;\n        const e = d + 1;\n" +
				"        const f = e + 1;\n        const g = f + 1;\n        const h = g + 1;\n" +
				"        const i = h + 1;\n        const j = i + 1;\n" +
				"        return <span>{j}</span>;\n    }\n" +
				strings.Repeat("    const filler = properties.value;\n", 70) +
				"    return (\n        <div><InnerCell row={1} /></div>\n    );\n}\n",
			[]string{"noMultiplePrimary"},
		},

		// A hook call makes a function a component even with no JSX in it. That is the original's
		// rule, and it is what catches a component whose whole body is state and effects.
		{
			"a component detected by its hook call, not by JSX",
			component("Primary", 80) +
				"export function Secondary(properties: { value: number }) {\n" +
				strings.Repeat("    const filler = properties.value;\n", 28) +
				"    const [state] = useState(0);\n    return state;\n}\n",
			[]string{"noMultiplePrimary"},
		},
		// The namespaced spelling reaches the same conclusion by a different path in the detector,
		// so it is proven separately rather than assumed to follow.
		{
			"a component detected by a React-namespaced hook call",
			component("Primary", 80) +
				"export function Secondary(properties: { value: number }) {\n" +
				strings.Repeat("    const filler = properties.value;\n", 28) +
				"    const [state] = React.useState(0);\n    return state;\n}\n",
			[]string{"noMultiplePrimary"},
		},

		// The tooManyHelpers half. The tree has zero of these, so it is proven here or nowhere.
		//
		// It needs a large component present and more than three small ones; the fourth small
		// component onward is reported.
		{
			"a large component with four small helpers",
			component("Primary", 80) +
				component("HelperA", 6) + component("HelperB", 6) +
				component("HelperC", 6) + component("HelperD", 6),
			[]string{"tooManyHelpers"},
		},
		{
			"a large component with six small helpers reports three",
			component("Primary", 80) +
				component("HelperA", 6) + component("HelperB", 6) + component("HelperC", 6) +
				component("HelperD", 6) + component("HelperE", 6) + component("HelperF", 6),
			[]string{"tooManyHelpers", "tooManyHelpers", "tooManyHelpers"},
		},
		// Both halves at once, which is the shape neither half alone would catch.
		{
			"a large component, a medium one, and four small helpers",
			component("Primary", 80) + component("Secondary", 30) +
				component("HelperA", 6) + component("HelperB", 6) +
				component("HelperC", 6) + component("HelperD", 6),
			[]string{"noMultiplePrimary", "tooManyHelpers"},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := rule_testing.Run(t, ReactComponentNoMultiplePrimary, componentFile, testCase.sourceText)
			rule_testing.ExpectFindings(t, result, testCase.wantIds...)
		})
	}
}

func TestReactComponentNoMultiplePrimaryStaysSilent(t *testing.T) {
	cases := []struct {
		name       string
		fileName   string
		sourceText string
	}{
		{"one large component alone", componentFile, component("Primary", 80)},
		{"one medium component alone", componentFile, component("Primary", 30)},

		// A helper read beside its only caller is easier than a helper in a file of its own, so
		// small company is the point rather than a violation.
		{
			"a large component with three small helpers", componentFile,
			component("Primary", 80) +
				component("HelperA", 6) + component("HelperB", 6) + component("HelperC", 6),
		},
		// Only when something large is present. A crowd of helpers around a medium component is a
		// file of small parts, which is not what the helper clause is about.
		{
			"a medium component with six small helpers", componentFile,
			component("Primary", 30) +
				component("HelperA", 6) + component("HelperB", 6) + component("HelperC", 6) +
				component("HelperD", 6) + component("HelperE", 6) + component("HelperF", 6),
		},
		// A file of nothing but small components is fine however many there are.
		{
			"five small components and nothing else", componentFile,
			component("A", 6) + component("B", 6) + component("C", 6) +
				component("D", 6) + component("E", 6),
		},

		// JSX is what makes a component file, so a .ts file is declined before a node is visited.
		{
			"a .ts file is not a React file", "/repository/source/Thing.ts",
			component("Primary", 80) + component("Secondary", 30),
		},
		// A Next.js special file's shape is the framework's contract rather than an authoring
		// choice, so the rule has no standing in one.
		{
			"a Next.js page file", "/repository/app/dashboard/page.tsx",
			component("Primary", 80) + component("Secondary", 30),
		},
		{
			"a Next.js layout file", "/repository/app/dashboard/layout.tsx",
			component("Primary", 80) + component("Secondary", 30),
		},
		{
			"a Next.js route file", "/repository/app/api/things/route.ts",
			component("Primary", 80) + component("Secondary", 30),
		},

		// A lowercase name is not a component. JSX decides by the capital, so the capital is the
		// author's claim and its absence is the author declining to make one.
		{
			"a lowercase function is not a component", componentFile,
			component("Primary", 80) + strings.Replace(component("Secondary", 30), "Secondary", "secondary", 1),
		},
		// A PascalCase function with no JSX and no hook is not a component either.
		{
			"a PascalCase function with no JSX", componentFile,
			component("Primary", 80) +
				"export function ComputeTotal(values: number[]) {\n" +
				strings.Repeat("    const filler = values.length;\n", 30) +
				"    return values.length;\n}\n",
		},
		// A file-local const holding a component is a helper by construction: nothing outside can
		// reach it, so it is not the thing an importer came for.
		{
			"a file-local const component is not counted", componentFile,
			component("Primary", 80) +
				"const Secondary = (properties: { value: number }) => {\n" +
				strings.Repeat("    const filler = properties.value;\n", 30) +
				"    return <div>{properties.value}</div>;\n};\n",
		},

		// The switch blind spot, which is a property of the gate this rule must match rather than a
		// bug to fix here. ESTree puts a switch's arms on `cases`, which the original does not walk,
		// so a component returning JSX only from switch arms does not read as a component to it.
		// Two real components on the ahra tree are this shape and the gate reports neither.
		{
			"jsx only in switch arms is invisible to the gate", componentFile,
			component("Primary", 80) +
				"export function Dispatch(properties: { kind: string }) {\n" +
				"    switch(properties.kind) {\n" +
				strings.Repeat("        case 'a': return <span />;\n", 30) +
				"    }\n    return null;\n}\n",
		},

		// The namespace matters. A use-prefixed method on something other than React is an
		// ordinary call, and reading it as a hook would count functions the gate does not.
		{
			"a use-prefixed call on a non-React object is not a hook", componentFile,
			component("Primary", 80) +
				"export function ComputeTotal(helpers: { useCache: () => number }) {\n" +
				strings.Repeat("    const filler = helpers.useCache;\n", 28) +
				"    return helpers.useCache();\n}\n",
		},
		// The uppercase letter after "use" is what React's own linting keys on, so a lowercase
		// continuation is an ordinary function. Without the check, "used" and "user" read as hooks
		// and any function calling one becomes a component.
		{
			"a lowercase use-prefixed call is not a hook", componentFile,
			component("Primary", 80) +
				"declare function used(value: number): number;\n" +
				"export function ComputeTotal(values: number[]) {\n" +
				strings.Repeat("    const filler = values.length;\n", 28) +
				"    return used(values.length);\n}\n",
		},

		{"no components at all", componentFile, "export const value = 1;\n"},

		// The case the gate used to report, and the reason it is here rather than beside the firing
		// cases. Two components over the helper size and nothing large is a file the rule never
		// meant to name: it would have printed "a component with 40 lines" against a threshold of
		// 60, a number below the one the same sentence cites. Measured before the repair, 39 of 52
		// findings on the ahra tree were this shape, five of them naming 11 lines.
		//
		// This must stay on the quiet side rather than be deleted, because the gate that fixes it is
		// one condition and a fixture asserting the wrong half is what encoded the defect in the
		// first place.
		{
			"two medium components and nothing large", componentFile,
			component("First", 30) + component("Second", 40),
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := rule_testing.Run(t, ReactComponentNoMultiplePrimary, testCase.fileName, testCase.sourceText)
			rule_testing.ExpectClean(t, result)
		})
	}
}

// The counter skips blanks and comment lines, and it anchors past leading trivia. Both disciplines
// are proven by putting a helper one line under the small-component ceiling and burying it in
// comments: with either discipline missing it crosses into the medium band and reports.
//
// The margin is the whole test. An earlier version used a four-line helper under a six-line comment
// block, which stayed small either way, so breaking the counter left it green. A control that cannot
// move a component across a band boundary measures nothing.
func TestReactComponentNoMultiplePrimaryCountsCodeLinesOnly(t *testing.T) {
	// Ten code lines, which is exactly maximumHelperLines and the largest a helper may be.
	helperBody := "export function Helper(properties: { value: number }) {\n"
	for filler := 0; filler < 7; filler++ {
		helperBody += "    const value" + itoa(filler) + " = properties.value;\n"
	}
	helperBody += "    return <span>{properties.value}</span>;\n}\n"

	// Comments and blanks around it, enough that counting any of them tips it past the ceiling.
	documented := "// A comment block above the helper.\n//\n//\n//\n//\n//\n//\n//\n" +
		helperBody

	t.Run("clean", func(t *testing.T) {
		rule_testing.ExpectClean(t, rule_testing.Run(t, ReactComponentNoMultiplePrimary, componentFile,
			component("Primary", 80)+documented))
	})

	// The same helper with interior blanks and a block comment, which the counter also skips.
	t.Run("interior comments and blanks", func(t *testing.T) {
		interior := "export function Helper(properties: { value: number }) {\n\n" +
			"    /*\n     * An interior block comment.\n     */\n\n"
		for filler := 0; filler < 6; filler++ {
			interior += "    const value" + itoa(filler) + " = properties.value;\n\n"
		}
		interior += "    return <span>{properties.value}</span>;\n}\n"
		rule_testing.ExpectClean(t, rule_testing.Run(t, ReactComponentNoMultiplePrimary, componentFile,
			component("Primary", 80)+interior))
	})
}

// The depth limit is behavior, not a safety valve, so it is pinned rather than assumed.
//
// The original bounds its walk at depth 20 and returns false past it, which means JSX buried deeper
// than that does not make a function a component to the gate verify replaces. An unbounded Go walk
// would find it and report a finding the gate does not have, so the bound ports with the rule.
//
// The nesting here is deliberate rather than realistic: this shape does not occur on our tree, which
// is exactly why it needs a fixture. Nothing else would notice if the limit were removed.
func TestReactComponentNoMultiplePrimaryRespectsTheDepthLimit(t *testing.T) {
	// JSX wrapped in enough parentheses to sit past depth 20.
	deeplyBuried := "export function Buried(properties: { value: number }) {\n" +
		strings.Repeat("    const filler = properties.value;\n", 28) +
		"    return " + strings.Repeat("(", 40) + "<span />" + strings.Repeat(")", 40) + ";\n}\n"

	// The primary is a real component, so the file has one. The buried one does not read as a
	// component to the gate, so there is nothing to report.
	rule_testing.ExpectClean(t, rule_testing.Run(t, ReactComponentNoMultiplePrimary, componentFile,
		component("Primary", 80)+deeplyBuried))

	// The same component with its JSX at a reachable depth does read as one, which is what proves
	// the test above is measuring the depth rather than something else about the shape.
	shallow := "export function Shallow(properties: { value: number }) {\n" +
		strings.Repeat("    const filler = properties.value;\n", 28) +
		"    return (<span />);\n}\n"
	rule_testing.ExpectFindings(t, rule_testing.Run(t, ReactComponentNoMultiplePrimary, componentFile,
		component("Primary", 80)+shallow), "noMultiplePrimary")
}

// The thresholds are options, so a project may move them. This proves they are read rather than
// ignored, in both directions.
func TestReactComponentNoMultiplePrimaryOptions(t *testing.T) {
	source := component("Primary", 80) + component("Secondary", 30)

	// Raising the helper ceiling above the secondary's size makes it a helper, and one large
	// component with one helper is a quiet file.
	relaxed := rule_testing.RunWithOptions(t, ReactComponentNoMultiplePrimary, componentFile, source,
		ReactComponentNoMultiplePrimaryOptions{MaximumComponentLines: 60, MaximumHelperLines: 40})
	rule_testing.ExpectClean(t, relaxed)

	// Lowering the component ceiling makes both components large, which is still two primaries.
	strict := rule_testing.RunWithOptions(t, ReactComponentNoMultiplePrimary, componentFile, source,
		ReactComponentNoMultiplePrimaryOptions{MaximumComponentLines: 10, MaximumHelperLines: 5})
	rule_testing.ExpectFindings(t, strict, "noMultiplePrimary")
}
