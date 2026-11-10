// Package correctness_require_handled_error is the Go engine's rule that an error a call returns is handled, or
// dropped with a reason.
//
// An error dropped without a word is a failure nobody will see: the file that never closed, the write that never
// landed, the number that never parsed, and the code runs on as if each succeeded. Sometimes dropping it is right,
// a best-effort cleanup whose failure changes nothing, and then the reason is what the next reader needs, so a
// dropped error says why in a comment where a reader looks.
package correctness_require_handled_error

import (
	"fmt"
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/marker"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/correctness-require-handled-error"

// listener reads the package's comment indexes as the syntax dispatcher's walk enters each file, and hears every
// statement or specification that can drop a call's error.
var listener = rule.Listener{
	Requires:  []*analysis.Analyzer{marker.Analyzer},
	FileEnter: enterFile,
	Nodes: []ast.Node{
		(*ast.ExprStmt)(nil),
		(*ast.DeferStmt)(nil),
		(*ast.GoStmt)(nil),
		(*ast.AssignStmt)(nil),
		(*ast.ValueSpec)(nil),
	},
	Node: judgeStatement,
}

// Analyzer runs the rule over one package, reporting what the syntax dispatcher's walk found for it.
var Analyzer = dispatch.RuleAnalyzer(Name, "correctness_require_handled_error", "Flags a call that returns an "+
	"error and drops it, as a statement of its own, as the operand of defer or go, or with the error assigned to _, "+
	"unless a // Ignored: <reason> comment directly above the statement or at the end of its line says why. Writes "+
	"whose error is documented never to be non-nil, such as those to a *strings.Builder, or to be kept for a later "+
	"checked call, a *bufio.Writer's Flush, are exempt by type.", &listener)

// Rule is this rule's entry in the Go engine's registry.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer, Listener: &listener}

var unhandledErrorMessage = policy.MessageOf(Name, "unhandledError")

var unhandledErrorWithoutReasonMessage = policy.MessageOf(Name, "unhandledErrorWithoutReason")

// The callees whose error need not be checked at the call: either it is documented never to be non-nil, so handling
// it would be ceremony around a value that cannot be anything but nil, or it is kept and handed back at a later call
// that is itself checked. Each is matched by its package, type and method, never by how the call is spelled.
var (
	// exemptWriteMethods are the write methods of the exemptWriters, as their documentation gives them:
	//
	//	strings.Builder.Write        "Write always returns len(p), nil."
	//	strings.Builder.WriteByte    "The returned error is always nil."
	//	strings.Builder.WriteRune    "It returns the number of bytes written and a nil error."
	//	strings.Builder.WriteString  "It returns the length of s and a nil error."
	//	bytes.Buffer.Write           "The return value n is the length of p; err is always nil."
	//	bytes.Buffer.WriteByte       "The returned error is always nil, but is included to match bufio.Writer's
	//	                             WriteByte."
	//	bytes.Buffer.WriteRune       "The nil error is included to match bufio.Writer's WriteRune."
	//	bytes.Buffer.WriteString     "The return value n is the length of s; err is always nil."
	//	bufio.Writer, every write    "If an error occurs writing to a Writer, no more data will be accepted and all
	//	                             subsequent writes, and Flush, will return the error."
	//
	// A bufio.Writer's error is sticky, so a dropped write loses nothing as long as its Flush is checked, which is
	// why Flush is absent and stays judged: it is where the error comes back. ReadFrom and WriteTo are absent on
	// purpose too: they read or write another io.Reader or io.Writer, and its error is theirs.
	exemptWriteMethods = map[string]bool{"Write": true, "WriteByte": true, "WriteRune": true, "WriteString": true}
	// exemptWriters are the types the write methods above belong to, by package path and type name, each through a
	// pointer.
	exemptWriters = []struct{ packagePath, typeName string }{
		{"strings", "Builder"},
		{"bytes", "Buffer"},
		{"bufio", "Writer"},
	}
	// exemptPrintFunctions are fmt's printers to a writer: "It returns the number of bytes written and any write
	// error encountered." Formatting itself never fails, so writing to one of the exemptWriters, they return what its
	// write would. Println and Printf write to standard output, which can fail, so they are not here.
	exemptPrintFunctions = map[string]bool{"Fprint": true, "Fprintf": true, "Fprintln": true}
	// exemptCallees are single callees documented never to fail, by package path, receiver type name (empty for a
	// package function) and name, as their documentation gives them, and the Go version that promise starts at (empty
	// for every version):
	//
	//	maphash.Hash.WriteString  "It always writes all of s and never fails; the count and error result are for
	//	                          implementing io.StringWriter."
	//	rand.Read                 "It never returns an error, and always fills b entirely." (crypto/rand, from Go
	//	                          1.24, whose release notes say "The Read function is now guaranteed not to fail.")
	//
	// The promise is the toolchain's, so it is read from the package's version, the go line of its go.mod, which
	// since Go 1.21 is the oldest toolchain that may build it. A file's //go:build version lowers only the language
	// that file is checked against, never the toolchain, so it does not count here.
	exemptCallees = []struct{ packagePath, typeName, name, sinceGoVersion string }{
		{"hash/maphash", "Hash", "WriteString", ""},
		{"crypto/rand", "", "Read", "go1.24"},
	}
)

// hashPackagePath holds hash.Hash, whose Write the interface itself documents: "Write (via the embedded io.Writer
// interface) adds more data to the running hash. It never returns an error." Every implementation is held to that,
// so the Write of any type implementing hash.Hash is exempt.
const hashPackagePath = "hash"

// errorInterface is the predeclared error's, which a result implements to count as an error.
var errorInterface = func() *types.Interface {
	predeclared, isInterface := types.Universe.Lookup("error").Type().Underlying().(*types.Interface)
	if !isInterface {
		// Invariant: the predeclared error is an interface in every Go this engine is built with.
		panic("the predeclared error is not an interface")
	}
	return predeclared
}()

// packageState is what the rule reads once for its package and once for each file, held in its context's state.
type packageState struct {
	// indexes is the package's comment indexes, read as the walk enters its first file.
	indexes *marker.Indexes
	// checker is what every judgment in the package reads, made with indexes.
	checker checker
	// comments is the comment index of the file being walked. A comment excuses a statement from directly above it,
	// on lines of its own, or from the end of its last line.
	comments *marker.Index
}

// enterFile reads the package's comment indexes and its checker the first time, and file's comment index.
func enterFile(context *rule.Context, file *ast.File) {
	state := rule.State[packageState](context)
	if state.indexes == nil {
		indexes, isIndexes := context.ResultOf(marker.Analyzer).(*marker.Indexes)
		if !isIndexes {
			// Invariant: marker's result is always its Indexes, so another is a mistake in the engine, and the
			// dispatcher's recover makes it this rule's crash on the package, so it never stops the process.
			panic(fmt.Sprintf("reading the comment indexes: marker gave %T", context.ResultOf(marker.Analyzer)))
		}
		state.indexes = indexes
		state.checker = checker{
			context:       context,
			hashInterface: findHashInterface(context.Package, context.Reads()),
		}
	}
	state.comments = state.indexes.ForFile(file)
}

// judgeStatement reports every call node drops the error of, unless the comments speaking for node give a reason.
func judgeStatement(context *rule.Context, node ast.Node, _ []ast.Node) {
	state := rule.State[packageState](context)
	switch statement := node.(type) {
	case *ast.ExprStmt:
		if call, isCall := ast.Unparen(statement.X).(*ast.CallExpr); isCall && state.checker.returnsError(call) {
			state.checker.report(call, state.comments.ExcuseFor(statement, marker.Ignored))
		}
	case *ast.DeferStmt:
		// A deferred or started call's results go nowhere, whatever they are.
		if state.checker.returnsError(statement.Call) {
			state.checker.report(statement.Call, state.comments.ExcuseFor(statement, marker.Ignored))
		}
	case *ast.GoStmt:
		if state.checker.returnsError(statement.Call) {
			state.checker.report(statement.Call, state.comments.ExcuseFor(statement, marker.Ignored))
		}
	case *ast.AssignStmt:
		for _, call := range state.checker.callsDroppingError(statement.Lhs, statement.Rhs) {
			state.checker.report(call, state.comments.ExcuseFor(statement, marker.Ignored))
		}
	case *ast.ValueSpec:
		// `var value, _ = strconv.Atoi(text)` drops the error as an assignment does.
		names := make([]ast.Expr, 0, len(statement.Names))
		for _, name := range statement.Names {
			names = append(names, name)
		}
		for _, call := range state.checker.callsDroppingError(names, statement.Values) {
			state.checker.report(call, state.comments.ExcuseFor(statement, marker.Ignored))
		}
	}
}

// isExemptWriterType is whether candidate is a pointer to one of exemptWriters.
func isExemptWriterType(candidate types.Type) bool {
	pointer, isPointer := types.Unalias(candidate).(*types.Pointer)
	if !isPointer {
		return false
	}
	named, isNamed := types.Unalias(pointer.Elem()).(*types.Named)
	if !isNamed || named.Obj().Pkg() == nil {
		return false
	}
	for _, writer := range exemptWriters {
		if named.Obj().Pkg().Path() == writer.packagePath && named.Obj().Name() == writer.typeName {
			return true
		}
	}
	return false
}

// findHashInterface is hash.Hash's interface from the import graph under analyzed, analyzed included, or nil when
// hash is not in it. What it found, or that it found nothing, is recorded in reads, since it is an answer of the
// import closure no type read of analyzed holds, and the search is the one the restore asks again in another build
// (build_reads.ClosureInterface, #b2t9xmf).
func findHashInterface(analyzed *types.Package, reads *build_reads.Reads) *types.Interface {
	hashInterface, declared := build_reads.ClosureInterface([]*types.Package{analyzed}, hashPackagePath, "Hash")
	reads.AddLookup(build_reads.Lookup{Query: build_reads.QueryClosure, Path: hashPackagePath, Name: "Hash",
		Found: declared != nil, Answer: declared})
	return hashInterface
}

// isBlank is whether name is the blank identifier, which keeps nothing it is given.
func isBlank(name ast.Expr) bool {
	identifier, isIdentifier := name.(*ast.Ident)
	return isIdentifier && identifier.Name == "_"
}
