package structure

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
		rule.Registration[rule.NoOptions]{Rule: BoundaryNoProjectThemeValue},
		rule.Registration[rule.NoOptions]{Rule: ConsistencyRequireOrganizedImports},
		rule.Registration[rule.NoOptions]{Rule: NetworkNoDirectFetch},
		rule.Registration[rule.NoOptions]{Rule: NetworkNoForbiddenImport},
		rule.Registration[rule.NoOptions]{Rule: NetworkNoInvalidateCacheInOnSuccess},
		rule.Registration[rule.NoOptions]{Rule: NetworkNoInvalidateCacheLiteralKey},
		rule.Registration[rule.NoOptions]{Rule: NetworkNoStringLiteralQuery},
		rule.Registration[rule.NoOptions]{Rule: NetworkRequireHookOptionsParameter},
		rule.Registration[rule.NoOptions]{Rule: NetworkRequireHookRequestSuffix},
		rule.Registration[rule.NoOptions]{Rule: NetworkRequireHookVariablesType},
		rule.Registration[rule.NoOptions]{Rule: NextNoPageState},
		rule.Registration[rule.NoOptions]{Rule: NextRequireApiParameterName},
		rule.Registration[rule.NoOptions]{Rule: NextRequirePageDefaultExport},
		rule.Registration[rule.NoOptions]{Rule: ReactComponentNoConstAssignment},
		rule.Registration[rule.NoOptions]{Rule: ReactComponentNoDestructuring},
		rule.Registration[rule.NoOptions]{Rule: ReactComponentNoDisplayName},
		rule.Registration[rule.NoOptions]{Rule: ReactComponentNoForwardReference},
		rule.Registration[rule.NoOptions]{Rule: ReactComponentNoSeparateNamedExport},
		rule.Registration[rule.NoOptions]{Rule: ReactComponentRequireNamedExport},
		rule.Registration[rule.NoOptions]{Rule: ReactComponentRequirePropertiesParameter},
		rule.Registration[rule.NoOptions]{Rule: ReactComponentRequirePropertiesTypeSuffix},
		rule.Registration[rule.NoOptions]{Rule: ReactHookNoAnyType},
		rule.Registration[rule.NoOptions]{Rule: ReactHookNoDestructuring},
		rule.Registration[rule.NoOptions]{Rule: ReactHookNoPropertiesInDependencies},
		rule.Registration[rule.NoOptions]{Rule: ReactHookRequireEffectComment},
		rule.Registration[rule.NoOptions]{Rule: ReactHookRequireResultNaming},
		rule.Registration[rule.NoOptions]{Rule: ImportRequireReactNamespace},
		rule.Registration[rule.NoOptions]{Rule: ReactElementNoAnchor},
		rule.Registration[rule.NoOptions]{Rule: ReactElementNoHorizontalRule},
		rule.Registration[rule.NoOptions]{Rule: StorageNoDirectLocalStorage},
	)
}
