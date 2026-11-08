package rule

import (
	"flag"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/go/ir"
)

// FunctionCheck is a check that judges one function of staticcheck's intermediate representation at a time, a
// Listener's Function. The engine's dispatcher walks a package's functions once and calls every registered check on
// each, in registry order, so a check keeps nothing from one package to the next, reads everything outside the
// function through context, and never recovers: the dispatcher recovers per check per package and records a crash
// under the check's rule name.
type FunctionCheck func(context *Context, function *ir.Function)

// Context is what a check reads beyond what it is called with, and where it reports. Each rule has its own over each
// package, the same for every call on that package.
type Context struct {
	// Package is the package the check judges.
	Package *types.Package
	// TypesInfo is the type information of the package's syntax.
	TypesInfo *types.Info
	// Fset holds the positions of the package's files.
	Fset *token.FileSet
	// Files is the package's syntax, one file for each of its Go files.
	Files []*ast.File
	// ReadFile reads a file of the package, such as an assembly or embedded file, as the pass's ReadFile does,
	// refusing a file the package does not hold.
	ReadFile func(filename string) ([]byte, error)
	// IsGenerated is staticcheck's own judgment of whether filename, a file as a finding's display position names it,
	// was generated, read from honnef.co/go/tools/analysis/facts/generated, the judgment report.FilterGenerated
	// reads, so a check ported from staticcheck leaves alone the files staticcheck's leaves alone.
	IsGenerated func(filename string) bool
	// TypesSizes is the sizes of the package's types under the target's word size and alignment, as the pass holds
	// them.
	TypesSizes types.Sizes

	// ruleName is the name the check was registered under, which Report stamps on each finding, so a ported check
	// can take the name of the rule it replaces and the settings and `//cohere:ignore` lines naming it keep working.
	ruleName string
	// sink receives each finding with the rule name it is reported under.
	sink func(ruleName string, diagnostic analysis.Diagnostic)
	// resultOf is the results of what the dispatcher's pass required, the union of every listener's requirements.
	resultOf map[*analysis.Analyzer]any
	// requires is the analyzers this rule's listener requires, the only ones whose results ResultOf hands it, so a
	// check never comes to depend on what another rule happened to require.
	requires []*analysis.Analyzer
	// options is the flags of the rule's own analyzer, which the engine sets from the rule's settings.
	options *flag.FlagSet
}

// NewContext is the context a check registered as ruleName reads over the package pass analyzes, its findings handed
// to sink. requires is what the rule's listener requires, each of which pass must have required too, options is the
// flags of the rule's own analyzer, and isGenerated is staticcheck's judgment of the package's files, which the
// dispatcher reads once for every rule. Only the code that calls checks builds one: the engine's dispatchers.
func NewContext(
	ruleName string,
	pass *analysis.Pass,
	requires []*analysis.Analyzer,
	options *flag.FlagSet,
	isGenerated func(filename string) bool,
	sink func(ruleName string, diagnostic analysis.Diagnostic),
) *Context {
	return &Context{
		Package:     pass.Pkg,
		TypesInfo:   pass.TypesInfo,
		Fset:        pass.Fset,
		Files:       pass.Files,
		ReadFile:    pass.ReadFile,
		IsGenerated: isGenerated,
		TypesSizes:  pass.TypesSizes,
		ruleName:    ruleName,
		sink:        sink,
		resultOf:    pass.ResultOf,
		requires:    requires,
		options:     options,
	}
}

// Report hands diagnostic to the sink under the rule name the check was registered under. The check never names its
// rule itself.
func (context *Context) Report(diagnostic analysis.Diagnostic) {
	context.sink(context.ruleName, diagnostic)
}

// ResultOf is the result of analyzer over the package, which the rule's listener must require. Asked for one it does
// not require, ResultOf panics rather than returning an error, since the read is a mistake in the check, not
// something a package can cause, and the dispatcher turns the panic into that rule's crash on the package, naming
// the analyzer, while every other rule runs on. A nil it could return instead would read as an analyzer with
// nothing to say.
func (context *Context) ResultOf(analyzer *analysis.Analyzer) any {
	if !slices.Contains(context.requires, analyzer) {
		// Invariant: a check reads only what its listener requires. A read past that is a mistake in the check, and
		// the dispatcher's recover makes it this rule's crash on the package, so it never stops the process.
		panic(fmt.Sprintf("rule %s reads the result of analyzer %s, which its listener does not require; list it in "+
			"the listener's Requires", context.ruleName, analyzer.Name))
	}
	return context.resultOf[analyzer]
}

// Option is the value of the rule's own option name, a flag of the rule's analyzer, as the settings set it. Asked for
// an option the analyzer has no flag for, it panics, for ResultOf's reason.
func (context *Context) Option(name string) flag.Value {
	option := context.options.Lookup(name)
	if option == nil {
		// Invariant: a check reads only options its rule's analyzer declares. A read past that is a mistake in the
		// check, and the dispatcher's recover makes it this rule's crash on the package, so it never stops the process.
		panic(fmt.Sprintf("rule %s reads option %s, which its analyzer has no flag for", context.ruleName, name))
	}
	return option.Value
}
