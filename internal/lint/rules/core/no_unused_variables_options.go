package core

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/system-inc/cohere/internal/lint/rule"
)

// NoUnusedVariablesOptions is the rule's configuration surface.
//
// Every field is taken from oxc's `options.rs` rather than from the inventory, which records this
// rule as having no options and is wrong: the option module alone is 891 lines. The defaults are
// ESLint's. Two are not the zero value and are pinned with fixtures: `args` defaults to
// `after-used` rather than to `all`, and `caughtErrors` defaults to `all`. Every ignore pattern
// defaults to nothing, so an unconfigured rule exempts no name.
//
// Every field is read. `vars`, `caughtErrors` and `destructuredArrayIgnorePattern` were decoded and
// never consulted until #6esg2nx, so a config setting them changed nothing; the registry guard
// TestEveryOptionFieldARuleDecodesIsRead now fails on a field like that.
//
// `ignoreClassWithStaticInitBlock`, `ignoreUsingDeclarations` and `reportUsedIgnorePattern` are
// typescript-eslint 8.71's and ESLint 10.8.1's core schema's too, and `enableAutofixRemoval` is
// typescript-eslint's alone, all ported against upstream's rows (#e1zk9s0). Each defaults to false,
// the zero value.
type NoUnusedVariablesOptions struct {
	// Variables is `all` or `local`. Default `all`. `local` skips a binding in the global scope, which
	// only a script has; see isGlobalScopeBinding.
	Variables string `json:"vars"`
	// VariablesIgnorePattern is a regular expression naming variables to skip.
	VariablesIgnorePattern string `json:"varsIgnorePattern"`
	// Arguments is `all`, `after-used`, or `none`. Default `after-used`.
	Arguments string `json:"args"`
	// ArgsIgnorePattern is a regular expression naming parameters to skip.
	ArgsIgnorePattern string `json:"argsIgnorePattern"`
	// CaughtErrors is `all` or `none`. Default `all`, which is oxc's answer and differs from older
	// ESLint releases where it defaulted to `none`. `none` skips every caught error, including each
	// name destructured out of one.
	CaughtErrors string `json:"caughtErrors"`
	// CaughtErrorsIgnorePattern is a regular expression naming catch bindings to skip, and names
	// destructured out of a catch binding with them.
	CaughtErrorsIgnorePattern string `json:"caughtErrorsIgnorePattern"`
	// IgnoreRestSiblings keeps a binding alive when it sits beside a rest element, which is the
	// idiom for omitting a property: `const { removed, ...rest } = o`. Default false, as upstream's.
	// See hasRestSiblingDeclarationOrWrite.
	IgnoreRestSiblings bool `json:"ignoreRestSiblings"`
	// DestructuredArrayIgnorePattern is a regular expression naming array-destructured elements to
	// skip: `const [_first, second] = pair`. It is asked before the other patterns and applies to a
	// parameter or a caught error destructured from an array too. See isDestructuredFromAnArray.
	DestructuredArrayIgnorePattern string `json:"destructuredArrayIgnorePattern"`
	// IgnoreClassWithStaticInitBlock exempts a class whose body holds a `static {}` block, which runs
	// when the class is defined. See hasStaticBlock.
	IgnoreClassWithStaticInitBlock bool `json:"ignoreClassWithStaticInitBlock"`
	// IgnoreUsingDeclarations exempts a `using` or `await using` binding, which exists to dispose of
	// its value. See isUsingDeclaration.
	IgnoreUsingDeclarations bool `json:"ignoreUsingDeclarations"`
	// ReportUsedIgnorePattern reports `usedIgnoredVar` on a name an ignore pattern sets aside that is
	// used, so the pattern cannot hide a name that lies about itself. See reportUsedIgnoredName.
	ReportUsedIgnorePattern bool `json:"reportUsedIgnorePattern"`
	// EnableAutofixRemoval is typescript-eslint's alone (core ESLint has no fixer here). Its one key,
	// imports, makes the removal of an unused import a fix the edit engine applies; off, the default,
	// the same removal is offered as a suggestion. See importRemoval.
	EnableAutofixRemoval NoUnusedVariablesAutofixRemoval `json:"enableAutofixRemoval"`
}

// UnmarshalJSON reads upstream's two forms: the object, decoded strictly, and the bare string
// `"all"` or `"local"`, which is shorthand for `{"vars": ...}` (`["error", "local"]`). A mode value
// outside upstream's enum is refused by key and value.
func (options *NoUnusedVariablesOptions) UnmarshalJSON(raw []byte) error {
	var shorthand string
	if decodeError := json.Unmarshal(raw, &shorthand); decodeError == nil {
		if shorthand != "all" && shorthand != "local" {
			return fmt.Errorf("the string form is \"all\" or \"local\", and the config gives %q", shorthand)
		}
		*options = NoUnusedVariablesOptions{Variables: shorthand}
		return nil
	}
	var fields noUnusedVariablesOptionFields
	if unmarshalOptionsError := rule.UnmarshalOptions(raw, &fields); unmarshalOptionsError != nil {
		return unmarshalOptionsError
	}
	decoded := NoUnusedVariablesOptions(fields)
	for _, mode := range noUnusedVariablesModes {
		value := mode.value(&decoded)
		if value != "" && !slices.Contains(mode.allowed, value) {
			return fmt.Errorf("%s is one of %q, and the config gives %q", mode.key, mode.allowed, value)
		}
	}
	*options = decoded
	return nil
}
