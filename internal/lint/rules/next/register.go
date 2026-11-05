package next

import (
	"github.com/system-inc/cohere/internal/lint/rule"
)

// init registers this package's rules with the catalog.
//
// Registration lives beside the rules rather than in a shared list another package maintains. That
// shared list was one file every port had to edit, so two ports landing at once conflicted on a
// file neither was really changing, and the conflict scaled with how many rules were being written
// at the same time. A rule is now two new files and no shared edit.
//
// The cost is that a package whose rules are never imported registers nothing, so the catalog would
// be silently short. `registry` imports every rule package for exactly that reason, and the parity
// guard reads the result against the inventory, so a package dropped from those imports shows up as
// rules that vanished rather than as a quieter run.
func init() {
	rule.Register(
		rule.Registration[rule.NoOptions]{Rule: GoogleFontDisplay},
		rule.Registration[rule.NoOptions]{Rule: GoogleFontPreconnect},
		rule.Registration[rule.NoOptions]{Rule: InlineScriptId},
		rule.Registration[rule.NoOptions]{Rule: NextScriptForGa},
		rule.Registration[rule.NoOptions]{Rule: NoAssignModuleVariable},
		rule.Registration[rule.NoOptions]{Rule: NoAsyncClientComponent},
		rule.Registration[rule.NoOptions]{Rule: NoBeforeInteractiveScriptOutsideDocument},
		rule.Registration[rule.NoOptions]{Rule: NoCssTags},
		rule.Registration[rule.NoOptions]{Rule: NoDuplicateHead},
		rule.Registration[rule.NoOptions]{Rule: NoDocumentImportInPage},
		rule.Registration[rule.NoOptions]{Rule: NoHeadElement},
		// DecodeAt because its pages directories are paths, anchored to the config's directory.
		rule.Registration[NoHtmlLinkForPagesOptions]{
			Rule:     NoHtmlLinkForPages,
			DecodeAt: decodeNoHtmlLinkForPagesOptions,
		},
		rule.Registration[rule.NoOptions]{Rule: NoHeadImportInDocument},
		rule.Registration[rule.NoOptions]{Rule: NoImgElement},
		rule.Registration[rule.NoOptions]{Rule: NoLocationAssignRelativeDestination},
		rule.Registration[rule.NoOptions]{Rule: NoStyledJsxInDocument},
		rule.Registration[rule.NoOptions]{Rule: NoPageCustomFont},
		rule.Registration[rule.NoOptions]{Rule: NoScriptComponentInHead},
		rule.Registration[rule.NoOptions]{Rule: NoSyncScripts},
		rule.Registration[rule.NoOptions]{Rule: NoTypos},
		rule.Registration[rule.NoOptions]{Rule: NoTitleInDocumentHead},
		rule.Registration[rule.NoOptions]{Rule: NoUnwantedPolyfillio},
	)
}
