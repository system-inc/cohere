package react

import (
	"encoding/json"
	"fmt"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/ecmascript/imports"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// forbidPropTypesDefaultForbid is upstream's `DEFAULTS`.
var forbidPropTypesDefaultForbid = []string{"any", "array", "object"}

// DefaultForbidPropTypesOptions is the unconfigured answer.
func DefaultForbidPropTypesOptions() ForbidPropTypesOptions {
	return ForbidPropTypesOptions{}
}

// DecodeForbidPropTypesOptions reads this rule's configuration from the config layer.
//
// Hand-rolled rather than `rule.DecodeOptionsInto` for one key. `forbid` has to be distinguishable
// as absent, because absent means upstream's three defaults while an explicitly empty list forbids
// nothing. The generic helper would decode both to a nil slice and silently turn `forbid: []` into
// the default list, which is the opposite of what it asks for. Measured on the installed build:
// `{forbid: []}` over `PropTypes.any` is clean.
func DecodeForbidPropTypesOptions(raw []byte) (ForbidPropTypesOptions, error) {
	options := DefaultForbidPropTypesOptions()
	if len(raw) == 0 {
		return options, nil
	}
	// Lenient on purpose: eslint-plugin-react 7.37.5's schema sets `additionalProperties: true` on
	// this object, so ESLint loads an extra key and so must this.
	if decodeError := json.Unmarshal(raw, &options); decodeError != nil {
		return options, decodeError
	}
	return options, nil
}

// forbidPropTypesMessage builds the finding, which names the offending type.
func forbidPropTypesMessage(target string) rule.Message {
	return rule.Message{
		Id: "forbiddenPropType",
		Description: fmt.Sprintf(
			"Prop type %q is forbidden here. A prop typed this loosely documents nothing and "+
				"checks nothing: every reader still has to find a call site to learn what shape "+
				"is really expected, and a wrong shape reaches the component unreported. Declare "+
				"the shape it actually takes instead.",
			target,
		),
	}
}

// ForbidPropTypes flags prop type declarations naming a type the configuration refuses.
//
//	valid:   Component.propTypes = { a: PropTypes.string }
//	valid:   Component.propTypes = { a: PropTypes.shape({ b: PropTypes.string }) }
//	invalid: Component.propTypes = { a: PropTypes.any }
//	invalid: Component.propTypes = { a: PropTypes.arrayOf(PropTypes.object) }
//	invalid: class C { static propTypes = { a: PropTypes.array } }
//
// Ported from `react/forbid-prop-types` in `eslint-plugin-react`, read from the clone at
// `lib/rules/forbid-prop-types.js`. Three options, one message, no fixer. Everything below was
// measured by driving the installed build (7.37.5) rather than reasoned from the source.
//
// # There is no file-suffix gate
//
// Measured under `.tsx`, `.jsx` and `.js`: all three report.
//
// # It does not ask whether anything is a component
//
// This reads as a React rule and is a purely syntactic one. A `static propTypes` on a plain class
// with no heritage reports, and so does `o.propTypes = {...}` on a bare object literal. Measured
// both ways. That is upstream being loose rather than upstream being right, and a port that added a
// component gate would go silent on inputs upstream reports while every imported fixture stayed
// green, because the corpus writes real components throughout.
//
// # What is looked for, and where
//
// Six shapes carry a prop types object, and all six are reachable:
//
//	class C { static propTypes = {...} }              a class property
//	C.propTypes = {...}                               an assignment to a member
//	createReactClass({ propTypes: {...} })            a property of an object literal
//	class C { static get propTypes() { return {...} } } a getter's return value
//	C.propTypes = someIdentifier                      an identifier resolved to its object
//	{ a: PropTypes.shape({...}) }                     nested inside a shape call
//
// `contextTypes` and `childContextTypes` are checked only when the matching option is on, which is
// upstream's default of off for both. Measured.
//
// # What counts as a forbidden type
//
// The value is unwrapped in three ways before its name is read, and each was measured:
//
//	PropTypes.any                reports `any`
//	PropTypes.any.isRequired     reports `any`; the `isRequired` member is peeled off first
//	PropTypes.arrayOf(X)         reports on the ARGUMENT's name rather than on `arrayOf`, so
//	                             `arrayOf(PropTypes.object)` reports `object`
//	a: any                       reports `any`; a bare identifier is read as the type name
//	PropTypes.oneOfType([...])   SILENT, because the argument is an array literal rather than a
//	                             member expression or an identifier
//
// The finding always points at the property, not at the value, so `a: PropTypes.any` underlines the
// whole `a: PropTypes.any`. For a nested shape it underlines the inner property. Measured by
// slicing the reported range.
//
// # The foreign-package guard, and it is module-level state
//
// Upstream tracks imports across the file: importing anything named `PropTypes` from a module that
// is neither `react` nor `prop-types` sets a flag that makes every subsequent check demand the
// local name match. Measured: `import { PropTypes } from 'other-lib'` makes an otherwise reporting
// file clean, while the same code importing from `prop-types` reports, and an aliased
// `import PT from 'prop-types'` reports on `PT.any`.
//
// The state is per file and the import declaration may sit after the usage in source order, so the
// imports are collected in one pass before anything is judged. Upstream gets the same effect from
// ESLint visiting the whole program, and reproducing it needs an explicit pre-pass here.
//
// # One arm is not reproduced, and the reason is a missing substrate rather than a decision
//
// Upstream also unwraps a "prop wrapper" call, so `C.propTypes = forbidExtraProps({...})` is looked
// inside. Which functions count comes from `settings.propWrapperFunctions`, a shared setting read
// by four rules in this plugin. Our `rule.Context` exposes `SourceFile`, `Program`, `TypeChecker`,
// `Report` and `FileCache` and no path to configuration settings, and the setting is not in this
// rule's own schema so it cannot be carried as a rule option without inventing a surface the other
// three rules would not share.
//
// Measured cost: 14 of upstream's 111 corpus cases carry that setting, all of them reporting, and
// they assert 26 of the 82 findings. Every one is the same shape, a props object wrapped in a call.
// The other 97 cases, including all 48 passing ones, are reproduced here.
//
// This is a stated divergence rather than a silent one, and it costs findings rather than inventing
// them: a wrapped props object is simply not looked inside. If a settings path is added to
// `rule.Context`, the arm is `checkNode`'s call-expression branch and the 14 cases are in the
// corpus waiting.
//
// # The same missing setting makes a sibling rule unportable outright, which is worth knowing here
//
// `react/prefer-exact-props` is on the audit as an unported line item rated Yes with no violations,
// and it is not work outstanding. The same `settings.propWrapperFunctions` this arm needs is the
// only input its reporting branch has: `getExactPropWrapperFunctions(context)` at its line 40 is
// where `exactWrappers` comes from, and its `propTypes` message requires that set to be non-empty.
//
// Where this rule loses 14 of 111 cases to the gap, that one loses everything. Measured against the
// installed 7.37.5 build on identical input:
//
//	no settings                    0 findings
//	settings.propWrapperFunctions  1 finding, messageId propTypes
//
// Its corpus is 31 cases, 17 of which carry a settings block. Replaying all 12 of its reporting
// cases with no settings: all twelve go silent, none survive. Its other message id is `flow` and
// needs Flow annotations a TypeScript tree does not have. Ahra configures `propWrapperFunctions`
// nowhere.
//
// So a port of it would register, pass a clean fixture set, and be structurally incapable of firing
// here, which is the shape the audit's zero cannot distinguish from a clean tree. It also ships no
// fixer, despite reading as though it does: its `meta` block is docs, messages and schema, and the
// string `fixer` does not appear in the file.
//
// Recorded here rather than in a rule file of its own, because this is the file that already
// explains the missing surface and the next reader chasing that audit line will arrive at the
// setting before they arrive at the rule.
var ForbidPropTypes = rule.Rule[ForbidPropTypesOptions]{
	Name: "react/forbid-prop-types",
	// Declared for one arm only: `C.propTypes = someIdentifier` resolves that identifier to the
	// object literal it was declared with, which upstream answers from its own variable index.
	// Every other arm is syntactic, so a nil checker costs exactly that arm rather than the rule,
	// and `resolveToObjectLiteral` guards for it. The fixture below pins that the typed harness is
	// required, so a later revert fails loudly instead of silently losing one shape.
	NeedsTypeChecker: true,
	Run: func(ruleContext rule.Context, options rule.Given[ForbidPropTypesOptions]) rule.Listeners {
		settings, ok := options.Read()
		if !ok {
			// A rule configured as bare `"error"` arrives with nil options, which type-asserts to
			// the zero value. That happens to be the right default here, since a nil Forbid means
			// upstream's three defaults, but it is written out so the reasoning is at the line.
			settings = DefaultForbidPropTypesOptions()
		}

		return rule.Listeners{
			ast.KindSourceFile: func(node *ast.Node) {
				walker := &forbidPropTypesWalker{ruleContext: ruleContext, settings: settings}
				walker.collectImports(node)
				walker.walk(node)
			},
		}
	},
}

// forbidPropTypesFirstLocalName returns the first local binding an import declaration introduces.
//
// Upstream reads `node.specifiers[0].local.name`, which is the default import when there is one and
// otherwise the first named specifier, so the search order below is upstream's rather than a
// preference. A namespace import sits between the two in the same slot ESTree would list first
// after a default, which is why it is consulted before the named list.
//
// `imports.BindingsOf` answers which of the three shapes the statement carries, with the default,
// the namespace and the named specifiers already separated, so the kind switch this used to hold is
// the utility's rather than each rule's.
func forbidPropTypesFirstLocalName(node *ast.Node) string {
	bindings := imports.BindingsOf(node)

	if bindings.Default != nil && bindings.Default.Kind == ast.KindIdentifier {
		return bindings.Default.Text()
	}
	if bindings.Namespace != nil {
		if name := bindings.Namespace.Name(); name != nil {
			return name.Text()
		}
	}
	for _, specifier := range bindings.Named {
		if name := specifier.Name(); name != nil {
			return name.Text()
		}
	}
	return ""
}

// forbidPropTypesLocalNameForImported finds the local name a named import binds for one imported
// name, or "".
//
// `import { PropTypes as PT }` binds `PT` locally for the imported `PropTypes`, and upstream reads
// exactly that pair. Only the named list can carry a rename, which is why this reads that field
// alone rather than the whole Bindings.
func forbidPropTypesLocalNameForImported(node *ast.Node, imported string) string {
	for _, element := range imports.BindingsOf(node).Named {
		specifier := element.AsImportSpecifier()
		local := specifier.Name()
		if local == nil {
			continue
		}
		// `PropertyName` is the imported name when the specifier renames, and nil when it does not,
		// in which case the local name is also the imported one.
		importedName := specifier.PropertyName
		if importedName == nil {
			if local.Text() == imported {
				return local.Text()
			}
			continue
		}
		if importedName.Kind == ast.KindIdentifier && importedName.Text() == imported {
			return local.Text()
		}
	}
	return ""
}

// forbidPropTypesBindsLocalName reports whether an import declaration binds a given local name.
//
// Upstream tests `node.specifiers.some((x) => x.local.name === 'PropTypes')`, which is the local
// side rather than the imported one, so `import { Foo as PropTypes } from 'other'` trips the guard.
// All three binding shapes are searched because `specifiers` holds all three together upstream.
func forbidPropTypesBindsLocalName(node *ast.Node, local string) bool {
	bindings := imports.BindingsOf(node)

	if bindings.Default != nil && bindings.Default.Kind == ast.KindIdentifier &&
		bindings.Default.Text() == local {
		return true
	}
	if bindings.Namespace != nil {
		if name := bindings.Namespace.Name(); name != nil && name.Text() == local {
			return true
		}
	}
	for _, specifier := range bindings.Named {
		if name := specifier.Name(); name != nil && name.Text() == local {
			return true
		}
	}
	return false
}

// forbidPropTypesTypeName reads the type name off a value, which upstream does two ways.
//
// A member expression contributes its property name, so `PropTypes.any` gives `any`; a bare
// identifier contributes its own name, so `a: any` gives `any`. Anything else, notably an array
// literal, contributes nothing, which is why `oneOfType([...])` is silent.
func forbidPropTypesTypeName(node *ast.Node) string {
	if node == nil {
		return ""
	}
	switch node.Kind {
	case ast.KindPropertyAccessExpression:
		if name := node.AsPropertyAccessExpression().Name(); name != nil &&
			name.Kind == ast.KindIdentifier {
			return name.Text()
		}
	case ast.KindIdentifier:
		return node.Text()
	}
	return ""
}
