package core

import (
	"fmt"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/verify/internal/rule"
)

// classMemberKey identifies one class member for duplicate detection.
//
// The name alone is not the key. `static foo()` and `foo()` are different members, and so are
// `#foo` and `foo`, so two members collide only when all three agree.
type classMemberKey struct {
	name      string
	isStatic  bool
	isPrivate bool
}

// NoDupeClassMembers flags a class declaring the same member twice.
//
//	valid:   class A { foo() {} bar() {} }
//	valid:   class A { static foo() {} foo() {} }
//	valid:   class A { get foo() {} set foo(value) {} }
//	valid:   class A { [1.0]() {} ['1.0']() {} }
//	invalid: class A { foo() {} foo() {} }
//	invalid: class A { 10() {} 1e1() {} }
//	invalid: class A { 'foo'() {} [`foo`]() {} }
//
// The later declaration silently wins, so the earlier one is dead code that looks live. Nothing in
// the language warns about it and nothing at runtime distinguishes the two, which is what makes it
// worth a rule rather than a review comment.
//
// # A getter and a setter of the same name are one member, not two
//
// `get foo` and `set foo` are the two halves of one accessor and are the reason the check is not a
// plain name comparison. Two getters collide, two setters collide, and a getter with a setter does
// not. Upstream expresses this as "if both are accessors, they must be the same kind to collide",
// and the fourth valid case shows why an ordinary member has to keep colliding with either.
//
// # Keys compare by value and by type, which the corpus pins in both directions
//
//	10 and 1e1          collide, so a numeric key compares by its value rather than its spelling
//	1.0 and '1.0'       do not, so a number and a string are different keys
//	'foo' and `+"`"+`foo`+"`"+`       collide, so the string forms unify across quoting
//	null and ''         do not, so a keyword is not the empty string
//
// A rule comparing source text gets the second and fourth right by accident and the first and third
// wrong. A rule comparing cooked text gets the first right and the second wrong. The type has to
// travel with the value.
var NoDupeClassMembers = rule.Rule{
	Name: "no-dupe-class-members",
	Run: func(ctx rule.Context, options any) rule.Listeners {
		check := func(members *ast.NodeList) {
			if members == nil {
				return
			}

			// Accessor kinds are tracked alongside the key so a getter and a setter of one name can
			// coexist while two of either cannot.
			seen := map[classMemberKey]ast.Kind{}

			for _, member := range members.Nodes {
				name := classMemberName(member)
				if name == nil {
					continue
				}
				key, ok := classMemberKeyOf(member, name)
				if !ok {
					continue
				}

				previousKind, duplicate := seen[key]
				if !duplicate {
					seen[key] = member.Kind
					continue
				}

				// Both accessors and of different kinds is a getter/setter pair rather than a
				// duplicate. Anything else collides, including an accessor against a method.
				bothAccessors := isAccessorKind(member.Kind) && isAccessorKind(previousKind)
				if bothAccessors && member.Kind != previousKind {
					continue
				}

				ctx.ReportNode(name, rule.Message{
					Id: "noDupeClassMembers",
					Description: fmt.Sprintf(
						"This class already declares a member named %s. The later declaration wins "+
							"silently, so the earlier one is dead code that reads as live, and "+
							"nothing in the language or at runtime tells the two apart.", key.name),
				})
			}
		}

		return rule.Listeners{
			ast.KindClassDeclaration: func(node *ast.Node) {
				check(node.AsClassDeclaration().Members)
			},
			ast.KindClassExpression: func(node *ast.Node) {
				check(node.AsClassExpression().Members)
			},
		}
	},
}

// isAccessorKind reports whether a member is a getter or a setter.
func isAccessorKind(kind ast.Kind) bool {
	return kind == ast.KindGetAccessor || kind == ast.KindSetAccessor
}

// classMemberName returns the node naming a member, or nil for one this rule does not judge.
//
// A static block and an index signature name nothing, and a member whose key is computed from an
// expression has no name known before it runs.
func classMemberName(member *ast.Node) *ast.Node {
	switch member.Kind {
	case ast.KindMethodDeclaration, ast.KindPropertyDeclaration,
		ast.KindGetAccessor, ast.KindSetAccessor:
		return member.Name()
	}
	return nil
}

// classMemberKeyOf builds the identity a duplicate is judged against.
//
// The second return is false for a member whose key cannot be known statically, which is a member
// the rule declines rather than guesses at: `[foo]()` and `foo()` are different members because the
// first depends on a variable, and the corpus pins that.
func classMemberKeyOf(member *ast.Node, name *ast.Node) (classMemberKey, bool) {
	text, known := staticClassMemberName(name)
	if !known {
		return classMemberKey{}, false
	}
	return classMemberKey{
		name:      text,
		isStatic:  ast.HasStaticModifier(member),
		isPrivate: name.Kind == ast.KindPrivateIdentifier,
	}, true
}

// staticClassMemberName returns a member key's static value, tagged by type.
//
// The tag is what keeps a number and a string apart when their text agrees. `[1.0]` and `['1.0']`
// are different members and `10` and `1e1` are the same one, so neither the source spelling nor the
// cooked text answers alone: a numeric key is normalized through its value and a string key is not.
func staticClassMemberName(name *ast.Node) (string, bool) {
	switch name.Kind {
	case ast.KindIdentifier, ast.KindPrivateIdentifier:
		// A bare identifier key is a name, and it unifies with the string spellings: `foo()` and
		// `[`+"`"+`foo`+"`"+`]()` are the same member, which upstream's corpus pins in both directions. Only a
		// *bracketed* identifier is a variable, and that arm declines below.
		return "string:" + name.Text(), true

	case ast.KindStringLiteral, ast.KindNoSubstitutionTemplateLiteral:
		return "string:" + name.Text(), true

	case ast.KindNumericLiteral:
		// No normalization: the TypeScript parser already gives `10` and `1e1` the same Text, so a
		// round-trip through a float adds nothing. Measured with a probe rather than assumed, after
		// a mutation replacing the normalizer with the raw text changed no fixture.
		return "number:" + name.Text(), true

	case ast.KindComputedPropertyName:
		expression := ast.SkipParentheses(name.AsComputedPropertyName().Expression)
		if expression == nil {
			return "", false
		}
		// An identifier inside brackets is a variable reference rather than a name, so `[foo]()`
		// and `foo()` are different members and the first is not knowable before the class runs.
		// Recursing without this arm reads the variable's spelling as the key, which reported the
		// pair as duplicates: caught by upstream's own clean case rather than by inspection.
		if expression.Kind == ast.KindIdentifier || expression.Kind == ast.KindPrivateIdentifier {
			return "", false
		}
		return staticClassMemberName(expression)
	}
	return "", false
}
