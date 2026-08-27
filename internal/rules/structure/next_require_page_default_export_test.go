package structure

import (
	"testing"

	"github.com/system-inc/verify/internal/rule_testing"
)

const pageFile = "/repository/app/account/settings/page.tsx"
const layoutFile = "/repository/app/account/settings/layout.tsx"

func TestNextRequirePageDefaultExportFires(t *testing.T) {
	cases := []struct {
		name       string
		sourceText string
		wantIds    []string
	}{
		{
			"no default export at all",
			"export function SettingsPageRoute() {\n    return null;\n}\n",
			[]string{"pageRequireDefaultExport"},
		},
		{
			"a file with only named exports",
			"export const value = 1;\nexport function helper() { return 1; }\n",
			[]string{"pageRequireDefaultExport"},
		},
		{
			"an empty page file",
			"",
			[]string{"pageRequireDefaultExport"},
		},
		// `export = x` is the CommonJS form and is not a default export, so a page using it still
		// has no route. Distinguishing the two is what keeps this a missing-export finding rather
		// than a name complaint about an export Next will not read. Found by treating export
		// equals as a default and watching the suite stay green.
		{
			"an export equals rather than a default export",
			"declare function SettingsPageRoute(): null;\nexport = SettingsPageRoute;\n",
			[]string{"pageRequireDefaultExport"},
		},
		// The separate-statement form. Two findings, because the name is wrong as well as the
		// placement, and they are two different edits.
		{
			"a separate default export statement",
			"function Settings() {\n    return null;\n}\nexport default Settings;\n",
			[]string{"pageDefaultExportInline", "pageDefaultExportNameSuffix"},
		},
		{
			"a separate default export of a correctly named function",
			"function SettingsPageRoute() {\n    return null;\n}\nexport default SettingsPageRoute;\n",
			[]string{"pageDefaultExportInline"},
		},
		{
			"an inline default export with the wrong name",
			"export default function Settings() {\n    return null;\n}\n",
			[]string{"pageDefaultExportNameSuffix"},
		},
		// A name holding PageRoute as a substring rather than a suffix is still wrong.
		{
			"a name with PageRoute in the middle",
			"export default function SettingsPageRouteView() {\n    return null;\n}\n",
			[]string{"pageDefaultExportNameSuffix"},
		},
		{
			"a default exported class with the wrong name",
			"export default class Settings {\n    render() { return null; }\n}\n",
			[]string{"pageDefaultExportNameSuffix"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			rule_testing.ExpectFindings(t, rule_testing.Run(t, NextRequirePageDefaultExport, pageFile,
				testCase.sourceText), testCase.wantIds...)
		})
	}
}

func TestNextRequirePageDefaultExportStaysSilent(t *testing.T) {
	cases := []struct {
		name       string
		fileName   string
		sourceText string
	}{
		// The shape the rule asks for.
		{
			"an inline default export ending in PageRoute",
			pageFile,
			"export default function SettingsPageRoute() {\n    return null;\n}\n",
		},
		{
			"a correctly named page alongside other exports",
			pageFile,
			"export const metadata = { title: 'Settings' };\n" +
				"export default function SettingsPageRoute() {\n    return null;\n}\n",
		},
		{
			"a default exported class ending in PageRoute",
			pageFile,
			"export default class SettingsPageRoute {\n    render() { return null; }\n}\n",
		},
		// The file gate. A layout has its own contract that this rule would misdescribe, so it is
		// not held to the page convention.
		{
			"a layout file with no default export",
			layoutFile,
			"export function Settings() {\n    return null;\n}\n",
		},
		{
			"an ordinary component file",
			"/repository/source/components/Settings.tsx",
			"export function Settings() {\n    return null;\n}\n",
		},
		// A page.ts is not a React file, so it is not a page in the sense this rule means.
		{
			"a page.ts rather than page.tsx",
			"/repository/app/account/page.ts",
			"export function Settings() {\n    return null;\n}\n",
		},
		// A named export is not the route's default, and treating it as one would report every
		// helper a page file exports.
		{
			"a named export that is not default",
			pageFile,
			"export function SettingsPageRoute() { return null; }\n" +
				"export default function OtherPageRoute() { return null; }\n",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			rule_testing.ExpectClean(t, rule_testing.Run(t, NextRequirePageDefaultExport, testCase.fileName,
				testCase.sourceText))
		})
	}
}
