package main

// The reasons that recur in the tables below.
const (
	javaScriptVariable = "JavaScript's var keyword"
	cssVariable        = "CSS's var() function"
	reactPropTypes     = "React's propTypes, a static property a component declares by that name"
	javaScriptMethod   = "JavaScript's String method of that name, which the function ports"
	javaScriptNameText = "text matched against a JavaScript name"
	// javaScriptStringMethod and javaScriptGlobal are keys of tables of JavaScript names a rule looks up.
	javaScriptStringMethod = "a JavaScript String method the rule knows returns a value without side effects"
	javaScriptGlobal       = "a JavaScript global the table describes"
)

// externalNames are names, read in their files, that spell a name outside Go the code refers to by that
// spelling: JavaScript's charCodeAt and var keyword, CSS's var(), React's propTypes. By the declaring file relative
// to the root and the name. Without -rename-owned-spellings each is listed with its reason. With it each is renamed,
// since the Go identifier is ours and only the outside name is someone else's, and its doc comment keeps the
// outside spelling: the doc's opening name takes the new name, every other occurrence of the old one stays, and a
// doc that names the spelling nowhere gains the note, a person's sentence read in the file.
var externalNames = map[string]externalName{
	"internal/lint/rules/core/no_var.go:messageUnexpectedVar": {
		reason: javaScriptVariable, spelling: "var",
		note: "messageUnexpectedVariable is upstream's unexpectedVar message: a `var` declaration.",
	},
	// one-var's and block-scoped-var's own names for a list the var keyword opens, which a rule's spelling alone
	// would rename without citing the keyword.
	"internal/lint/rules/core/one_var.go:oneVarKindVar": {
		reason: javaScriptVariable, spelling: "var",
		note: "oneVariableKindVariable is a declaration list the `var` keyword opens.",
	},
	"internal/lint/rules/core/block_scoped_var.go:blockScopedVarIsVarDeclaration": {
		reason:   javaScriptVariable,
		spelling: "var",
	},
	"internal/lint/rules/core/no_unmodified_loop_condition_test.go:" +
		"TestNoUnmodifiedLoopConditionVarInitializerCountsAsAWrite": {
		reason: javaScriptVariable, spelling: "var",
	},
	"internal/format/css/utilities.go:isVarFunctionNode": {
		reason: cssVariable, spelling: "var",
		note: "isVariableFunctionNode is upstream's isVarFunctionNode: whether a node calls CSS's var().",
	},
	"internal/lint/rules/tailwind/collapse/variant_printer.go:isLoneVar": {
		reason:   cssVariable,
		spelling: "var",
	},
	"internal/lint/rules/tailwind/collapse/data_type_test.go:TestVarShortCircuitBeatsEveryCheck": {
		reason:   cssVariable,
		spelling: "var",
	},
	"internal/lint/rules/tailwind/collapse/data_type_test.go:TestImageVarSkipIsUnreachableThroughInferDataType": {
		reason:   cssVariable,
		spelling: "var",
	},
	"internal/format/css/utilities.go:isPostcssSimpleVarNode": {
		reason:   "the postcss-simple-vars plugin's node, upstream's isPostcssSimpleVarNode",
		spelling: "isPostcssSimpleVarNode",
		note: "isPostcssSimpleVariableNode is upstream's isPostcssSimpleVarNode: a `$$` function a word follows " +
			"with no space between, postcss-simple-vars' interpolation.",
	},
	"internal/lint/rules/structure/react_component_require_properties_parameter.go:messageUsePropertiesNotProps": {
		reason: "the binding `props` the rule asks a component to rename to `properties`", spelling: "props",
		note: "It asks for `properties` where a component's parameter is named `props`.",
	},
	"internal/lint/rules/react/no_unsafe.go:messageUnsafeComponentWillReceiveProps": {
		reason: "React's UNSAFE_componentWillReceiveProps lifecycle method", spelling: "componentWillReceiveProps",
		note: "messageUnsafeComponentWillReceiveProperties reports React's `componentWillReceiveProps`.",
	},
	"internal/lint/rules/react/no_unused_state.go:noUnusedStateIsDerivedStateFromProps": {
		reason: "React's getDerivedStateFromProps lifecycle method", spelling: "getDerivedStateFromProps",
		note: "React names the hook getDerivedStateFromProps.",
	},
	"internal/lint/rules/react/forward_ref_uses_ref.go:canRemoveForwardRef": {
		reason:   "React's forwardRef",
		spelling: "forwardRef",
	},
	"internal/lint/rules/react/default_props_match_prop_types.go:propTypeEntry": {
		reason: reactPropTypes, spelling: "propTypes",
		note: "Upstream reads it from a component's propTypes.",
	},
	"internal/lint/rules/react/default_props_match_prop_types.go:propTypeIsRequired": {
		reason:   reactPropTypes,
		spelling: "propTypes",
	},
	"internal/lint/rules/react/boolean_prop_naming.go:propTypesKeyName": {
		reason:   reactPropTypes,
		spelling: "propTypes",
	},
	"internal/format/css/postcss/scss_tokenizer.go:charCodeAt": {
		reason:   javaScriptMethod,
		spelling: "charCodeAt",
	},
	"internal/format/css/postcss/tokenizer.go:charCodeAt": {
		reason:   javaScriptMethod,
		spelling: "charCodeAt",
	},
	"internal/format/css/selector/tokenize.go:charCodeAt": {
		reason:   javaScriptMethod,
		spelling: "charCodeAt",
	},
	"internal/format/graphql/lexer.go:charCodeAt": {
		reason:   javaScriptMethod,
		spelling: "charCodeAt",
	},
	"internal/format/javascript/utility_text.go:charAt": {
		reason:   javaScriptMethod,
		spelling: "charAt",
	},
	"internal/format/yaml/cst/lexer.go:charAt": {
		reason: "upstream's Lexer.charAt, after JavaScript's String method", spelling: "charAt",
		note: "characterAt is upstream's charAt(n): the code unit n past the lexer's position, or -1 past either end.",
	},
	// An initialism finding, not a vocabulary one: the initialism pass refuses it for the JavaScript global's name
	// in two string literals, and this pass takes it under the same ruling, by its own name.
	"internal/format/yaml/compose/directives.go:decodeURIComponent": {
		reason: "JavaScript's global decodeURIComponent, which the function ports", spelling: "decodeURIComponent",
		note: "It ports JavaScript's decodeURIComponent.", newName: "decodeUriComponent",
	},
}

