package nexus

import (
	"strings"
	"testing"

	"github.com/system-inc/verify/internal/ruletest"
)

var pathAliasOptions = ImportRequirePathAliasOptions{
	RepositoryRoot: "/repository",
	Aliases: []PathAlias{
		{Directory: "libraries/base/source", Alias: "@base/source"},
		{Directory: "libraries/base", Alias: "@base"},
		{Directory: "source", Alias: "@project/source"},
	},
	StrictRoots: []string{"libraries/base"},
}

const aliasImporter = "/repository/source/features/orders/Thing.ts"

func TestImportRequirePathAliasFires(t *testing.T) {
	cases := []struct {
		name       string
		fileName   string
		sourceText string
		wantId     string
	}{
		{"climbs two levels", aliasImporter, "import { Thing } from '../../foundation/Thing';\n", "useAlias"},
		{"climbs three levels", aliasImporter, "import { Thing } from '../../../source/foundation/Thing';\n", "useAlias"},
		{"dynamic import climbs", aliasImporter, "const thing = import('../../foundation/Thing');\n", "useAlias"},
		{"require climbs", aliasImporter, "const thing = require('../../foundation/Thing');\n", "useAlias"},
		// In a strict root even a sibling must be aliased.
		{"sibling inside a strict root", "/repository/libraries/base/source/Thing.ts", "import { Other } from './Other';\n", "useAliasInStrictRoot"},
		{"parent inside a strict root", "/repository/libraries/base/source/deep/Thing.ts", "import { Other } from '../Other';\n", "useAliasInStrictRoot"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := ruletest.RunWithOptions(t, ImportRequirePathAlias, testCase.fileName, testCase.sourceText, pathAliasOptions)
			ruletest.ExpectFindings(t, result, testCase.wantId)
		})
	}
}

func TestImportRequirePathAliasStaysSilent(t *testing.T) {
	cases := []struct {
		name       string
		fileName   string
		sourceText string
		options    any
	}{
		// One level stays relative on purpose: those are facts the reader already has.
		{"sibling import", aliasImporter, "import { Sibling } from './Sibling';\n", pathAliasOptions},
		{"parent import", aliasImporter, "import { Parent } from '../Parent';\n", pathAliasOptions},
		{"already aliased", aliasImporter, "import { Thing } from '@base/source/Thing';\n", pathAliasOptions},
		{"a package import", aliasImporter, "import * as NodePath from 'node:path';\n", pathAliasOptions},
		// Reporting here would demand exactly what boundary-no-internal-import rejects.
		{"an internal path must stay relative", aliasImporter, "import { Detail } from '../../internal/Detail';\n", pathAliasOptions},
		// A path leaving the repository is not ours to name.
		{"climbs out of the repository", "/repository/Thing.ts", "import { Thing } from '../../elsewhere/Thing';\n", pathAliasOptions},
		// No alias covers the destination, so there is nothing to suggest.
		{"no alias covers it", aliasImporter, "import { Thing } from '../../../other/Thing';\n", pathAliasOptions},
		{"a computed dynamic import", aliasImporter, "const thing = import(somePath);\n", pathAliasOptions},
		// A misconfigured rule declines rather than guessing.
		{"no options at all", aliasImporter, "import { Thing } from '../../foundation/Thing';\n", nil},
		// These two carry a real options value with one half missing. Both stay silent even without
		// the explicit guard, because aliasForPath finds nothing with an empty alias list and an
		// empty root makes every path fall outside the repository. The guard is deliberate
		// redundancy rather than the only thing holding these up, and saying so is more useful
		// than implying a mutation would catch it.
		{"no aliases configured", aliasImporter, "import { Thing } from '../../foundation/Thing';\n", ImportRequirePathAliasOptions{RepositoryRoot: "/repository"}},
		{"no repository root", aliasImporter, "import { Thing } from '../../foundation/Thing';\n", ImportRequirePathAliasOptions{Aliases: pathAliasOptions.Aliases}},
		{"file outside the repository", "/elsewhere/Thing.ts", "import { Thing } from '../../foundation/Thing';\n", pathAliasOptions},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := ruletest.RunWithOptions(t, ImportRequirePathAlias, testCase.fileName, testCase.sourceText, testCase.options)
			ruletest.ExpectClean(t, result)
		})
	}
}

// Longest directory wins, so a nested root is not shadowed by its parent.
//
// The alias strings here are deliberately shaped so the two orders produce different text. With the
// natural spellings they do not: "@base" + "/source/foundation/Thing" and "@base/source" +
// "/foundation/Thing" are the same string, so a fixture using those cannot tell a correct sort from
// a reversed one. That fixture existed first and a reversed-sort mutation sailed straight past it.
func TestImportRequirePathAliasPrefersTheLongestRoot(t *testing.T) {
	options := ImportRequirePathAliasOptions{
		RepositoryRoot: "/repository",
		Aliases: []PathAlias{
			{Directory: "libraries/base", Alias: "@base"},
			{Directory: "libraries/base/source", Alias: "@basesource"},
		},
	}
	result := ruletest.RunWithOptions(t, ImportRequirePathAlias,
		"/repository/libraries/base/deep/nested/Thing.ts",
		"import { Thing } from '../../source/foundation/Thing';\n", options)
	if len(result.Diagnostics) == 0 {
		t.Fatalf("expected a finding, got none")
	}
	description := result.Diagnostics[0].Message.Description
	if !strings.Contains(description, "@basesource/foundation/Thing") {
		t.Fatalf("expected the longest matching alias, got: %s", description)
	}
}

// The suggestion has to be a path someone can paste, so it is asserted rather than assumed.
func TestImportRequirePathAliasSuggestsTheAliasedPath(t *testing.T) {
	result := ruletest.RunWithOptions(t, ImportRequirePathAlias, aliasImporter,
		"import { Thing } from '../../foundation/Thing';\n", pathAliasOptions)
	ruletest.ExpectFindings(t, result, "useAlias")

	description := result.Diagnostics[0].Message.Description
	if !strings.Contains(description, "@project/source/foundation/Thing") {
		t.Fatalf("expected the aliased suggestion, got: %s", description)
	}
}
