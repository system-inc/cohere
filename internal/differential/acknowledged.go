// Acknowledged differences are the ones we decided on purpose.
//
// The harness exists to catch a port that drifted from the gate, so every difference it finds is a
// defect in one of the two until someone says otherwise. But the gate is software, and software has
// bugs: when a rule is ported to its stated semantics and the gate cannot see a case it claims to
// enforce, the two disagree and cohere is the one that is right.
//
// "Cohere and the gate agree" is the acceptance test for a faithful port, not the definition of a
// correct rule. Those are the same thing right up until the gate has a bug, and then they are
// opposites. Without somewhere to record that, the only ways forward are to reproduce the bug so the
// numbers match, or to let the harness sit red and lose the signal it exists to give.
package differential

import "fmt"

// AcknowledgedDifference is one finding we expect only one side to report, and why.
//
// Keyed to file, line, and rule rather than to the rule alone. A rule-wide excuse would also swallow
// the next genuine drift in that rule, which is the failure this whole package is built to prevent:
// an excuse broad enough to be convenient is an excuse broad enough to hide a defect.
type AcknowledgedDifference struct {
	File string
	Line int
	Rule string
	// Side is which gate is expected to report it alone.
	Side Side
	// Reason is why this difference is correct, in a sentence a reader can check.
	//
	// Required rather than optional. An acknowledgement with no reason is indistinguishable from a
	// suppression someone added to make a red build green, which is the shape the suppression note
	// in the main output already exists to count.
	Reason string
}

// Key is the identity an acknowledgement shares with the finding it excuses.
func (acknowledged AcknowledgedDifference) Key() string {
	return fmt.Sprintf("%s:%d:%s:%s", acknowledged.File, acknowledged.Line, acknowledged.Rule, acknowledged.Side)
}

