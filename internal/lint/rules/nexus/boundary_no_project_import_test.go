package nexus

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/scanner"
	"github.com/system-inc/cohere/internal/lint/testing"
)

const baseLibraryFile = "/repo/libraries/base/source/Thing.ts"

var baseLibraryOptions = BoundaryNoProjectImportOptions{
	LibraryDirectory: "/libraries/base/",
	Allowed:          []string{"@project/ProjectSettings"},
}

func TestBoundaryNoProjectImportFires(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name   string
		source string
	}{
		{"static import", "import { helper } from '@project/source/Helper';\n"},
		{"require", "const helper = require('@project/source/Helper');\n"},
		{"dynamic import", "const helper = import('@project/source/Helper');\n"},
		// A whitelist entry admits exactly itself. If it matched by prefix, a file added beside
		// the approved one would be admitted without anyone approving it.
		{"near miss on whitelist", "import { secret } from '@project/ProjectSettingsSecret';\n"},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunWithOptions(
				test,
				BoundaryNoProjectImport,
				baseLibraryFile,
				testCase.source,
				baseLibraryOptions,
			)
			rule_testing.ExpectFindings(test, result, "forbiddenProjectImport")
		})
	}
}

func TestBoundaryNoProjectImportStaysSilent(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name     string
		fileName string
		source   string
		options  any
	}{
		{
			"whitelisted specifier",
			baseLibraryFile,
			"import { settings } from '@project/ProjectSettings';\n",
			baseLibraryOptions,
		},
		{"relative import", baseLibraryFile, "import { Thing } from './Thing';\n", baseLibraryOptions},
		{"sibling alias", baseLibraryFile, "import { Thing } from '@structure/source/Thing';\n", baseLibraryOptions},
		{"unrelated package", baseLibraryFile, "import { Thing } from '@projector/Thing';\n", baseLibraryOptions},
		{
			"outside the guarded library",
			"/repo/application/source/Thing.ts",
			"import { helper } from '@project/source/Helper';\n",
			baseLibraryOptions,
		},
		// A rule registered without its required option must guard nothing rather than guard
		// everything. Failing open here would have been a silent tree-wide false positive; the
		// liveness harness in ahra reports exactly this rule as its one dead fixture.
		{"no options at all", baseLibraryFile, "import { helper } from '@project/source/Helper';\n", nil},
		{
			"empty library directory",
			baseLibraryFile,
			"import { helper } from '@project/source/Helper';\n",
			BoundaryNoProjectImportOptions{},
		},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunWithOptions(
				test,
				BoundaryNoProjectImport,
				testCase.fileName,
				testCase.source,
				testCase.options,
			)
			rule_testing.ExpectClean(test, result)
		})
	}
}

// A finding has to land on the line the author can suppress. A node's Pos() includes its leading
// trivia, so reporting the declaration anchors the finding at the first comment above the import,
// where no `eslint-disable-next-line` can reach it.
func TestBoundaryNoProjectImportReportsAtTheSpecifier(test *testing.T) {
	test.Parallel()

	sourceText := "// Dependencies\n// a second comment\nimport { helper } from '@project/source/Helper';\\n"

	result := rule_testing.RunWithOptions(
		test,
		BoundaryNoProjectImport,
		baseLibraryFile,
		sourceText,
		baseLibraryOptions,
	)
	if len(result.Diagnostics) == 0 {
		test.Fatalf("expected a finding, got none")
	}

	line, _ := scanner.GetECMALineAndByteOffsetOfPosition(result.SourceFile, result.Diagnostics[0].Range.Pos())
	const importLine = 2 // zero-based, so the third line
	if line != importLine {
		test.Fatalf("expected the finding on the import line (%d), got line %d", importLine+1, line+1)
	}
}
