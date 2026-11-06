package typescript

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
		rule.Registration[rule.NoOptions]{Rule: AwaitThenable},
		rule.Registration[BanTsCommentOptions]{
			Rule:   BanTsComment,
			Decode: DecodeBanTsCommentOptions,
		},
		rule.Registration[ConsistentTypeImportsOptions]{
			Rule:   ConsistentTypeImports,
			Decode: DecodeConsistentTypeImportsOptions,
		},
		rule.Registration[rule.NoOptions]{Rule: NoArrayDelete},
		rule.Registration[rule.NoOptions]{Rule: NoDuplicateEnumValues},
		rule.Registration[NoExplicitAnyOptions]{
			Rule:   NoExplicitAny,
			Decode: rule.DecodeOptionsInto[NoExplicitAnyOptions](),
		},
		rule.Registration[rule.NoOptions]{Rule: NoExtraNonNullAssertion},
		rule.Registration[rule.NoOptions]{Rule: NoImpliedEval},
		rule.Registration[NoFloatingPromisesOptions]{
			Rule:   NoFloatingPromises,
			Decode: DecodeNoFloatingPromisesOptions,
		},
		rule.Registration[rule.NoOptions]{Rule: NoForInArray},
		rule.Registration[rule.NoOptions]{Rule: NoMisusedNew},
		rule.Registration[NoMisusedPromisesOptions]{
			Rule:   NoMisusedPromises,
			Decode: rule.DecodeOptionsInto[NoMisusedPromisesOptions](),
		},
		rule.Registration[rule.NoOptions]{Rule: NoNonNullAssertedOptionalChain},
		rule.Registration[rule.NoOptions]{Rule: NoNonNullAssertion},
		rule.Registration[NoRequireImportsOptions]{
			Rule:   NoRequireImports,
			Decode: DecodeNoRequireImportsOptions,
		},
		rule.Registration[NoThisAliasOptions]{
			Rule:   NoThisAlias,
			Decode: DecodeNoThisAliasOptions,
		},
		rule.Registration[rule.NoOptions]{Rule: NoUnnecessaryParameterPropertyAssignment},
		rule.Registration[rule.NoOptions]{Rule: NoUnnecessaryTypeConstraint},
		rule.Registration[rule.NoOptions]{Rule: NoUnsafeDeclarationMerging},
		rule.Registration[rule.NoOptions]{Rule: NoUnsafeFunctionType},
		rule.Registration[rule.NoOptions]{Rule: NoUnsafeUnaryMinus},
		rule.Registration[rule.NoOptions]{Rule: NoUselessEmptyExport},
		rule.Registration[rule.NoOptions]{Rule: NoWrapperObjectTypes},
		rule.Registration[rule.NoOptions]{Rule: PreferAsConst},
		rule.Registration[rule.NoOptions]{Rule: PreferNamespaceKeyword},
		rule.Registration[TripleSlashReferenceOptions]{
			Rule:   TripleSlashReference,
			Decode: DecodeTripleSlashReferenceOptions,
		},
		rule.Registration[SwitchExhaustivenessCheckOptions]{
			Rule:   SwitchExhaustivenessCheck,
			Decode: rule.DecodeOptionsInto[SwitchExhaustivenessCheckOptions](),
		},
	)
}