// KnownGateDefects are the differences where cohere is right and the gate is wrong.
//
// Two directions. `SideCohere` is a true positive the gate cannot see. `SideGate` is a false positive
// the gate reports and cohere deliberately does not, where a rule was made to tell with the type
// checker rather than given an allowance (cohere's parity doctrine: never worse than ESLint, rule by
// rule, and differing only in its favour). Each gate-side entry names the rule document (`.md` beside
// the rule) that records the condition, and the fixture holding the site as a must-stay-silent case.
//
// Each one names a defect in the tool being replaced, so each is a reason the migration is worth
// doing rather than a cost of it. They are listed here, in source, rather than passed in at the
// command line: an acknowledgement that can be supplied per-run can be supplied by whoever wants a
// green result, and this list is reviewed like any other code.
var KnownGateDefects = []AcknowledgedDifference{
	{
		File: "libraries/structure/libraries/nexus/source/types/Constructor.test.ts",
		Line: 2,
		Rule: "max-classes-per-file",
		Side: SideGate,
		Reason: "every class beyond the first is declared inside a describe or it callback, a test fixture rather " +
			"than a design unit of the file, and cohere counts only classes declared outside every function " +
			"(Kirk, #mbbg6js); core/max_classes_per_file.md, " +
			"TestMaxClassesPerFileDoesNotCountAClassDeclaredInsideAFunction",
	},
	{
		File: "libraries/structure/libraries/nexus/source/types/TypeFunction.test.ts",
		Line: 6,
		Rule: "max-classes-per-file",
		Side: SideGate,
		Reason: "every class beyond the first is declared inside a describe or it callback, a test fixture rather " +
			"than a design unit of the file, and cohere counts only classes declared outside every function " +
			"(Kirk, #mbbg6js); core/max_classes_per_file.md, " +
			"TestMaxClassesPerFileDoesNotCountAClassDeclaredInsideAFunction",
	},
	{
		File: "libraries/structure/libraries/nexus/source/types/UnionFromClasses.test.ts",
		Line: 2,
		Rule: "max-classes-per-file",
		Side: SideGate,
		Reason: "every class beyond the first is declared inside a describe or it callback, a test fixture rather " +
			"than a design unit of the file, and cohere counts only classes declared outside every function " +
			"(Kirk, #mbbg6js); core/max_classes_per_file.md, " +
			"TestMaxClassesPerFileDoesNotCountAClassDeclaredInsideAFunction",
	},
	{
		// api-phi-health, found by @system_cohere_base_rules with ignoreExpressions set (Base 32a17a6e):
		// four class declarations inside describe, at lines 10, 13, 18 and 36.
		File: "libraries/base/source/foundation/dependency-injection/InjectOptional.test.ts",
		Line: 1,
		Rule: "max-classes-per-file",
		Side: SideGate,
		Reason: "every class is declared inside a describe or it callback, a test fixture rather than a design " +
			"unit of the file, and cohere counts only classes declared outside every function " +
			"(Kirk, #mbbg6js); core/max_classes_per_file.md, " +
			"TestMaxClassesPerFileDoesNotCountAClassDeclaredInsideAFunction",
	},
	{
		File: "libraries/structure/source/services/network/NetworkService.ts",
		Line: 228,
		Rule: "storage-no-direct-local-storage",
		Side: SideCohere,
		Reason: "the gate's rule matches window.localStorage only as the object of an outer member " +
			"expression, so it sees window.localStorage.getItem(...) and never window.localStorage " +
			"passed as a value; this line is the latter and is a real violation of the rule's " +
			"stated intent",
	},
	{
		File: "libraries/structure/source/components/buttons/Button.tsx",
		Line: 222,
		Rule: "button-has-type",
		Side: SideGate,
		Reason: "the checker proves `type` is always 'button', 'submit' or 'reset' (a defaulted prop typed as that " +
			"union), so nothing computed can submit a form; react/button_has_type.md, " +
			"TestButtonHasTypeTrustsAProvenType",
	},
	{
		File: "libraries/structure/libraries/nexus/source/types/UnionFromClasses.test.ts",
		Line: 23,
		Rule: "no-unnecessary-type-parameters",
		Side: SideGate,
		Reason: "both findings on this line are the exact type-equality idiom, where a type parameter used once as " +
			"the check type of a deferred conditional is the mechanism; typescript/no_unnecessary_type_parameters.md, " +
			"TestNoUnnecessaryTypeParametersRecognizesTheExactEqualityIdiom",
	},
	{
		File: "libraries/structure/libraries/nexus/source/types/ObjectTypes.ts",
		Line: 93,
		Rule: "no-unnecessary-type-parameters",
		Side: SideGate,
		Reason: "`typeOnly<Shape>(): Shape` takes no value and uses `Shape` only in its return type, so it is a " +
			"phantom-type witness rather than a disguised cast (nothing is handed in to cast from); the type-witness " +
			"principle keeps it silent; typescript/no_unnecessary_type_parameters.md, " +
			"TestNoUnnecessaryTypeParametersTypeWitnesses",
	},
	{
		File: "libraries/structure/libraries/nexus/source/security/random/Random.test.ts",
		Line: 6,
		Rule: "no-confusing-void-expression",
		Side: SideGate,
		Reason: "the call types as undefined, a value, not void; typescript/no_confusing_void_expression.md, " +
			"TestNoConfusingVoidExpressionJudgesVoidNotUndefined",
	},
	{
		File: "libraries/structure/libraries/nexus/source/coordination/TrackedPromise.test.ts",
		Line: 132,
		Rule: "no-confusing-void-expression",
		Side: SideGate,
		Reason: "an await of Promise<undefined> is a value, not void; typescript/no_confusing_void_expression.md, " +
			"TestNoConfusingVoidExpressionJudgesVoidNotUndefined",
	},
	{
		File: "libraries/structure/libraries/nexus/source/validation/schema/StringSchema.ts",
		Line: 37,
		Rule: "class-literal-property-style",
		Side: SideGate,
		Reason: "the getter overrides a concrete base getter, so the suggested field is TS2610 and cannot compile; " +
			"typescript/class_literal_property_style.md, " +
			"TestClassLiteralPropertyStyleDeclinesAConversionThatCannotCompile",
	},
	{
		File: "libraries/structure/source/components/maps/Map.tsx",
		Line: 747,
		Rule: "no-implicit-coercion",
		Side: SideGate,
		Reason: "`1 * zoom` with zoom already a number coerces nothing; core/no_implicit_coercion.md, " +
			"TestNoImplicitCoercionIsSilentWhenNothingIsCoerced",
	},
	{
		File: "libraries/structure/source/components/maps/Map.tsx",
		Line: 765,
		Rule: "no-implicit-coercion",
		Side: SideGate,
		Reason: "`1 * zoom` with zoom already a number coerces nothing; core/no_implicit_coercion.md, " +
			"TestNoImplicitCoercionIsSilentWhenNothingIsCoerced",
	},
	{
		File: "libraries/structure/source/components/maps/Map.tsx",
		Line: 784,
		Rule: "no-implicit-coercion",
		Side: SideGate,
		Reason: "`1 * zoom` with zoom already a number coerces nothing; core/no_implicit_coercion.md, " +
			"TestNoImplicitCoercionIsSilentWhenNothingIsCoerced",
	},
	{
		File: "libraries/structure/source/components/maps/MapDrawing.ts",
		Line: 220,
		Rule: "no-implicit-coercion",
		Side: SideGate,
		Reason: "`1.0 * zoom` with zoom already a number coerces nothing; core/no_implicit_coercion.md, " +
			"TestNoImplicitCoercionIsSilentWhenNothingIsCoerced",
	},
	{
		File: "modules/tasks/TasksWatchCommandLineInterface.ts",
		Line: 305,
		Rule: "no-unmodified-loop-condition",
		Side: SideGate,
		Reason: "the loop awaits and `stopping` is set by a SIGINT handler registered before it, which runs while " +
			"the loop is suspended; core/no_unmodified_loop_condition.md, " +
			"TestNoUnmodifiedLoopConditionSeesAWriterThatRunsWhileTheLoopIsSuspended",
	},
	{
		File: "modules/os/sensation/AhraOsMonitors.ts",
		Line: 548,
		Rule: "no-unmodified-loop-condition",
		Side: SideGate,
		Reason: "the loop awaits and `abortRequested` is set by an abort closure created before it; " +
			"core/no_unmodified_loop_condition.md, " +
			"TestNoUnmodifiedLoopConditionSeesAWriterThatRunsWhileTheLoopIsSuspended",
	},
	{
		File: "modules/os/boot-screens/RainbowMatrix.ts",
		Line: 689,
		Rule: "no-unmodified-loop-condition",
		Side: SideGate,
		Reason: "the loop awaits and `stopped` is set by teardown, reached through a SIGINT handler registered " +
			"before it; core/no_unmodified_loop_condition.md, " +
			"TestNoUnmodifiedLoopConditionSeesAWriterThatRunsWhileTheLoopIsSuspended",
	},
	{
		File: "libraries/structure/libraries/nexus/source/text/Shouting.ts",
		Line: 569,
		Rule: "consistency-no-shouting",
		Side: SideGate,
		Reason: "the `NOT` sits inside a double-quoted phrase the block comment wrapped onto the next line, and a " +
			"quoted all-caps phrase is a literal by ruling; the gate's double-quote mask stops at a newline; " +
			"nexus/consistency_no_shouting.md, TestConsistencyNoShoutingMasksADoubleQuoteThatWraps",
	},
	{
		File: "libraries/structure/libraries/nexus/source/geography/Countries.ts",
		Line: 13,
		Rule: "no-misused-spread",
		Side: SideGate,
		Reason: "a string spread walks code points, which is what mapping ISO letters to regional indicators wants; " +
			"cohere drops upstream's string branch by ruling, since its only repair is Array.from, the same iteration; " +
			"typescript/no_misused_spread.md, TestNoMisusedSpreadLeavesStringSpreadAlone",
	},
	{
		File: "modules/openai/PngTextMetadata.ts",
		Line: 64,
		Rule: "no-misused-spread",
		Side: SideGate,
		Reason: "a Latin-1 filter that wants code points; typescript/no_misused_spread.md, " +
			"TestNoMisusedSpreadLeavesStringSpreadAlone",
	},
	{
		File: "modules/pensieve/PensieveDailies.ts",
		Line: 288,
		Rule: "no-misused-spread",
		Side: SideGate,
		Reason: "quote-mark scanning by code point; typescript/no_misused_spread.md, " +
			"TestNoMisusedSpreadLeavesStringSpreadAlone",
	},
	{
		File: "modules/pensieve/PensieveDailies.ts",
		Line: 326,
		Rule: "no-misused-spread",
		Side: SideGate,
		Reason: "ignored characters filtered by code point; typescript/no_misused_spread.md, " +
			"TestNoMisusedSpreadLeavesStringSpreadAlone",
	},
	{
		File: "libraries/structure/source/components/maps/MapProjection.ts",
		Line: 5,
		Rule: "consistency-require-constant-casing",
		Side: SideGate,
		Reason: "`DegreesToRadians` leaves the file through `export { DegreesToRadians }` and Map.tsx imports it, so " +
			"PascalCase is right; the gate's rule sees only an `export` modifier and calls it file-local; " +
			"TestConsistencyRequireConstantCasingCountsALocalExportClause",
	},
	{
		File: "modules/ollama/OllamaApi.ts",
		Line: 51,
		Rule: "consistency-require-constant-casing",
		Side: SideGate,
		Reason: "`OllamaEnvironment` is exported through `export { OllamaEnvironment }`, which the gate's rule does " +
			"not read; TestConsistencyRequireConstantCasingCountsALocalExportClause",
	},
	{
		File: "app/(os-layout)/_components/row/TaskList.tsx",
		Line: 61,
		Rule: "consistency-require-constant-casing",
		Side: SideGate,
		Reason: "`PriorityOrder` leaves the file through `export { PriorityOrder }` and TaskDetailFields.tsx imports " +
			"it, so PascalCase is right; the gate's rule sees only an `export` modifier and calls it file-local; " +
			"TestConsistencyRequireConstantCasingCountsALocalExportClause",
	},
	{
		File: "modules/google/ads/GoogleAdsClient.ts",
		Line: 57,
		Rule: "consistency-require-constant-casing",
		Side: SideGate,
		Reason: "`GoogleAdsCredentials` leaves the file through `export { GoogleAdsCredentials }` and " +
			"seven Google Ads API files import it, so PascalCase is right; the gate's rule " +
			"sees only an `export` modifier and calls it file-local; " +
			"TestConsistencyRequireConstantCasingCountsALocalExportClause",
	},
}

// acknowledgedIndex is the lookup the comparison uses, built once per run.
func acknowledgedIndex(acknowledged []AcknowledgedDifference) map[string]AcknowledgedDifference {
	index := make(map[string]AcknowledgedDifference, len(acknowledged))
	for _, one := range acknowledged {
		index[one.Key()] = one
	}
	return index
}
