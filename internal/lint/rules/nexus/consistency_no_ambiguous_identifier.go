package nexus

import (
	"regexp"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/ecmascript/binding"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/policy"
)

// alwaysAllowedSingleLetters are the coordinate and math names, where the single letter is the
// conventional spelling and a longer one would read worse.
var alwaysAllowedSingleLetters = map[string]bool{"x": true, "y": true, "z": true}

var (
	handlerNamePattern     = regexp.MustCompile(`(?i)handle|on[A-Z]|event`)
	eventHandlerKeyPattern = regexp.MustCompile(`^on[A-Z]`)
	singleLowercaseLetter  = regexp.MustCompile(`^[a-z]$`)
)

// The rule's messages, whose wording lives in `policy/messages/consistency-no-ambiguous-identifier.json`.
// An `e` finding says which way the rule guessed, as one of the `context` phrase's options.
var (
	consistencyNoAmbiguousIdentifierAmbiguousEText = policy.MessageOf(
		"nexus/consistency-no-ambiguous-identifier",
		"noAmbiguousE",
	)
	consistencyNoAmbiguousIdentifierErrorContext = consistencyNoAmbiguousIdentifierAmbiguousEText.Option(
		"context",
		"error",
	)
	consistencyNoAmbiguousIdentifierEventContext = consistencyNoAmbiguousIdentifierAmbiguousEText.Option(
		"context",
		"event",
	)
	consistencyNoAmbiguousIdentifierUnclearContext = consistencyNoAmbiguousIdentifierAmbiguousEText.Option(
		"context",
		"unclear",
	)
	consistencyNoAmbiguousIdentifierSingleLetterText = policy.MessageOf(
		"nexus/consistency-no-ambiguous-identifier",
		"noSingleLetter",
	)
)

func messageNoAmbiguousE(context policy.MessageOption, suggestedName string) rule.Message {
	return rule.Message{
		Id: consistencyNoAmbiguousIdentifierAmbiguousEText.Id,
		Description: consistencyNoAmbiguousIdentifierAmbiguousEText.Render(
			map[string]string{"suggestedName": suggestedName},
			context,
		),
	}
}

func messageNoSingleLetter(name string) rule.Message {
	return rule.Message{
		Id:          consistencyNoAmbiguousIdentifierSingleLetterText.Id,
		Description: consistencyNoAmbiguousIdentifierSingleLetterText.Render(map[string]string{"name": name}),
	}
}

// ConsistencyNoAmbiguousIdentifier bans single-letter names, judged where they are declared.
//
//	valid:   catch (error) { }
//	valid:   const x = 1
//	valid:   items.sort((a, b) => a - b)
//	valid:   values.map((_, index) => index)
//	invalid: catch (e) { }
//	invalid: const n = 1
//
// A name is reported once, at its declaration, and never at its reads. The declaration is where the
// name was chosen and the only place a rename starts, so a finding at every read repeated one defect
// as many times as the name was used (`const n = 1; return n + n` was three findings), and gave a
// catch-bound `e` two different pieces of advice: an error at the catch, an unclear context at the
// read (#rb0ewgm, cases am01 and am06 in `conformance/textrules`).
//
// The bare `_` is not judged. It is the discard, a value deliberately left unnamed: Go and Swift make
// it one by grammar, and TypeScript spells it the same way by convention (am02).
//
// The exemptions carry the judgment. Coordinates keep x, y, and z, where the single letter is the
// conventional spelling. A sort comparator keeps a and b, because that is the shape everyone reads
// and naming them would be noise. Property names, object keys, import specifiers, and type property
// signatures are skipped entirely: those are not bindings the reader has to track, they are names
// somebody else chose.
//
// `e` gets its own message because it is the ambiguous one: it could be an error or an event, and
// which one it is decides the right name. The rule infers from context and says what it inferred.
//
// No fix, and that is a departure from the TypeScript original, which rewrites `e` to `event` or
// `error` in place. Renaming a binding without following its references through scope leaves every
// other use of `e` pointing at a name that no longer exists, so the fix would turn a style finding
// into a broken file. It is the same trap import-require-node-namespace declines for the same
// reason. The suggested name travels in the message instead, where a reader applies it with the
// scope in front of them.
var ConsistencyNoAmbiguousIdentifier = rule.Rule[rule.NoOptions]{
	Name: "nexus/consistency-no-ambiguous-identifier",
	Run: func(ruleContext rule.Context, options rule.Given[rule.NoOptions]) rule.Listeners {
		return rule.Listeners{
			ast.KindIdentifier: func(node *ast.Node) {
				name := node.Text()

				// Only single lowercase letters are in scope. An uppercase single letter is a type
				// parameter (T, K, V, R, E) and is left alone, and the bare `_` is the discard, which
				// names nothing for a reader to track.
				if !singleLowercaseLetter.MatchString(name) {
					return
				}

				// Judged where it is declared, and only there: its reads are the same defect again.
				if !isAmbiguousDeclarationName(node) {
					return
				}

				// These are names somebody else chose, not bindings this file has to live with.
				//
				// This rule carried its own copy of the question until a census found it beside the
				// abbreviation rule's, one word apart in name and four kinds apart in answer. The
				// shared version is wider in four places and narrower in one, and both directions
				// were measured at this rule rather than argued:
				//
				//	import * as e from 'm'    was reported; a namespace binding is not ours
				//	import e from 'm'         was reported; a default binding is not ours
				//	let v: N.e                was reported; the right half of a qualified name
				//	let v: e                  was reported; a type this file may not own
				//	this.e                    was exempt; the class owns that property, so it is
				//	                          judged now, matching the sibling rule, which carries a
				//	                          named production defect as its evidence
				if binding.IsForeignName(node) {
					return
				}

				if alwaysAllowedSingleLetters[name] {
					return
				}
				if name == "e" {
					suggestedName, context := inferEventOrErrorContext(node)
					ruleContext.ReportNode(node, messageNoAmbiguousE(context, suggestedName))
					return
				}
				// A sort comparator is the one place a and b read correctly.
				if (name == "a" || name == "b") && isInsideSortComparator(node) {
					return
				}

				ruleContext.ReportNode(node, messageNoSingleLetter(name))
			},
		}
	},
}

