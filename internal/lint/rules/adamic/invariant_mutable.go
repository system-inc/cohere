package adamic

import (
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/system-inc/cohere/internal/lint/checking"
	"github.com/system-inc/cohere/internal/lint/checking/flow"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/policy"
)

// invariantMutableText is the rule's message, whose wording lives in `policy/messages/invariant-mutable.json`.
var invariantMutableText = policy.MessageOf("adamic/invariant-mutable", "mutableWidening")

/*
 * InvariantMutable reports a value seen through a wider type at a place that type lets you write (#drbrp8c).
 *
 *     invalid: const animals: Animal[] = dogs;             animals.push(cat) puts a cat in dogs
 *     invalid: const pen: { pet: Animal } = kennel;         pen.pet = cat does the same
 *     invalid: const animals: Map<string, Animal> = dogs;   animals.set('Tom', cat)
 *     valid:   const animals: readonly Animal[] = dogs;     nothing can be written through it
 *     valid:   const pen: { pet: Animal } = { pet: rex };   the literal is held by nobody else
 *
 * # The hole
 *
 * tsc relates a mutable location covariantly: an element of `Dog[]` is accepted where an element of
 * `Animal[]` is wanted, because reading one is safe. Writing one is not, and tsc lets the wider name
 * write. Each shape above is accepted by tsc 6.0.3 under strict and fails on Node with `dog.bark is not
 * a function` (probes h03, h04, h13, h19, h20 on #drbrp8c). Adamic's rule: a mutable location is
 * invariant and a `readonly` one is covariant, which is what tsc already does for `readonly T[]`.
 *
 * # How
 *
 * The file's flow.Walker finds every place a value goes into a typed slot and pairs the value's parts
 * with the slot's. At every pair the target marks mutable, whatever the wider type can write there must be
 * something the original can read: the target's slot type must be assignable to the source's. tsc already
 * proved the other direction when it accepted the site, so together the slot is invariant. That catches a
 * literal widened in a mutable slot (`{ kind: 'Circle' }` into `{ kind: string }`) and a callback slot
 * narrowed the wrong way, and leaves alone a slot whose two types differ only where writing back is safe, as
 * a DOM event handler's `this` does on every element seen as `Element`.
 *
 * It began as identity, by the checker's identity relation, and the four consumers measured why not: an
 * `HTMLElement` seen as an `Element` reported its event handler slots, which differ only in `this` and are
 * safe to write back. `{ x }` against `{ x; y?: number }` is mutually assignable, and that hole is
 * no-optional-widening's.
 *
 * A class instance is nominal-class's: it requires identical type arguments, which covers every mutable
 * part, so this rule does not descend into one and the two rules never report one hole twice.
 *
 * # No fix
 *
 * Every repair changes a type (add `readonly`, or copy), so none preserves meaning, and the edit engine
 * applies only fixes that do.
 */
var InvariantMutable = rule.Rule{
	Name:             "adamic/invariant-mutable",
	NeedsTypeChecker: true,

	Run: func(ctx rule.Context, options any) rule.Listeners {
		if ctx.TypeChecker == nil || ctx.SourceFile == nil {
			return nil
		}
		typeChecker := ctx.TypeChecker
		walker := flow.WalkerFor(ctx)
		// Made once per file, not per site: a judge made per site was a closure per site (#m6tyg79).
		judge := func(pair flow.Pair) (bool, bool) {
			if isClassInstance(pair.Target) {
				return false, false
			}
			if !pair.Mutable {
				return false, true
			}
			// Writing through the wider type stores a target value where the original reads a source
			// value, so every target value must be a source value too.
			return !walker.IsAssignable(pair.Target, pair.Source), true
		}
		return walker.Listeners(func(site flow.Site) {
			// Object intersections are this rule's to pair, and no other's (#b9a0wgy).
			found, wrong := walker.WalkWith(site, judge, flow.WalkOptions{ObjectIntersections: true})
			if !wrong {
				return
			}
			ctx.ReportNode(site.Node, rule.Message{
				Id: "mutableWidening",
				Description: invariantMutableText.Render(map[string]string{
					"source":     type_checking.StableTypeText(typeChecker, site.Source),
					"target":     type_checking.StableTypeText(typeChecker, site.Target),
					"slot":       slotText(ctx.SourceFile, site.Node, found.Path),
					"sourcePart": type_checking.StableTypeText(typeChecker, found.Source),
					"targetPart": type_checking.StableTypeText(typeChecker, found.Target),
				}),
			})
		})
	},
}

// slotText names the mutable slot as a reader would reach it from the expression: `dogs[]`,
// `kennel.pet`, `dogs<Map value>`. A long or multi-line expression is named `value` instead.
func slotText(sourceFile *ast.SourceFile, node *ast.Node, path []flow.Step) string {
	textRange := rule.TokenRange(sourceFile, node)
	expression := sourceFile.Text()[textRange.Pos():textRange.End()]
	if len(expression) > 40 || strings.ContainsAny(expression, "\n\r") {
		expression = "value"
	}
	return expression + flow.PathText(path)
}

// isClassInstance is the instance side of a class, generic or not: `Box<Dog>`, `Shelter`. The class's
// own constructor type (`typeof Shelter`) carries the class's symbol too, and is not one.
func isClassInstance(t *checker.Type) bool {
	if t == nil || t.Flags()&checker.TypeFlagsObject == 0 {
		return false
	}
	if t.ObjectFlags()&checker.ObjectFlagsClass != 0 {
		return true
	}
	return t.ObjectFlags()&checker.ObjectFlagsReference != 0 && t.Target() != nil &&
		t.Target().ObjectFlags()&checker.ObjectFlagsClass != 0
}
