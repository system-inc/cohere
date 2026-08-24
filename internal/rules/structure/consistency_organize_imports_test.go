package structure

import (
	"testing"

	"github.com/system-inc/verify/internal/ruletest"
)

// The corpus is written rather than imported, because this is a house rule and no oxc
// implementation exists. Every case below was run against the live TypeScript rule through
// ESLint's Linter API before it was written down, so each records a measured verdict rather than
// a belief about one. A rule with no imported floor is a weaker artifact than a ported one and
// the next reader should know which kind this is.
//
// The probe that produced these needed `files: ['**/*.ts', '**/*.tsx']` in its flat config. Without
// it ESLint matches no configuration for a TypeScript file and reports nothing, and the first run
// of 400 real files came back clean for exactly that reason. The control that caught it was the
// tree's own fixture, which is a known violation and must report.
func TestConsistencyOrganizeImportsFires(t *testing.T) {
	testCases := []struct {
		name   string
		source string
		// reported is the exact source text the finding must span. The rule carries no fix, so
		// the span is the only thing that can catch it pointing at the wrong node.
		reported string
	}{
		{
			name:     "react sorts ahead of every other framework package",
			source:   "import a from 'next/link';\nimport React from 'react';\nimport b from 'react-dom';\n\nexport const X = 1;\n",
			reported: "import a from 'next/link';",
		},
		{
			// The react-first tiebreak, from the reporting side. `next/link` sorts before `react`
			// by string comparison, so a block written in plain alphabetical order is wrong.
			name:     "inside Frameworks, plain alphabetical order is not canonical order",
			source:   "// Dependencies - Frameworks\nimport a from 'next/link';\nimport React from 'react';\n\nexport const X = 1;\n",
			reported: "import a from 'next/link';",
		},
		{
			name:     "alphabetical inside a group",
			source:   "import b from 'zebra';\nimport a from 'alpha';\n\nexport const X = 1;\n",
			reported: "import b from 'zebra';",
		},
		{
			name:     "a single group with no header at all still needs one",
			source:   "import React from 'react';\n\nexport const X = 1;\n",
			reported: "import React from 'react';",
		},
		{
			name:     "a header naming a group that does not exist",
			source:   "// Dependencies - Bogus\nimport React from 'react';\n\nexport const X = 1;\n",
			reported: "import React from 'react';",
		},
		{
			name:     "a bare Dependencies header is not a group header",
			source:   "// Dependencies\nimport React from 'react';\n\nexport const X = 1;\n",
			reported: "import React from 'react';",
		},
		{
			name:     "a directive with no trailing comment",
			source:   "'use client';\n\nimport React from 'react';\n\nexport const X = 1;\n",
			reported: "import React from 'react';",
		},
		{
			// The other side of telling the two directives apart: a server directive carrying the
			// client comment is wrong and reports. Without this the comment table could map both
			// directives to one string and nothing would notice.
			name:     "use server carrying the client comment",
			source:   "'use server'; // Uses client-only features\n\n// Dependencies - Frameworks\nimport React from 'react';\n\nexport const X = 1;\n",
			reported: "import React from 'react';",
		},
		{
			name:     "use server takes its own comment",
			source:   "'use server';\nimport React from 'react';\nexport const X = 1;\n",
			reported: "import React from 'react';",
		},
		{
			name:     "a type-only import splits away from the value import of the same module",
			source:   "import type { T } from 'alpha';\nimport v from 'alpha';\n\nexport const X = 1;\n",
			reported: "import type { T } from 'alpha';",
		},
		{
			name:     "a statement written between two imports",
			source:   "// Dependencies - Frameworks\nimport React from 'react';\n\ninterface Thing { a: number }\n\n// Dependencies - Utilities\nimport x from '@structure/source/utilities/A';\n\nexport const X = 1;\n",
			reported: "import React from 'react';",
		},
		{
			name:     "a side-effect import sorts by its specifier like any other",
			source:   "import 'zzz-side';\nimport a from 'alpha';\n\nexport const X = 1;\n",
			reported: "import 'zzz-side';",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result := ruletest.Run(t, ConsistencyOrganizeImports, "/repository/app/Thing.tsx", testCase.source)
			ruletest.ExpectFindings(t, result, "importsNotOrganized")

			span := result.Diagnostics[0].Range
			got := testCase.source[span.Pos():span.End()]
			if got != testCase.reported {
				t.Fatalf("finding spans %q, want %q", got, testCase.reported)
			}
		})
	}
}

