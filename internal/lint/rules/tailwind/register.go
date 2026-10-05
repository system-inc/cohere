package tailwind

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
		rule.Registration{
			Rule:     EnforceConsistentClassOrder,
			DecodeAt: decodeTailwindOptionsAt[EnforceConsistentClassOrderOptions](),
		},
		rule.Registration{
			Rule:     EnforceConsistentVariantOrder,
			DecodeAt: decodeTailwindOptionsAt[EnforceConsistentVariantOrderOptions](),
		},
		rule.Registration{
			Rule:     EnforceConsistentImportantPosition,
			DecodeAt: decodeTailwindOptionsAt[EnforceConsistentImportantPositionOptions](),
		},
		rule.Registration{
			Rule:     EnforceConsistentVariableSyntax,
			DecodeAt: decodeTailwindOptionsAt[EnforceConsistentVariableSyntaxOptions](),
		},
		rule.Registration{
			Rule:     EnforceShorthandClasses,
			DecodeAt: decodeTailwindOptionsAt[EnforceShorthandClassesOptions](),
		},
		rule.Registration{
			Rule:     EnforceCanonicalClasses,
			DecodeAt: decodeTailwindOptionsAt[EnforceCanonicalClassesOptions](),
		},
		rule.Registration{
			Rule:     NoConcatenatedClasses,
			DecodeAt: decodeTailwindOptionsAt[NoConcatenatedClassesOptions](),
		},
		rule.Registration{
			Rule:     NoConflictingClasses,
			DecodeAt: decodeTailwindOptionsAt[NoConflictingClassesOptions](),
		},
		rule.Registration{
			Rule:     NoDeprecatedClasses,
			DecodeAt: decodeTailwindOptionsAt[NoDeprecatedClassesOptions](),
		},
		rule.Registration{
			Rule:     NoDuplicateClasses,
			DecodeAt: decodeTailwindOptionsAt[NoDuplicateClassesOptions](),
		},
		rule.Registration{
			Rule:     NoUnknownClasses,
			DecodeAt: decodeTailwindOptionsAt[NoUnknownClassesOptions](),
		},
		rule.Registration{
			Rule:     NoUnnecessaryWhitespace,
			DecodeAt: decodeTailwindOptionsAt[NoUnnecessaryWhitespaceOptions](),
		},
		rule.Registration{Rule: NoPhysicalDirection},
	)
}