// isAmbiguousDeclarationName reports whether an identifier is where its name is declared, rather than a
// read or a write of a name declared elsewhere.
//
// TypeScript's own answer is the base, the name of a declaration node, which covers every binding:
// variables, parameters, destructured elements, functions, classes, members, enum members. It is
// adjusted in three places, all by the shape of the syntax:
//
//	{ n }        a shorthand property, which TypeScript counts as declaring the key; for the binding
//	             `n` it is a read, or a write in `({ n } = value)`, and `n` is declared elsewhere
//	n: for ...   a label, which TypeScript does not count because it binds no symbol; the labeled
//	             statement is where it is chosen, and `break n` and `continue n` are its reads
//	this.n = 1   in a JavaScript class, the declaration of a property the class body never names;
//	             see isFirstThisPropertyDeclaration
func isAmbiguousDeclarationName(node *ast.Node) bool {
	parent := node.Parent
	if parent == nil {
		return false
	}
	if parent.Kind == ast.KindShorthandPropertyAssignment {
		return false
	}
	return ast.IsLabelOfLabeledStatement(node) || ast.IsDeclarationName(node) ||
		isFirstThisPropertyDeclaration(node)
}

// isFirstThisPropertyDeclaration reports whether an identifier is `n` in a JavaScript class's first
// `this.n = ...`, for a property `n` the class does not declare as a member.
//
// JavaScript has no field declaration it requires, so a class can create a property by assigning it,
// and TypeScript's binder takes the assignment as the property's declaration. Without this, a
// single-letter property made that way had no declaration to judge and went unreported anywhere,
// where the rule caught it at its reads before it judged declarations only.
//
// The binder's own reading decides it, by syntax and without the checker:
// `ast.GetAssignmentDeclarationKind` names the assignment a this-property declaration (only ever in a
// JavaScript file, since TypeScript requires the member), and its `this` belongs to the class when its
// container is one of the class's members, the containers `getThisClassAndSymbolTable` binds onto a
// class. A static member's `this` is the class itself, so the static and instance properties are
// separate names, as the binder keeps them in separate tables. Every such assignment declares the
// property again, so only the first in source order reports, and a member the class declares is the
// declaration instead, judged where it is written.
func isFirstThisPropertyDeclaration(node *ast.Node) bool {
	class, static, isThisPropertyDeclaration := thisPropertyDeclarationOwner(node)
	if !isThisPropertyDeclaration {
		return false
	}
	name := node.Text()
	for _, member := range class.Members() {
		memberName := member.Name()
		if memberName == nil || (memberName.Kind != ast.KindIdentifier && memberName.Kind != ast.KindStringLiteral) {
			continue
		}
		if memberName.Text() == name && ast.IsStatic(member) == static {
			return false
		}
	}
	var first *ast.Node
	var visit func(current *ast.Node) bool
	visit = func(current *ast.Node) bool {
		if current.Kind == ast.KindIdentifier && current.Text() == name {
			if owner, ownerStatic, declares := thisPropertyDeclarationOwner(current); declares &&
				owner == class && ownerStatic == static {
				first = current
				return true
			}
		}
		return current.ForEachChild(visit)
	}
	class.ForEachChild(visit)
	return first == node
}

