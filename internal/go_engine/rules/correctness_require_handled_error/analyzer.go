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

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/marker"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/correctness-require-handled-error"

// Analyzer runs the rule over one package.
var Analyzer = &analysis.Analyzer{
	Name: "correctness_require_handled_error",
	Doc: "Flags a call that returns an error and drops it, as a statement of its own, as the operand of defer or " +
		"go, or with the error assigned to _, unless a // Ignored: <reason> comment directly above the statement or " +
		"at the end of its line says why. Writes whose error is documented never to be non-nil, such as those to a " +
		"*strings.Builder, or to be kept for a later checked call, a *bufio.Writer's Flush, are exempt by type.",
	Requires: []*analysis.Analyzer{marker.Analyzer},
	Run:      run,
}

// Rule is this rule's entry in the Go engine's registry.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer}

var unhandledErrorMessage = policy.MessageOf(Name, "unhandledError")

var unhandledErrorWithoutReasonMessage = policy.MessageOf(Name, "unhandledErrorWithoutReason")

// ignoredMarker opens the comment that excuses a dropped error.
const ignoredMarker = "Ignored:"

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

func run(pass *analysis.Pass) (any, error) {
	errorInterface, isInterface := types.Universe.Lookup("error").Type().Underlying().(*types.Interface)
	if !isInterface {
		return nil, fmt.Errorf("reading the predeclared error as an interface")
	}
	indexes, isIndexes := pass.ResultOf[marker.Analyzer].(*marker.Indexes)
	if !isIndexes {
		return nil, fmt.Errorf("reading the comment indexes: marker gave %T", pass.ResultOf[marker.Analyzer])
	}
	checker := checker{pass: pass, errorInterface: errorInterface, hashInterface: findHashInterface(pass.Pkg)}
	for _, file := range pass.Files {
		// A comment excuses a statement from directly above it, on lines of its own, or from the end of its last line.
		comments := indexes.ForFile(file)
		ast.Inspect(file, func(node ast.Node) bool {
			switch statement := node.(type) {
			case *ast.ExprStmt:
				if call, isCall := ast.Unparen(statement.X).(*ast.CallExpr); isCall && checker.returnsError(call) {
					checker.report(call, comments.ExcuseFor(statement, ignoredMarker))
				}
			case *ast.DeferStmt:
				// A deferred or started call's results go nowhere, whatever they are.
				if checker.returnsError(statement.Call) {
					checker.report(statement.Call, comments.ExcuseFor(statement, ignoredMarker))
				}
			case *ast.GoStmt:
				if checker.returnsError(statement.Call) {
					checker.report(statement.Call, comments.ExcuseFor(statement, ignoredMarker))
				}
			case *ast.AssignStmt:
				for _, call := range checker.callsDroppingError(statement.Lhs, statement.Rhs) {
					checker.report(call, comments.ExcuseFor(statement, ignoredMarker))
				}
			case *ast.ValueSpec:
				// `var value, _ = strconv.Atoi(text)` drops the error as an assignment does.
				names := make([]ast.Expr, 0, len(statement.Names))
				for _, name := range statement.Names {
					names = append(names, name)
				}
				for _, call := range checker.callsDroppingError(names, statement.Values) {
					checker.report(call, comments.ExcuseFor(statement, ignoredMarker))
				}
			}
			return true
		})
	}
	return nil, nil
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
// hash is not in it.
func findHashInterface(analyzed *types.Package) *types.Interface {
	seen := map[*types.Package]bool{}
	pending := []*types.Package{analyzed}
	for len(pending) > 0 {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if seen[current] {
			continue
		}
		seen[current] = true
		if current.Path() == hashPackagePath {
			typeName, isTypeName := current.Scope().Lookup("Hash").(*types.TypeName)
			if !isTypeName {
				return nil
			}
			hashInterface, isInterface := typeName.Type().Underlying().(*types.Interface)
			if !isInterface {
				return nil
			}
			return hashInterface
		}
		pending = append(pending, current.Imports()...)
	}
	return nil
}

// isBlank is whether name is the blank identifier, which keeps nothing it is given.
func isBlank(name ast.Expr) bool {
	identifier, isIdentifier := name.(*ast.Ident)
	return isIdentifier && identifier.Name == "_"
}
