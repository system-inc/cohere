package no_for_in_array

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/system-inc/verify/internal/upstream/tsgolint/rule"
	"github.com/system-inc/verify/internal/upstream/tsgolint/utils"
)

func buildForInViolationMessage() rule.RuleMessage {
	return rule.RuleMessage{
		Id:          "forInViolation",
		Description: "For-in loops over arrays skips holes, returns indices as strings, and may visit the prototype chain or other enumerable properties. Use a more robust iteration method such as for-of or array.forEach instead.",
	}
}

var NoForInArrayRule = rule.Rule{
	// Upstream spells this "no-for-in-array-rule". That trailing "-rule" is a typo, and correcting
	// it is the one edit this vendored file carries.
	//
	// It is not cosmetic. verify keys the catalog, the config, and suppression comments on this
	// string, so shipping upstream's spelling would register a rule no `VerifySettings.json` entry
	// enables, that the inventory's `typescript/no-for-in-array` never matches, and that no
	// `verify-disable` comment an author would actually write could silence.
	//
	// Measured rather than assumed before editing: all 38 rules in tsgolint's `internal/rules/` were
	// fetched and their Name compared against their directory, and this is the ONLY one that
	// disagrees. tsgolint's own `cmd/tsgolint/main.go` passes `r.Name` straight through to its
	// reporter with no mapping table, so upstream really does emit the suffixed name; the typo is
	// live there rather than absorbed somewhere downstream.
	//
	// Both other references spell it without the suffix: oxc declares `NoForInArray(tsgolint)` and
	// `@typescript-eslint` 8.67.0 declares `name: 'no-for-in-array'`.
	Name: "no-for-in-array",
	Run: func(ctx rule.RuleContext, options any) rule.RuleListeners {
		hasArrayishLength := func(t *checker.Type) bool {
			lengthProperty := checker.Checker_getPropertyOfType(ctx.TypeChecker, t, "length")
			if lengthProperty == nil {
				return false
			}

			return utils.IsTypeFlagSet(checker.Checker_getTypeOfSymbol(ctx.TypeChecker, lengthProperty), checker.TypeFlagsNumberLike)
		}
		isArrayLike := func(t *checker.Type) bool {
			return utils.TypeRecurser(t, func(t *checker.Type) bool {
				return utils.GetNumberIndexType(ctx.TypeChecker, t) != nil && hasArrayishLength(t)
			})
		}

		return rule.RuleListeners{
			ast.KindForInStatement: func(node *ast.Node) {
				t := utils.GetConstrainedTypeAtLocation(ctx.TypeChecker, node.AsForInOrOfStatement().Expression)

				if isArrayLike(t) {
					ctx.ReportRange(
						utils.GetForStatementHeadLoc(ctx.SourceFile, node),
						buildForInViolationMessage(),
					)
				}
			},
		}
	},
}
