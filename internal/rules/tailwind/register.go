package tailwind

import (
	"github.com/system-inc/verify/internal/rule"
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
			Rule:   EnforceConsistentClassOrder,
			Decode: rule.DecodeOptionsInto[EnforceConsistentClassOrderOptions](),
		},
		rule.Registration{
			Rule:   EnforceCanonicalClasses,
			Decode: rule.DecodeOptionsInto[EnforceCanonicalClassesOptions](),
		},
		rule.Registration{
			Rule:   NoConcatenatedClasses,
			Decode: rule.DecodeOptionsInto[NoConcatenatedClassesOptions](),
		},
		rule.Registration{
			Rule:   NoConflictingClasses,
			Decode: rule.DecodeOptionsInto[NoConflictingClassesOptions](),
		},
		rule.Registration{
			Rule:   NoDeprecatedClasses,
			Decode: rule.DecodeOptionsInto[NoDeprecatedClassesOptions](),
		},
		rule.Registration{
			Rule:   NoDuplicateClasses,
			Decode: rule.DecodeOptionsInto[NoDuplicateClassesOptions](),
		},
		rule.Registration{
			Rule:   NoUnknownClasses,
			Decode: rule.DecodeOptionsInto[NoUnknownClassesOptions](),
		},
		rule.Registration{
			Rule:   NoUnnecessaryWhitespace,
			Decode: rule.DecodeOptionsInto[NoUnnecessaryWhitespaceOptions](),
		},
		rule.Registration{Rule: NoPhysicalDirection},
	)
}
