package rule

import (
	"flag"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
)

// Context is what a check reads beyond what it is called with, and where it reports. Each rule has its own over each
// package, the same for every call on that package.
type Context struct {
	// Package is the package the check judges.
	Package *types.Package
	// TypesInfo is the type information of the package's syntax.
	TypesInfo *types.Info
	// FileSet holds the positions of the package's files.
	FileSet *token.FileSet
	// Files is the package's syntax, one file for each of its Go files.
	Files []*ast.File
	// ReadFile reads a file of the package, such as an assembly or embedded file, as the pass's ReadFile does,
	// refusing a file the package does not hold.
	ReadFile func(filename string) ([]byte, error)
	// IsGenerated is staticcheck's own judgment of whether filename was generated, read from
	// honnef.co/go/tools/analysis/facts/generated, so a check ported from staticcheck leaves alone the files
	// staticcheck's leaves alone. filename is the name the file was parsed from, `FileSet.PositionFor(pos,
	// false).Filename`, which is how staticcheck keys the judgment. A caller asking by display name, as
	// report.FilterGenerated does, misses under a `//line` directive naming another file, exactly as staticcheck's own
	// does.
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
	// state is what the check gathers over the package, made by State on first use as the type the check asks for.
	//cohere:ignore consistency-no-any each check chooses its own state type, read back through State's type parameter
	state any
	// cursor is the inspector's cursor of the node the syntax dispatcher is calling Node on, and the zero cursor, which
	// is not valid, between those calls.
	cursor inspector.Cursor
	// fileLocal is set on the context of a file-local run, which hears only some of the package's files, so State,
	// which gathers across them, panics there (see Listener.FileLocal).
	fileLocal bool
	// file is the file the syntax walk is calling the check on, from FileEnter through FileExit, and nil outside one,
	// and fileStates is each file's FileState, by file.
	file *token.File
	//cohere:ignore consistency-no-any each check chooses its own state type, read back through FileState's type
	fileStates map[*token.File]any
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
		FileSet:     pass.Fset,
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

// NewFileLocalContext is NewContext's context for a file-local run of a listener marked FileLocal, which hears only
// some of the package's files, where State panics, so a listener marked wrongly crashes on its rule rather than
// judging some of the files as though they were all of them.
func NewFileLocalContext(
	ruleName string,
	pass *analysis.Pass,
	requires []*analysis.Analyzer,
	options *flag.FlagSet,
	isGenerated func(filename string) bool,
	sink func(ruleName string, diagnostic analysis.Diagnostic),
) *Context {
	context := NewContext(ruleName, pass, requires, options, isGenerated, sink)
	context.fileLocal = true
	return context
}

// Report hands diagnostic to the sink under the rule name the check was registered under. The check never names its
// rule itself.
func (context *Context) Report(diagnostic analysis.Diagnostic) {
	context.sink(context.ruleName, diagnostic)
}

// Reportf reports a finding at position whose message is format filled with arguments, the diagnostic
// analysis.Pass's Reportf builds, so a check ported from a pass keeps its calls as they were and reports what the
// pass reported.
func (context *Context) Reportf(position token.Pos, format string, arguments ...any) {
	context.Report(analysis.Diagnostic{Pos: position, Message: fmt.Sprintf(format, arguments...)})
}

// ReportRangef reports a finding over node, from its Pos to its End, whose message is format filled with arguments,
// the diagnostic analysis.Pass's ReportRangef builds, for Reportf's reason. An ast.Node is a range.
func (context *Context) ReportRangef(node analysis.Range, format string, arguments ...any) {
	context.Report(analysis.Diagnostic{Pos: node.Pos(), End: node.End(), Message: fmt.Sprintf(format, arguments...)})
}

// Reads is the reads of the context's package the checker handed the dispatcher's pass under Recorded, nil when the
// run records none, which every recording method takes as recording nothing.
func (context *Context) Reads() *build_reads.Reads {
	reads, _ := context.resultOf[Recorded].(*build_reads.Reads)
	return reads
}

// LookupIn is what declaring declares as name, nil when it declares nothing so, the question recorded in the
// context's reads when declaring is another package (see build_reads.Reads.ScopeLookup).
func (context *Context) LookupIn(declaring *types.Package, name string) types.Object {
	return context.Reads().ScopeLookup(context.Package, declaring, name)
}

// RuleName is the name the check was registered under, its row's name under `--timing`.
func (context *Context) RuleName() string {
	return context.ruleName
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

// Cursor is the inspector's cursor of the node a syntax listener's Node is called on, for a check that reads what only
// a cursor answers: the node's siblings, the field of its parent that holds it (ParentEdge), a node found beneath it
// (FindNode). It holds only during that call, since the context is the same for every call on the package. Called
// anywhere else, in FileEnter, FileExit, PackageExit, a walk of the intermediate representation, or after Node
// returned, Cursor panics, for ResultOf's reason, since a cursor it handed there would be another node's.
func (context *Context) Cursor() inspector.Cursor {
	if !context.cursor.Valid() {
		// Invariant: a check reads the cursor only during its Node call. A read anywhere else is a mistake in the
		// check, and the dispatcher's recover makes it this rule's crash on the package, so it never stops the process.
		panic(fmt.Sprintf("rule %s reads the cursor outside a Node call, where there is no node to hand it; read it "+
			"in Node, or keep what it answers there", context.ruleName))
	}
	return context.cursor
}

// EnterNode makes cursor the one Cursor hands the check, as the syntax dispatcher calls Node on cursor's node, and
// LeaveNode takes it back once Node returns. Only the syntax dispatcher calls them, never a check.
func (context *Context) EnterNode(cursor inspector.Cursor) {
	context.cursor = cursor
}

// EnterFile is called by the syntax dispatcher as it enters file, before FileEnter, and with nil once FileExit has
// returned, so FileState reads the slot of the file the call is on and panics outside one. Only the dispatcher calls
// it.
func (context *Context) EnterFile(file *token.File) {
	context.file = file
}

// LeaveNode makes Cursor panic again, as Node returns. See EnterNode.
func (context *Context) LeaveNode() {
	context.cursor = inspector.Cursor{}
}

// FileState is the check's own state over the file the syntax walk is calling it on, a new Kind made on the first call
// on that file and the same one on every call after on it, so a check gathers across one file's calls and judges in a
// later one, FileExit at the latest. It is keyed by the file, not cleared as a file is entered, so it holds however a
// package's files are split among runs (see Listener.FileLocal), and a check can never read another file's. A
// listener marked FileLocal may read it. It panics outside the syntax walk's calls on a file: in PackageExit, and on
// the walk over the intermediate representation, where no check needs one.
//
// Mutates: context gains the file's state on the first call on that file, handed back on every call after
func FileState[Kind any](context *Context) *Kind {
	if context.file == nil {
		// Invariant: the syntax dispatcher sets the file for every call from FileEnter through FileExit, so a call
		// outside one is a check reading it where no file is being walked, which the dispatcher's recover makes this
		// rule's crash on the package.
		panic(fmt.Sprintf("rule %s reads FileState outside the syntax walk's calls on a file", context.ruleName))
	}
	if context.fileStates == nil {
		context.fileStates = map[*token.File]any{}
	}
	if context.fileStates[context.file] == nil {
		context.fileStates[context.file] = new(Kind)
	}
	state, isKind := context.fileStates[context.file].(*Kind)
	if !isKind {
		// Invariant: a check asks for its file state as one type, so a second type is a mistake in the check, and the
		// dispatcher's recover makes it this rule's crash on the package.
		panic(fmt.Sprintf("rule %s reads its file state as %T, where it was made as %T", context.ruleName, state,
			context.fileStates[context.file]))
	}
	return state
}

// State is the check's own state over the package context covers, a new Kind made on the first call and the same one
// on every call after, so a check gathers across its calls and judges in a later one, FileExit or PackageExit, without
// a variable of its own. Each rule has its own context on each package, so the state is dropped with the package and
// two packages walked at once never share one. A context holds one state, of one type. A file-local run's context has
// none: State panics there (see Listener.FileLocal).
//
// Mutates: context gains its state on the first call, handed back on every call after, which is the point of it
func State[Kind any](context *Context) *Kind {
	if context.fileLocal {
		// Invariant: a listener marked FileLocal keeps nothing across files, which the mark promises, so a State here
		// is a wrong mark, and the dispatcher's recover makes it this rule's crash on the package.
		panic(fmt.Sprintf("rule %s reads State in a file-local run, where it hears only some of the package's "+
			"files; a listener that gathers across files is not FileLocal", context.ruleName))
	}
	if context.state == nil {
		context.state = new(Kind)
	}
	state, isKind := context.state.(*Kind)
	if !isKind {
		// Invariant: a check asks for its state as one type, so a second type is a mistake in the check, and the
		// dispatcher's recover makes it this rule's crash on the package, so it never stops the process.
		panic(fmt.Sprintf("rule %s reads its state as %T, where it was made as %T", context.ruleName, state,
			context.state))
	}
	return state
}
