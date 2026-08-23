package nexus

import (
	"strings"

	"github.com/microsoft/typescript-go/shim/ast"
	"github.com/system-inc/verify/internal/rule"
)

var (
	messageNoUnderscoreUtils = rule.Message{
		Id:          "noUnderscoreUtils",
		Description: `Folder name "_utils" is not allowed. Use "_utilities" instead.`,
	}
	messageNoUtils = rule.Message{
		Id:          "noUtils",
		Description: `Folder name "utils" is not allowed. Use "utilities" instead.`,
	}
)

// ConsistencyNoUtilsFolder bans "utils" and "_utils" as directory names.
//
// The abbreviation is the smaller half of the problem. A folder named for nothing in particular
// collects everything, and "utilities" is not meaningfully better at that, but the spelling is what
// the kingdom settled on and one spelling searched is worth two spellings guessed.
//
// This rule reads only the path, so it is the cheapest kind of rule there is: it decides on a
// string and declines the file before any node is visited.
var ConsistencyNoUtilsFolder = rule.Rule{
	Name: "consistency-no-utils-folder",
	Run: func(ctx rule.Context, options any) rule.Listeners {
		if ctx.SourceFile == nil {
			return nil
		}

		fileName := ctx.SourceFile.FileName()
		hasUnderscoreUtils := containsPathSegment(fileName, "_utils")
		hasUtils := containsPathSegment(fileName, "utils")
		if !hasUnderscoreUtils && !hasUtils {
			// Declining costs one string scan. Listening would cost a walk of the whole file to
			// learn the same thing.
			return nil
		}

		return rule.Listeners{
			ast.KindSourceFile: func(node *ast.Node) {
				if hasUnderscoreUtils {
					ctx.ReportRange(node.Loc.WithEnd(node.Loc.Pos()), messageNoUnderscoreUtils)
				}
				if hasUtils {
					ctx.ReportRange(node.Loc.WithEnd(node.Loc.Pos()), messageNoUtils)
				}
			},
		}
	},
}

// containsPathSegment reports whether a path contains a directory named exactly segment.
//
// Matching the bare substring would flag "utilities" for containing "utils", and a rule that fires
// on the spelling it is asking for is worse than no rule.
func containsPathSegment(path string, segment string) bool {
	for _, separator := range []string{"/", "\\"} {
		if strings.Contains(path, separator+segment+separator) {
			return true
		}
	}
	return false
}
