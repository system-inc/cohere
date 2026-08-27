package nexus

import (
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/verify/internal/rule"
	"github.com/system-inc/verify/internal/utilities/ecmascript/imports"
)

// The aliases that point out of nexus and into the things built on top of it.
var outsideNexusAliases = []string{"@project", "@structure", "@base"}

var messageForbiddenOutsideImport = rule.Message{
	Id: "forbiddenOutsideImport",
	Description: "Nexus must not import from '@project', '@structure', or '@base'. Nexus is the layer they " +
		"are all built on, so a reach upward makes the foundation depend on what rests on it, and the " +
		"cycle only shows up as a build failure in whichever project imports nexus first.",
}

// BoundaryNoNexusOutsideImport keeps nexus self-contained.
//
//	valid (in libraries/nexus):   import { Thing } from './Thing'
//	invalid (in libraries/nexus): import { Thing } from '@structure/source/Thing'
//
// No fix, and there is no plausible one: the repair is either to move the imported code down into
// nexus or to invert the dependency, and neither is a text edit.
var BoundaryNoNexusOutsideImport = rule.Rule{
	Name: "boundary-no-nexus-outside-import",
	Run: func(ctx rule.Context, options any) rule.Listeners {
		// The whole rule is a question about where the file lives, so a file outside nexus declines
		// before a single node is visited. This is the cheapest a rule gets.
		if !strings.Contains(imports.NormalizedFileName(ctx.SourceFile), "/libraries/nexus/") {
			return nil
		}

		return imports.SourceVisitors(func(source string, node *ast.Node) {
			if isOutsideNexusAlias(source) {
				ctx.ReportNode(imports.SpecifierNode(node), messageForbiddenOutsideImport)
			}
		})
	},
}

// isOutsideNexusAlias reports whether a specifier reaches out of nexus.
//
// The prefix has to end at a boundary. Matching the bare prefix would flag a package genuinely named
// "@projections", and a rule that fires on a name it never meant to claim is how rules get disabled.
func isOutsideNexusAlias(source string) bool {
	for _, alias := range outsideNexusAliases {
		if source == alias || strings.HasPrefix(source, alias+"/") {
			return true
		}
	}
	return false
}
