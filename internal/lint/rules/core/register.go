package core

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
		rule.Registration[rule.NoOptions]{Rule: ConstructorSuper},
		rule.Registration[rule.NoOptions]{Rule: ForDirection},
		rule.Registration[GetterReturnOptions]{
			Rule:   GetterReturn,
			Decode: rule.DecodeOptionsInto[GetterReturnOptions](),
		},
		rule.Registration[rule.NoOptions]{Rule: NoArrayConstructor},
		rule.Registration[rule.NoOptions]{Rule: NoAsyncPromiseExecutor},
		rule.Registration[rule.NoOptions]{Rule: NoCaseDeclarations},
		rule.Registration[rule.NoOptions]{Rule: NoClassAssign},
		rule.Registration[rule.NoOptions]{Rule: NoConstAssign},
		rule.Registration[rule.NoOptions]{Rule: NoCompareNegZero},
		rule.Registration[NoCondAssignOptions]{
			Rule:   NoCondAssign,
			Decode: rule.DecodeOptionsInto[NoCondAssignOptions](),
		},
		rule.Registration[rule.NoOptions]{Rule: NoCaller},
		rule.Registration[rule.NoOptions]{Rule: NoControlRegex},
		rule.Registration[rule.NoOptions]{Rule: NoDupeClassMembers},
		rule.Registration[NoFallthroughOptions]{
			Rule:   NoFallthrough,
			Decode: rule.DecodeOptionsInto[NoFallthroughOptions](),
		},
		rule.Registration[rule.NoOptions]{Rule: NoImportAssign},
		rule.Registration[NoIrregularWhitespaceOptions]{
			Rule:   NoIrregularWhitespace,
			Decode: rule.DecodeOptionsInto[NoIrregularWhitespaceOptions](),
		},
		rule.Registration[rule.NoOptions]{Rule: NoIterator},
		rule.Registration[rule.NoOptions]{Rule: NoLossOfPrecision},
		rule.Registration[rule.NoOptions]{Rule: NoNewNativeNonconstructor},
		rule.Registration[rule.NoOptions]{Rule: NoObjCalls},
		rule.Registration[rule.NoOptions]{Rule: NoOctal},
		rule.Registration[rule.NoOptions]{Rule: NoPrototypeBuiltins},
		rule.Registration[NoUnusedExpressionsOptions]{
			Rule:   NoUnusedExpressions,
			Decode: rule.DecodeOptionsInto[NoUnusedExpressionsOptions](),
		},
		rule.Registration[NoUnusedVariablesOptions]{
			Rule:   NoUnusedVariables,
			Decode: rule.DecodeOptionsInto[NoUnusedVariablesOptions](),
		},
		rule.Registration[rule.NoOptions]{Rule: NoUnusedPrivateClassMembers},
		rule.Registration[PreferConstOptions]{
			Rule:   PreferConst,
			Decode: rule.DecodeOptionsInto[PreferConstOptions](),
		},
		rule.Registration[rule.NoOptions]{Rule: PreferRestParameters},
		rule.Registration[NoConstantBinaryExpressionOptions]{
			Rule:   NoConstantBinaryExpression,
			Decode: rule.DecodeOptionsInto[NoConstantBinaryExpressionOptions](),
		},
		rule.Registration[NoConstantConditionOptions]{
			Rule:   NoConstantCondition,
			Decode: rule.DecodeOptionsInto[NoConstantConditionOptions](),
		},
		rule.Registration[NoExtraBooleanCastOptions]{
			Rule:   NoExtraBooleanCast,
			Decode: rule.DecodeOptionsInto[NoExtraBooleanCastOptions](),
		},
		rule.Registration[rule.NoOptions]{Rule: NoDebugger},
		rule.Registration[rule.NoOptions]{Rule: NoDeleteVariable},
		rule.Registration[rule.NoOptions]{Rule: NoDupeElseIf},
		rule.Registration[rule.NoOptions]{Rule: NoDuplicateCase},
		rule.Registration[NoEmptyOptions]{
			Rule:   NoEmpty,
			Decode: rule.DecodeOptionsInto[NoEmptyOptions](),
		},
		rule.Registration[rule.NoOptions]{Rule: NoEmptyCharacterClass},
		rule.Registration[NoEmptyPatternOptions]{
			Rule:   NoEmptyPattern,
			Decode: rule.DecodeOptionsInto[NoEmptyPatternOptions](),
		},
		rule.Registration[rule.NoOptions]{Rule: NoEmptyStaticBlock},
		rule.Registration[NoEvalOptions]{
			Rule:   NoEval,
			Decode: rule.DecodeOptionsInto[NoEvalOptions](),
		},
		rule.Registration[rule.NoOptions]{Rule: NoExAssign},
		rule.Registration[rule.NoOptions]{Rule: NoFuncAssign},
		rule.Registration[NoGlobalAssignOptions]{
			Rule:   NoGlobalAssign,
			Decode: rule.DecodeOptionsInto[NoGlobalAssignOptions](),
		},
		rule.Registration[NoInvalidRegexpOptions]{
			Rule:   NoInvalidRegexp,
			Decode: DecodeNoInvalidRegexpOptions,
		},
		rule.Registration[NoMisleadingCharacterClassOptions]{
			Rule:   NoMisleadingCharacterClass,
			Decode: rule.DecodeOptionsInto[NoMisleadingCharacterClassOptions](),
		},
		rule.Registration[rule.NoOptions]{Rule: NoNonoctalDecimalEscape},
		rule.Registration[rule.NoOptions]{Rule: NoRegexSpaces},
		rule.Registration[rule.NoOptions]{Rule: NoSetterReturn},
		rule.Registration[NoShadowRestrictedNamesOptions]{
			Rule:   NoShadowRestrictedNames,
			Decode: rule.DecodeOptionsInto[NoShadowRestrictedNamesOptions](),
		},
		rule.Registration[rule.NoOptions]{Rule: NoSparseArrays},
		rule.Registration[rule.NoOptions]{Rule: NoThisBeforeSuper},
		rule.Registration[rule.NoOptions]{Rule: NoUnassignedVariables},
		rule.Registration[rule.NoOptions]{Rule: NoUselessAssignment},
		rule.Registration[rule.NoOptions]{Rule: NoUnsafeFinally},
		rule.Registration[NoUnsafeNegationOptions]{
			Rule:   NoUnsafeNegation,
			Decode: rule.DecodeOptionsInto[NoUnsafeNegationOptions](),
		},
		rule.Registration[NoUnsafeOptionalChainingOptions]{
			Rule:   NoUnsafeOptionalChaining,
			Decode: rule.DecodeOptionsInto[NoUnsafeOptionalChainingOptions](),
		},
		rule.Registration[rule.NoOptions]{Rule: NoUnusedLabels},
		rule.Registration[rule.NoOptions]{Rule: NoUselessBackreference},
		rule.Registration[rule.NoOptions]{Rule: NoUselessCatch},
		rule.Registration[NoUselessEscapeOptions]{
			Rule:   NoUselessEscape,
			Decode: rule.DecodeOptionsInto[NoUselessEscapeOptions](),
		},
		rule.Registration[rule.NoOptions]{Rule: NoVarDeclarations},
		rule.Registration[rule.NoOptions]{Rule: NoWith},
		rule.Registration[rule.NoOptions]{Rule: PreferSpread},
		rule.Registration[PreserveCaughtErrorOptions]{
			Rule:   PreserveCaughtError,
			Decode: rule.DecodeOptionsInto[PreserveCaughtErrorOptions](),
		},
		rule.Registration[rule.NoOptions]{Rule: RequireYield},
		rule.Registration[UseIsNaNOptions]{
			Rule:   UseIsNaN,
			Decode: rule.DecodeOptionsInto[UseIsNaNOptions](),
		},
		rule.Registration[ValidTypeofOptions]{
			Rule:   ValidTypeof,
			Decode: rule.DecodeOptionsInto[ValidTypeofOptions](),
		},
	)
}
