package nexus

import (
	"testing"

	"github.com/system-inc/verify/internal/ruletest"
)

const nexusFile = "/repository/libraries/nexus/code-quality/Thing.ts"

func TestBoundaryNoNexusOutsideImportFires(t *testing.T) {
	// All three import shapes, because a boundary that only guards the static form is a boundary
	// with a documented way around it.
	cases := []struct {
		name       string
		sourceText string
	}{
		{"static", "import { Thing } from '@structure/source/Thing';\n"},
		{"static default", "import Thing from '@project/ProjectSettings';\n"},
		{"static bare alias", "import '@base';\n"},
		{"dynamic", "export async function load() {\n    return await import('@structure/source/Thing');\n}\n"},
		{"require", "const thing = require('@base/source/Thing');\n"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := ruletest.Run(t, BoundaryNoNexusOutsideImport, nexusFile, testCase.sourceText)
			ruletest.ExpectFindings(t, result, "forbiddenOutsideImport")
		})
	}
}

func TestBoundaryNoNexusOutsideImportStaysSilent(t *testing.T) {
	cases := []struct {
		name       string
		fileName   string
		sourceText string
	}{
		// Inside nexus, reaching sideways and downward is exactly what nexus is supposed to do.
		{"relative", nexusFile, "import { Thing } from './Thing';\n"},
		{"parent relative", nexusFile, "import { Thing } from '../Thing';\n"},
		{"nexus alias", nexusFile, "import { Thing } from '@nexus/code-quality/Thing';\n"},
		{"package", nexusFile, "import * as NodePath from 'node:path';\n"},

		// The prefix has to end at a boundary. A package genuinely named "@projections" is not
		// "@project", and flagging it would be the rule claiming a name it never meant to.
		{"lookalike alias", nexusFile, "import { Thing } from '@projections/Thing';\n"},
		{"lookalike base", nexusFile, "import { Thing } from '@basement/Thing';\n"},

		// Outside nexus, every one of these is legal, so the rule must decline the file entirely.
		{"outside nexus static", "/repository/libraries/structure/source/Thing.ts", "import { Thing } from '@project/ProjectSettings';\n"},
		{"outside nexus dynamic", "/repository/source/Thing.ts", "export async function load() {\n    return await import('@structure/source/Thing');\n}\n"},

		// A path that merely mentions nexus is not the nexus library.
		{"nexus lookalike path", "/repository/libraries/nexus-template/source/Thing.ts", "import { Thing } from '@project/ProjectSettings';\n"},

		// A computed specifier has no string to read, and the rule must not crash reaching for one.
		{"computed dynamic import", nexusFile, "export async function load(name: string) {\n    return await import(name);\n}\n"},

		// A method named require is not the CommonJS require.
		{"method named require", nexusFile, "declare const loader: { require(name: string): unknown };\nconst thing = loader.require('@base/source/Thing');\n"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := ruletest.Run(t, BoundaryNoNexusOutsideImport, testCase.fileName, testCase.sourceText)
			ruletest.ExpectClean(t, result)
		})
	}
}
