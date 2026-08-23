package nexus

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/scanner"
	"github.com/system-inc/verify/internal/ruletest"
)

const forbiddenSourceFile = "/repository/source/Thing.tsx"

func TestImportNoForbiddenSourceFires(t *testing.T) {
	cases := []struct {
		name       string
		sourceText string
		wantId     string
		wantFix    string
	}{
		{
			"next navigation",
			"import { useRouter } from 'next/navigation';\n",
			"forbiddenNavigationImport",
			"'@structure/source/router/Navigation'",
		},
		{
			"next link",
			"import Link from 'next/link';\n",
			"forbiddenLinkImport",
			"'@structure/source/components/navigation/Link'",
		},
		{
			"next image",
			"import Image from 'next/image';\n",
			"forbiddenImageImport",
			"'@structure/source/components/images/Image'",
		},
		{
			"framer motion",
			"import { motion } from 'framer-motion';\n",
			"forbiddenMotionImport",
			"'motion/react'",
		},
		{
			"require",
			"const Link = require('next/link');\n",
			"forbiddenLinkImport",
			"'@structure/source/components/navigation/Link'",
		},
		{
			"dynamic import",
			"const Image = import('next/image');\n",
			"forbiddenImageImport",
			"'@structure/source/components/images/Image'",
		},
		{
			// A named import carries bindings the fix does not touch, on purpose. Rewriting only
			// the string is the part that is always right.
			"named imports keep their bindings",
			"import { motion, AnimatePresence } from 'framer-motion';\n",
			"forbiddenMotionImport",
			"'motion/react'",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := ruletest.Run(t, ImportNoForbiddenSource, forbiddenSourceFile, testCase.sourceText)
			ruletest.ExpectFindings(t, result, testCase.wantId)

			// A fixable rule whose fix is never checked is half-tested: the finding proves it saw
			// the problem, the fix text proves it would repair it rather than corrupt the file.
			fixes := result.Diagnostics[0].Fixes
			if len(fixes) != 1 {
				t.Fatalf("expected exactly one fix, got %d", len(fixes))
			}
			if fixes[0].Text != testCase.wantFix {
				t.Fatalf("expected fix %q, got %q", testCase.wantFix, fixes[0].Text)
			}
		})
	}
}

// A finding has to land on the line the author can suppress.
//
// A node's Pos() includes leading trivia, so anchoring on the enclosing declaration reports at the
// first comment above the import rather than at the import. That put real findings on line 1 of the
// Next wrapper files while the `eslint-disable-next-line` sat on line 3 covering line 4, making them
// unreachable by any suppression that could be written. A finding at the wrong location is invisible
// in a findings count and fatal to suppression, so the location is asserted rather than assumed.
func TestImportNoForbiddenSourceReportsAtTheSpecifier(t *testing.T) {
	sourceText := "// Dependencies - Frameworks\n" +
		"// This is the only place this import is valid\n" +
		"// eslint-disable-next-line nexus/import-no-forbidden-source\n" +
		"import NextImage from 'next/image';\n"

	result := ruletest.Run(t, ImportNoForbiddenSource, forbiddenSourceFile, sourceText)
	ruletest.ExpectFindings(t, result, "forbiddenImageImport")

	line, _ := scanner.GetECMALineAndByteOffsetOfPosition(result.SourceFile, result.Diagnostics[0].Range.Pos())
	const importLine = 3 // zero-based, so the fourth line
	if line != importLine {
		t.Fatalf("expected the finding on the import line (%d), got line %d", importLine+1, line+1)
	}
}

func TestImportNoForbiddenSourceStaysSilent(t *testing.T) {
	cases := []struct {
		name       string
		sourceText string
	}{
		{"the replacement itself", "import Link from '@structure/source/components/navigation/Link';\n"},
		{"the motion replacement", "import { motion } from 'motion/react';\n"},
		{"an unrelated next module", "import { headers } from 'next/headers';\n"},
		// A prefix match would flag these. The comparison is exact, because 'next/linkify' is a
		// different package than 'next/link' and asking a reader to rewrite it would be wrong.
		{"a longer specifier sharing a prefix", "import { thing } from 'next/link-utilities';\n"},
		{"a package whose name contains a forbidden one", "import { thing } from 'framer-motion-extras';\n"},
		{"an ordinary relative import", "import { Thing } from './Thing';\n"},
		{"a computed dynamic import", "const module = import(somePath);\n"},
		{"no imports at all", "export const Value = 1;\n"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := ruletest.Run(t, ImportNoForbiddenSource, forbiddenSourceFile, testCase.sourceText)
			ruletest.ExpectClean(t, result)
		})
	}
}
