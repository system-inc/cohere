package rule

import (
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/go/ir"
)

// FunctionCheck is a check that judges one function of staticcheck's intermediate representation at a time. The
// engine's dispatcher walks a package's functions once and calls every registered check on each, in registry order,
// so a check keeps no state from one call to the next, reads everything outside the function through context, and
// never recovers: the dispatcher recovers per check per package and records a crash under the check's rule name.
type FunctionCheck func(context *Context, function *ir.Function)

// Context is what a FunctionCheck reads beyond the function it judges, and where it reports. It is the same for every
// function of a package.
type Context struct {
	// Package is the package the function belongs to.
	Package *types.Package
	// TypesInfo is the type information of the package's syntax.
	TypesInfo *types.Info
	// Fset holds the positions of the package's files.
	Fset *token.FileSet

	// ruleName is the name the check was registered under, which Report stamps on each finding, so a ported check
	// can take the name of the rule it replaces and the settings and `//cohere:ignore` lines naming it keep working.
	ruleName string
	// sink receives each finding with the rule name it is reported under.
	sink func(ruleName string, diagnostic analysis.Diagnostic)
}

// NewContext is the context a check registered as ruleName reads over one package, its findings handed to sink. Only
// the code that calls checks builds one: the engine's dispatcher, and until it lands the one wrapper that runs a check
// as an analyzer.
func NewContext(
	ruleName string,
	typesPackage *types.Package,
	typesInfo *types.Info,
	fileSet *token.FileSet,
	sink func(ruleName string, diagnostic analysis.Diagnostic),
) *Context {
	return &Context{Package: typesPackage, TypesInfo: typesInfo, Fset: fileSet, ruleName: ruleName, sink: sink}
}

// Report hands diagnostic to the sink under the rule name the check was registered under. The check never names its
// rule itself.
func (context *Context) Report(diagnostic analysis.Diagnostic) {
	context.sink(context.ruleName, diagnostic)
}
