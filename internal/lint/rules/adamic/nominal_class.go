package adamic

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/system-inc/cohere/internal/lint/checking/flow"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/policy"
)

// The rule's messages, whose wording lives in `policy/messages/nominal-class.json`.
var (
	nominalClassNotAnInstanceText       = policy.MessageOf("adamic/nominal-class", "notAnInstance")
	nominalClassTypeArgumentsDifferText = policy.MessageOf("adamic/nominal-class", "typeArgumentsDiffer")
)

/*
 * NominalClass reports a value accepted as a class type that is not an instance of that class, or is one
 * with different type arguments (#drbrp8c).
 *
 *     invalid: const shelter: AnimalShelter = new DogShelter();             another class, same shape
 *     invalid: const shelter: AnimalShelter = { admit: (dog: Dog) => '' };  a plain object
 *     invalid: const box: Box<Animal> = dogBox;                             Box<Dog>
 *     valid:   const shelter: Shelter = new DogShelter();                   Shelter is an interface
 *     valid:   const box: Box<Dog> = dogBox;
 *
 * # The hole
 *
 * tsc relates classes structurally, and when the target member is a method it compares parameters in
 * both directions. So `DogShelter`, whose `admit` takes only a Dog, is accepted as `AnimalShelter`, whose
 * `admit` takes any Animal, and `shelter.admit(cat)` is `dog.bark is not a function` (probes h01, h18).
 * `Box<Dog>` passes as `Box<Animal>` the same way, even with a private field (adamic's generic.ts), and
 * `put(cat)` then fills the dog box. Adamic 0.1 makes classes nominal and invariant: a class type accepts
 * an instance of that class, with identical type arguments, and nothing else. An instance can still be
 * passed as an interface it satisfies, which tsc checks strictly when the interface's members are written
 * property-style (probe h02), and method-signature-style sees to that.
 *
 * # How
 *
 * At every pair flow.Walk relates, when the target is a class instance type, the source must be an
 * instance of that class or of one derived from it, and for the class itself every type argument must be
 * identical. A derived generic class seen as its generic base is accepted without comparing arguments,
 * since mapping them through the heritage clause is not built; Adamic 0.1 has no `extends`, so this is a
 * gap only outside it, and it is named here rather than guessed at.
 *
 * The rule owns a class instance whole, so it does not descend into one, and invariant-mutable does not
 * either: one hole, one finding.
 *
 * # No fix
 *
 * The repair is a different type or a different value, and only the author can pick it.
 */
var NominalClass = rule.Rule{
	Name:             "adamic/nominal-class",
	NeedsTypeChecker: true,

	Run: func(ctx rule.Context, options any) rule.Listeners {
		if ctx.TypeChecker == nil || ctx.SourceFile == nil {
			return nil
		}
		typeChecker := ctx.TypeChecker
		return flow.Listeners(ctx, func(site flow.Site) {
			var handle policy.MessageHandle
			found, wrong := flow.Walk(typeChecker, site, func(pair flow.Pair) (bool, bool) {
				if !isClassInstance(pair.Target) {
					return false, true
				}
				verdict := nominalVerdict(typeChecker, pair.Source, pair.Target)
				switch verdict {
				case nominalAccepted:
					return false, false
				case nominalUndecided:
					// A union or intersection source: the walk pairs its members one by one.
					return false, true
				case nominalTypeArgumentsDiffer:
					handle = nominalClassTypeArgumentsDifferText
				default:
					handle = nominalClassNotAnInstanceText
				}
				return true, false
			})
			if !wrong {
				return
			}
			ctx.ReportNode(site.Node, rule.Message{
				Id: handle.Id,
				Description: handle.Render(map[string]string{
					"slot":   slotText(ctx.SourceFile, site.Node, found.Path),
					"source": typeChecker.TypeToString(found.Source),
					"target": typeChecker.TypeToString(found.Target),
				}),
			})
		})
	},
}

type nominal int

const (
	nominalAccepted nominal = iota
	nominalUndecided
	nominalNotAnInstance
	nominalTypeArgumentsDiffer
)

// nominalVerdict judges a source against a class instance type.
func nominalVerdict(typeChecker *checker.Checker, source *checker.Type, target *checker.Type) nominal {
	flags := source.Flags()
	switch {
	case flags&(checker.TypeFlagsAny|checker.TypeFlagsNever) != 0:
		// `any` is no-explicit-any's and the no-unsafe rules', and `never` holds no value at all.
		return nominalAccepted
	case flags&checker.TypeFlagsUnion != 0:
		return nominalUndecided
	case flags&checker.TypeFlagsIntersection != 0:
		for _, member := range source.Types() {
			if nominalVerdict(typeChecker, member, target) == nominalAccepted {
				return nominalAccepted
			}
		}
		return nominalNotAnInstance
	case flags&checker.TypeFlagsTypeParameter != 0:
		// `this` inside a class, or a parameter constrained by one: judged by its constraint.
		constraint := checker.Checker_getBaseConstraintOfType(typeChecker, source)
		if constraint == nil || constraint == source {
			return nominalNotAnInstance
		}
		return nominalVerdict(typeChecker, constraint, target)
	}
	if !isClassInstance(source) {
		return nominalNotAnInstance
	}
	targetClass := classSymbol(target)
	if classSymbol(source) == targetClass {
		sourceArguments := checker.Checker_getTypeArguments(typeChecker, source)
		targetArguments := checker.Checker_getTypeArguments(typeChecker, target)
		if len(sourceArguments) != len(targetArguments) {
			return nominalTypeArgumentsDiffer
		}
		for index := range sourceArguments {
			if !checker.Checker_isTypeIdenticalTo(typeChecker, sourceArguments[index], targetArguments[index]) {
				return nominalTypeArgumentsDiffer
			}
		}
		return nominalAccepted
	}
	if derivesFrom(typeChecker, declaredClassType(typeChecker, source), targetClass, map[*ast.Symbol]bool{}) {
		return nominalAccepted
	}
	return nominalNotAnInstance
}

// classSymbol is the class an instance type is an instance of.
func classSymbol(t *checker.Type) *ast.Symbol {
	if t.ObjectFlags()&checker.ObjectFlagsReference != 0 && t.Target() != nil {
		return t.Target().Symbol()
	}
	return t.Symbol()
}

// declaredClassType is the class's own declared type, whose base types the checker resolves.
func declaredClassType(typeChecker *checker.Checker, instance *checker.Type) *checker.Type {
	if instance.ObjectFlags()&checker.ObjectFlagsReference != 0 && instance.Target() != nil {
		return instance.Target()
	}
	return instance
}

// derivesFrom walks a class's base types for the target class.
func derivesFrom(typeChecker *checker.Checker, declared *checker.Type, targetClass *ast.Symbol, seen map[*ast.Symbol]bool) bool {
	if declared == nil || declared.ObjectFlags()&(checker.ObjectFlagsClass|checker.ObjectFlagsInterface) == 0 {
		return false
	}
	for _, base := range checker.Checker_getBaseTypes(typeChecker, declared) {
		baseClass := classSymbol(base)
		if baseClass == nil || seen[baseClass] {
			continue
		}
		if baseClass == targetClass {
			return true
		}
		seen[baseClass] = true
		if derivesFrom(typeChecker, declaredClassType(typeChecker, base), targetClass, seen) {
			return true
		}
	}
	return false
}
