package nexus

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
		rule.Registration[rule.NoOptions]{Rule: BoundaryNoInternalImport},
		rule.Registration[rule.NoOptions]{Rule: BoundaryNoNexusOutsideImport},
		rule.Registration[BoundaryNoProjectImportOptions]{
			Rule:            BoundaryNoProjectImport,
			Decode:          rule.DecodeOptionsInto[BoundaryNoProjectImportOptions](),
			RequiresOptions: true,
		},
		rule.Registration[ConsistencyNoAbbreviatedIdentifierOptions]{
			Rule:   ConsistencyNoAbbreviatedIdentifier,
			Decode: rule.DecodeOptionsInto[ConsistencyNoAbbreviatedIdentifierOptions](),
		},
		rule.Registration[rule.NoOptions]{Rule: ConsistencyNoAmbiguousIdentifier},
		rule.Registration[ConsistencyNoBooleanOutcomeOptions]{
			Rule:   ConsistencyNoBooleanOutcome,
			Decode: rule.DecodeOptionsInto[ConsistencyNoBooleanOutcomeOptions](),
		},
		rule.Registration[rule.NoOptions]{Rule: ConsistencyNoEnum},
		rule.Registration[ConsistencyNoLongLineCommentOptions]{
			Rule:   ConsistencyNoLongLineComment,
			Decode: rule.DecodeOptionsInto[ConsistencyNoLongLineCommentOptions](),
		},
		rule.Registration[rule.NoOptions]{Rule: ConsistencyNoMultilineArrowFunction},
		rule.Registration[ConsistencyNoScreamingSnakeCaseOptions]{
			Rule:   ConsistencyNoScreamingSnakeCase,
			Decode: rule.DecodeOptionsInto[ConsistencyNoScreamingSnakeCaseOptions](),
		},
		rule.Registration[rule.NoOptions]{Rule: ConsistencyNoShouting},
		rule.Registration[rule.NoOptions]{Rule: ConsistencyNoSingleLineJsDoc},
		rule.Registration[ConsistencyNoStutteringNameOptions]{
			Rule:   ConsistencyNoStutteringName,
			Decode: rule.DecodeOptionsInto[ConsistencyNoStutteringNameOptions](),
		},
		rule.Registration[ConsistencyNoUtilsFolderOptions]{
			Rule:     ConsistencyNoUtilsFolder,
			DecodeAt: decodeConsistencyNoUtilsFolderOptions,
		},
		rule.Registration[ConsistencyRequireConstantCasingOptions]{
			Rule:   ConsistencyRequireConstantCasing,
			Decode: rule.DecodeOptionsInto[ConsistencyRequireConstantCasingOptions](),
		},
		rule.Registration[rule.NoOptions]{Rule: ConsistencyRequireTypeSuffix},
		rule.Registration[rule.NoOptions]{Rule: ImportNoForbiddenSource},
		rule.Registration[ImportRequireModuleAliasOptions]{
			Rule:   ImportRequireModuleAlias,
			Decode: rule.DecodeOptionsInto[ImportRequireModuleAliasOptions](),
		},
		rule.Registration[rule.NoOptions]{Rule: ImportRequireNodeNamespace},
		rule.Registration[ImportRequirePathAliasOptions]{
			Rule:            ImportRequirePathAlias,
			DecodeAt:        decodeImportRequirePathAliasOptions,
			RequiresOptions: true,
		},
		rule.Registration[rule.NoOptions]{Rule: LocalizationNoUntranslatedValue},
	)
}
