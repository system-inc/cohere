package core

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
		rule.Registration{Rule: ForDirection},
		rule.Registration{Rule: NoCaseDeclarations},
		rule.Registration{Rule: NoCompareNegZero},
		rule.Registration{Rule: NoIterator},
		rule.Registration{
			Rule:   NoConstantBinaryExpression,
			Decode: rule.DecodeOptionsInto[NoConstantBinaryExpressionOptions](),
		},
		rule.Registration{
			Rule:   NoConstantCondition,
			Decode: rule.DecodeOptionsInto[NoConstantConditionOptions](),
		},
		rule.Registration{Rule: NoDebugger},
		rule.Registration{Rule: NoDeleteVar},
		rule.Registration{Rule: NoDupeElseIf},
		rule.Registration{Rule: NoDuplicateCase},
		rule.Registration{
			Rule:   NoEmpty,
			Decode: rule.DecodeOptionsInto[NoEmptyOptions](),
		},
		rule.Registration{Rule: NoEmptyCharacterClass},
		rule.Registration{
			Rule:   NoEmptyPattern,
			Decode: rule.DecodeOptionsInto[NoEmptyPatternOptions](),
		},
		rule.Registration{Rule: NoEmptyStaticBlock},
		rule.Registration{Rule: NoExAssign},
		rule.Registration{Rule: NoInvalidRegexp},
		rule.Registration{Rule: NoNonoctalDecimalEscape},
		rule.Registration{Rule: NoSelfAssign},
		rule.Registration{Rule: NoSparseArrays},
		rule.Registration{Rule: NoUnsafeFinally},
		rule.Registration{Rule: NoUselessCatch},
		rule.Registration{Rule: NoVar},
		rule.Registration{Rule: PreferSpread},
		rule.Registration{Rule: RequireYield},
		rule.Registration{
			Rule:   UseIsNaN,
			Decode: rule.DecodeOptionsInto[UseIsNaNOptions](),
		},
		rule.Registration{Rule: ValidTypeof},
	)
}