// thisPropertyDeclarationOwner answers, for the name in `this.name = ...`, the class whose property
// the assignment declares and whether that property is static. It reports false for anything else.
func thisPropertyDeclarationOwner(node *ast.Node) (*ast.Node, bool, bool) {
	access := node.Parent
	if access == nil || access.Kind != ast.KindPropertyAccessExpression || access.Name() != node {
		return nil, false, false
	}
	assignment := access.Parent
	if assignment == nil || assignment.Kind != ast.KindBinaryExpression ||
		assignment.AsBinaryExpression().Left != access ||
		ast.GetAssignmentDeclarationKind(assignment) != ast.JSDeclarationKindThisProperty {
		return nil, false, false
	}
	container := ast.GetThisContainer(assignment, false, false)
	switch container.Kind {
	case ast.KindConstructor, ast.KindPropertyDeclaration, ast.KindMethodDeclaration, ast.KindGetAccessor,
		ast.KindSetAccessor, ast.KindClassStaticBlockDeclaration:
		if class := container.Parent; class != nil && ast.IsClassLike(class) {
			return class, ast.IsStatic(container), true
		}
	}
	return nil, false, false
}

// isInsideSortComparator reports whether an identifier sits in a function passed as the first
// argument to a `.sort(...)` call.
func isInsideSortComparator(node *ast.Node) bool {
	for current := node.Parent; current != nil; current = current.Parent {
		if current.Kind != ast.KindArrowFunction && current.Kind != ast.KindFunctionExpression {
			continue
		}

		call := current.Parent
		if call == nil || call.Kind != ast.KindCallExpression {
			return false
		}
		callExpression := call.AsCallExpression()
		if callExpression == nil || callExpression.Arguments == nil ||
			len(callExpression.Arguments.Nodes) == 0 || callExpression.Arguments.Nodes[0] != current {
			return false
		}

		callee := callExpression.Expression
		if callee == nil || callee.Kind != ast.KindPropertyAccessExpression {
			return false
		}
		access := callee.AsPropertyAccessExpression()
		if access == nil {
			return false
		}
		calleeName := access.Name()
		return calleeName != nil && calleeName.Text() == "sort"
	}
	return false
}

// inferEventOrErrorContext guesses whether an `e` is an error or an event, and says which it
// guessed so a reader can disagree with the reasoning rather than just the verdict.
func inferEventOrErrorContext(node *ast.Node) (string, policy.MessageOption) {
	// A catch parameter is unambiguous. The identifier's parent is the VariableDeclaration that
	// binds it, and the CatchClause is one level above that, so this reaches through both rather
	// than testing the immediate parent.
	if declaration := node.Parent; declaration != nil && declaration.Kind == ast.KindVariableDeclaration {
		if clause := declaration.Parent; clause != nil && clause.Kind == ast.KindCatchClause {
			if declaration.Name() == node {
				return "error", consistencyNoAmbiguousIdentifierErrorContext
			}
		}
	}

	for current := node.Parent; current != nil; current = current.Parent {
		parent := current.Parent
		if parent == nil {
			break
		}

		switch parent.Kind {
		case ast.KindPropertyAssignment:
			// A handler passed as `{ onClick: (e) => ... }`.
			assignment := parent.AsPropertyAssignment()
			if assignment != nil && assignment.Initializer == current {
				if key := assignment.Name(); key != nil && eventHandlerKeyPattern.MatchString(key.Text()) {
					return "event", consistencyNoAmbiguousIdentifierEventContext
				}
			}

		case ast.KindJsxExpression:
			// A handler passed as `onClick={(e) => ...}`.
			if attribute := parent.Parent; attribute != nil && attribute.Kind == ast.KindJsxAttribute {
				jsxAttribute := attribute.AsJsxAttribute()
				if jsxAttribute != nil {
					if name := jsxAttribute.Name(); name != nil && eventHandlerKeyPattern.MatchString(name.Text()) {
						return "event", consistencyNoAmbiguousIdentifierEventContext
					}
				}
			}

		case ast.KindVariableDeclaration:
			// Assigned to something named like a handler.
			declaration := parent.AsVariableDeclaration()
			if declaration != nil && declaration.Initializer == current {
				if name := declaration.Name(); name != nil && name.Kind == ast.KindIdentifier &&
					handlerNamePattern.MatchString(name.Text()) {
					return "event", consistencyNoAmbiguousIdentifierEventContext
				}
			}
		}

		// A function declaration named like a handler.
		if current.Kind == ast.KindFunctionDeclaration {
			declaration := current.AsFunctionDeclaration()
			if declaration != nil {
				if name := declaration.Name(); name != nil && handlerNamePattern.MatchString(name.Text()) {
					return "event", consistencyNoAmbiguousIdentifierEventContext
				}
			}
		}
	}

	return "event", consistencyNoAmbiguousIdentifierUnclearContext
}
