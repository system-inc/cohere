package core

// NoUseBeforeDefineOptions configures which kinds of binding the rule judges.
//
// Every field is a pointer, and that is load-bearing rather than tidy. Six of the seven options
// default to true upstream, so a plain bool would put upstream's default on the wrong side of the
// zero value: a rule reached through the config layer with no options written would see false for
// all of them and switch off almost the entire check while still registering on every file. That is
// the shape the brief calls a rule that passes every fixture and lints nothing, and a pointer is
// what keeps "absent" distinguishable from "written as false".
type NoUseBeforeDefineOptions struct {
	// Functions judges references to function declarations. Defaults to true.
	//
	// Separable because a function declaration is fully hoisted: calling one above its definition
	// works, so a codebase that deliberately puts helpers below their callers is not making the
	// mistake this rule exists to catch. That is why upstream ships the `"nofunc"` string as a
	// shorthand for turning this one option off and nothing else.
	Functions *bool `json:"functions"`

	// Classes judges references to class declarations. Defaults to true.
	Classes *bool `json:"classes"`

	// Variables judges references to variable declarations. Defaults to true.
	Variables *bool `json:"variables"`

	// AllowNamedExports stops reporting a name listed in an `export { a }` clause above its
	// declaration. Defaults to false.
	//
	// The odd one out: it is the only option here whose default is off, because an export clause
	// naming a binding declared below it is a real forward reference by every other measure and
	// upstream only exempts it on request.
	AllowNamedExports *bool `json:"allowNamedExports"`

	// Enums judges references to TypeScript enum declarations. Defaults to true.
	Enums *bool `json:"enums"`

	// Typedefs judges references to TypeScript type aliases and interfaces. Defaults to true.
	Typedefs *bool `json:"typedefs"`

	// IgnoreTypeReferences stops reporting a name used in type position. Defaults to true.
	//
	// Defaulted on because a type reference is erased before anything runs, so a type annotation
	// naming a type declared below it cannot fail at runtime the way a value reference can. The
	// forward reference is real and the consequence is not, which is why upstream separates this
	// from `typedefs`: one asks whether to judge the declaration kind, the other whether to judge
	// the use position.
	IgnoreTypeReferences *bool `json:"ignoreTypeReferences"`
}

// resolve applies each written option over the defaults, leaving an absent key alone.
func (options NoUseBeforeDefineOptions) resolve() noUseBeforeDefineSettings {
	settings := defaultNoUseBeforeDefineSettings()
	if options.Functions != nil {
		settings.functions = *options.Functions
	}
	if options.Classes != nil {
		settings.classes = *options.Classes
	}
	if options.Variables != nil {
		settings.variables = *options.Variables
	}
	if options.AllowNamedExports != nil {
		settings.allowNamedExports = *options.AllowNamedExports
	}
	if options.Enums != nil {
		settings.enums = *options.Enums
	}
	if options.Typedefs != nil {
		settings.typedefs = *options.Typedefs
	}
	if options.IgnoreTypeReferences != nil {
		settings.ignoreTypeReferences = *options.IgnoreTypeReferences
	}
	return settings
}