// externalName is why a name spells someone else's, and what its doc comment keeps when it is renamed.
type externalName struct {
	reason string
	// spelling is the outside name as its own language writes it, which the doc comment names after the rename.
	spelling string
	// note is the comment line a declaration gains when no doc comment of its own names spelling: a whole doc
	// opening with the new name where it has none, or a closing paragraph of the one it has.
	note string
	// newName names a declaration the vocabulary does not flag, which only this table proposes.
	newName string
}

// vocabularyWords are the files whose literals list the vocabulary's words, as words.
var vocabularyWords = map[string]string{
	"internal/lint/rules/nexus/abbreviation_vocabulary_test.go":               "a word of the vocabulary the test lists",
	"internal/lint/rules/nexus/consistency_no_abbreviated_identifier_test.go": "a word of the vocabulary the test lists",
}

// reviewedLiterals are string literals read in their files and judged unable to name the declaration a
// candidate renames, by the name the literal spells, then the file relative to the root, the reason.
var reviewedLiterals = map[string]map[string]string{
	"Props": {
		"internal/lint/rules/structure/react_component_require_properties_type_suffix.go": "a TypeScript type name's suffix the rule checks",
	},
	"Dir": {
		"internal/goengine/rules/correctness_require_declared_walk/analyzer.go": "the name of filepath.Dir, which the analyzer matches in calls",
	},
	"Ref": {
		"internal/lint/ecmascript/react/compiled.go":    javaScriptNameText,
		"internal/lint/rules/react/error_boundaries.go": javaScriptNameText,
		"internal/lint/rules/react/refs.go":             javaScriptNameText,
	},
	"charCodeAt": {
		"internal/lint/rules/nexus/correctness_no_discarded_pure_result.go": javaScriptStringMethod,
	},
	"charAt": {
		"internal/lint/rules/nexus/correctness_no_discarded_pure_result.go": javaScriptStringMethod,
	},
	"decodeURIComponent": {
		"internal/lint/rules/typescript/builtin_globals.go":                          javaScriptGlobal,
		"internal/lint/ecmascript/high_level_intermediate_representation/effects.go": javaScriptGlobal,
	},
	"cur": vocabularyWords,
	"pos": mergeReviews(
		vocabularyWords,
		map[string]string{"internal/format/yaml/compose/oracle_test.go": "a key the oracle's writer prints"},
	),
	"prev": mergeReviews(
		vocabularyWords,
		map[string]string{"internal/lint/rules/react/no_invalid_html_attribute.go": "an HTML rel value"},
	),
	"args": mergeReviews(vocabularyWords, map[string]string{
		"internal/lint/rules/core/no_unused_vars.go":                         "the value of no-unused-vars' args option",
		"internal/lint/rules/nexus/consistency_no_abbreviated_identifier.go": "a JavaScript binding's name the TypeScript rule looks for",
	}),
}

// mergeReviews is base with more's files added.
func mergeReviews(base map[string]string, more map[string]string) map[string]string {
	merged := map[string]string{}
	for file, reason := range base {
		merged[file] = reason
	}
	for file, reason := range more {
		merged[file] = reason
	}
	return merged
}

// nameOverrides choose the name where the word-by-word expansion reads wrong, by the declaring file relative to
// the root and the old name. Each is a person's choice, read in its file.
var nameOverrides = map[string]string{
	// Upstream's /^[a-z0-9]/i: alphanumeric, not an alpha number.
	"internal/format/css/values/tokenize.go:alphaNum": "alphanumeric",
	// Upstream's set of characters that end a number word.
	"internal/format/css/values/tokenize.go:wordEndNum": "endsNumberWord",
	// The finding of react-component-require-properties-parameter, whose expansion would read "use properties, not
	// properties"; its doc's note says what it asks.
	"internal/lint/rules/structure/react_component_require_properties_parameter.go:messageUsePropertiesNotProps": "messageUsePropertiesParameter",
	// Its catalog text, which the rule does not flag, keeps the pair's one name.
	"internal/lint/rules/structure/react_component_require_properties_parameter.go:messageUsePropertiesNotPropsText": "messageUsePropertiesParameterText",
}
