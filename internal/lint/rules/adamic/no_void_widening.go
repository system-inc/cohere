package adamic

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
	type_checking "github.com/system-inc/cohere/internal/lint/checking"
	"github.com/system-inc/cohere/internal/lint/checking/flow"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/policy"
)

var voidWideningText = policy.MessageOf("adamic/no-void-widening", "voidWidening")

// NoVoidWidening refuses reading a hidden return value through a type that promises
// something about it (#cs0p22v). A void-typed function may return any value: assigning
// () => 'x' to () => void is legal. Relating that function to () => number | void
// lets a truthiness check expose the string as a number. The fixtures compile under
// strict tsc and throw in Node. The same hole survives containers and Promise.resolve.
//
// unknown exposes no unchecked value and is deliberately left to Adamic's compiler.
// Direct void-to-{} is already rejected by tsc. Value-to-void remains legal.
var NoVoidWidening = rule.Rule{
	Name:             "adamic/no-void-widening",
	NeedsTypeChecker: true,
	ProgramReads:     rule.ReadsCompilerOptions | rule.ReadsDefaultLibrary,
	Run: func(ctx rule.Context, options any) rule.Listeners {
		if ctx.TypeChecker == nil || ctx.SourceFile == nil {
			return nil
		}
		tc := ctx.TypeChecker
		walker := flow.WalkerFor(ctx)
		judge := func(pair flow.Pair) (bool, bool) {
			// Judge the whole callable union before the walker chooses a safe member.
			// Its call result includes the value-returning member even when the value
			// itself entered through the void-returning member.
			source := checker.Checker_getSignaturesOfType(tc, pair.Source, checker.SignatureKindCall)
			target := checker.Checker_getSignaturesOfType(tc, pair.Target, checker.SignatureKindCall)
			if len(source) == 1 && len(target) == 1 &&
				checker.Checker_getReturnTypeOfSignature(tc, source[0]).Flags()&checker.TypeFlagsVoid != 0 &&
				readsVoidValue(tc, checker.Checker_getReturnTypeOfSignature(tc, target[0])) {
				return true, false
			}
			// Pair the promise's value directly, rather than descending into then's
			// generic callback signatures. Promise<void> may resolve to the hidden value.
			if isVoidPromise(pair.Source) && isVoidPromise(pair.Target) && pair.Source.Target() == pair.Target.Target() {
				from := checker.Checker_getTypeArguments(tc, pair.Source)
				to := checker.Checker_getTypeArguments(tc, pair.Target)
				return len(from) == 1 && len(to) == 1 && from[0].Flags()&checker.TypeFlagsVoid != 0 && readsVoidValue(tc, to[0]), false
			}
			return false, true
		}
		return walker.Listeners(func(site flow.Site) {
			found, wrong := walker.Walk(site, judge)
			if !wrong {
				return
			}
			ctx.ReportNode(site.Node, rule.Message{Id: "voidWidening", Description: voidWideningText.Render(map[string]string{
				"slot":   slotText(ctx.SourceFile, site.Node, found.Path),
				"source": type_checking.StableTypeText(tc, found.Source),
				"target": type_checking.StableTypeText(tc, found.Target),
			})})
		})
	},
}

// Only any or a union exposing a readable value is this hole. In particular,
// unknown requires a check, and {} | void promises nothing about a truthy value.
func readsVoidValue(tc *checker.Checker, target *checker.Type) bool {
	if target.Flags()&checker.TypeFlagsAny != 0 {
		return true
	}
	if target.Flags()&checker.TypeFlagsUnion == 0 {
		return false
	}
	for _, member := range target.Types() {
		if member.Flags()&(checker.TypeFlagsStringLike|checker.TypeFlagsNumberLike|checker.TypeFlagsBooleanLike|checker.TypeFlagsBigIntLike|checker.TypeFlagsESSymbolLike|checker.TypeFlagsNonPrimitive) != 0 {
			return true
		}
		if member.Flags()&checker.TypeFlagsObject != 0 &&
			(len(checker.Checker_getPropertiesOfType(tc, member)) != 0 || len(checker.Checker_getSignaturesOfType(tc, member, checker.SignatureKindCall)) != 0) {
			return true
		}
	}
	return false
}

// Like flow's library containers, Promise is recognized in declaration files;
// a local class with an unused type argument merely wearing the name is not one.
func isVoidPromise(t *checker.Type) bool {
	if t.ObjectFlags()&checker.ObjectFlagsReference == 0 || t.Target() == nil ||
		t.Target().Symbol() == nil || t.Target().Symbol().Name != "Promise" {
		return false
	}
	for _, declaration := range t.Target().Symbol().Declarations {
		if sourceFile := ast.GetSourceFileOfNode(declaration); sourceFile != nil && sourceFile.IsDeclarationFile {
			return true
		}
	}
	return false
}