func TestConsistencyOrganizeImportsStaysSilent(t *testing.T) {
	testCases := []struct {
		name string
		// fileName is the path the rule is handed. Empty means the default below, and it exists so
		// a case can say something about the file rather than about its contents.
		fileName string
		source   string
	}{
		{
			name:   "two groups already in canonical order",
			source: "// Dependencies - Frameworks\nimport React from 'react';\n\n// Dependencies - Utilities\nimport x from '@structure/source/utilities/A';\n\nexport const X = 1;\n",
		},
		{
			// The half that can see the react-first tiebreak. Every other Frameworks fixture is
			// unsorted anyway and reports with or without the tiebreak, so both paths reach the same
			// verdict and none of them can catch a mutation that removes it. This block is clean
			// only because react jumps the queue: by string comparison `next/link` sorts first.
			name:   "inside Frameworks, react sits above a package that sorts before it",
			source: "// Dependencies - Frameworks\nimport React from 'react';\nimport a from 'next/link';\n\nexport const X = 1;\n",
		},
		{
			// Two imports in ONE group, already ascending. Every other clean case here holds at most
			// one import per group, so the comparison inside a group is never exercised by them and
			// a mutation flipping it to descending changed no verdict at all. Measured on the
			// original: this input is clean.
			name:   "two imports in one group, already in ascending order",
			source: "// Dependencies - Third-party\nimport a from 'alpha';\nimport z from 'zebra';\n\nexport const X = 1;\n",
		},
		{
			// Generated output is declined before a single node is read. Measured on the original:
			// this file is unsorted and unheadered and still reports nothing.
			name:     "a generated file is declined however wrong its imports are",
			fileName: "/repository/app/generated/Thing.tsx",
			source:   "import z from 'zebra';\nimport a from 'alpha';\n\nexport const X = 1;\n",
		},
		{
			// use server takes its OWN comment, not the client one. Nothing else here can see the
			// two directives being told apart: the reporting cases pass through a directive with no
			// comment at all, which reports whichever text the rule would have written.
			name:   "use server already carrying the server comment",
			source: "'use server'; // Uses server-only features\n\n// Dependencies - Frameworks\nimport React from 'react';\n\nexport const X = 1;\n",
		},
		{
			// Only the FIRST statement is a directive, which is what the language says too. A
			// string expression written below the imports is an ordinary statement, so it is not
			// re-emitted with a comment and its presence does not make the file unorganized.
			name:   "a directive-shaped string below the imports is not a directive",
			source: "// Dependencies - Frameworks\nimport React from 'react';\n\n'use client';\n\nexport const X = 1;\n",
		},
		{
			name:   "a directive that already carries its comment",
			source: "'use client'; // Uses client-only features\n\n// Dependencies - Frameworks\nimport React from 'react';\n\nexport const X = 1;\n",
		},
		{
			name:   "a file description block between the directive and the headers",
			source: "'use client'; // Uses client-only features\n\n/*\n * What this file does.\n */\n\n// Dependencies - Frameworks\nimport React from 'react';\n\nexport const X = 1;\n",
		},
		{
			name:   "a comment a reader wrote above an import travels with it",
			source: "// Dependencies - Frameworks\n// why we need react\nimport React from 'react';\n\nexport const X = 1;\n",
		},
		{
			name:   "a disable comment above an import is preserved when a header sits above it",
			source: "// Dependencies - Frameworks\n// eslint-disable-next-line no-restricted-imports\nimport React from 'react';\n\nexport const X = 1;\n",
		},
		{
			name:   "a file with no imports has no section to organize",
			source: "export const X = 1;\n",
		},
		{
			name:   "a preamble comment above the section stays preamble",
			source: "// Copyright 2026\n\n// Dependencies - Frameworks\nimport React from 'react';\n\nexport const X = 1;\n",
		},
		{
			name:   "no blank line before the first statement after the section",
			source: "// Dependencies - Frameworks\nimport React from 'react';\nexport const X = 1;\n",
		},
		{
			name:   "every group in canonical order, which is the ordering itself under test",
			source: "// Dependencies - Node\nimport NodePath from 'node:path';\n\n// Dependencies - Frameworks\nimport React from 'react';\n\n// Dependencies - Third-party\nimport lodash from 'lodash';\n\n// Dependencies - Nexus\nimport n from '@nexus/source/Thing';\n\n// Dependencies - Theme\nimport t from '@structure/source/theme/Theme';\n\n// Dependencies - Types\nimport type { T } from '@structure/source/Thing';\n\n// Dependencies - APIs\nimport api from '@structure/source/api/Api';\n\n// Dependencies - Hooks\nimport h from '@structure/source/hooks/useThing';\n\n// Dependencies - Components\nimport c from '@structure/source/components/Button';\n\n// Dependencies - Local Components\nimport l from './Thing';\n\n// Dependencies - Animations\nimport m from 'motion/react';\n\n// Dependencies - Assets\nimport icon from '@phosphor-icons/react';\n\n// Dependencies - Utilities\nimport u from '@structure/source/utilities/Strings';\n\nexport const X = 1;\n",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fileName := testCase.fileName
			if fileName == "" {
				fileName = "/repository/app/Thing.tsx"
			}
			result := ruletest.Run(t, ConsistencyOrganizeImports, fileName, testCase.source)
			ruletest.ExpectClean(t, result)
		})
	}
}
