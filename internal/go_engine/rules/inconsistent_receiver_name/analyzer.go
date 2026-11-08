// Package inconsistent_receiver_name is staticcheck's `ST1016`, the methods of one type giving their receiver more
// than one name, ported onto the engine's syntax dispatcher.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 stylecheck/st1016/st1016.go, whose message it reports
// word for word, at the type's first method in its method set's order, with no range, as the original does. Like the
// original, it leaves out a method whose receiver is in a generated file rather than withholding the finding, so the
// names counted are the ones written by hand. The original requires buildir only to list the package's members, of
// which it keeps the named types. The port lists those types from the top-level type declarations of each file
// instead, the very declarations the intermediate representation makes a member of each (honnef.co/go/tools
// go/ir/create.go, membersFromDecl, which leaves out a type named "_"), so the run builds no intermediate
// representation for this check. From each type on, the two read the same go/types answers through the same
// staticcheck helpers.
//
// The original's MergeIf, MergeIfAny, tells staticcheck's runner to keep a finding any one build configuration makes,
// and the engine runs one configuration, so there is nothing for the port to merge. The original is NonDefault, off
// unless configured, which is the registry's to honor, not the check's.
package inconsistent_receiver_name

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"honnef.co/go/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Name is the original's code, which the port takes, so the rule, its settings and every `//cohere:ignore` naming it
// read the same as before the port took the original's place.
const Name = "ST1016"

// Doc is the original's title.
const Doc = "Use consistent method receiver names"

// Listener is what the port hears of each package from the dispatcher: each file as the walk enters it, whose
// top-level type declarations are all the port reads of the syntax.
var Listener = rule.Listener{FileEnter: checkFile}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener.
var Analyzer = dispatch.RuleAnalyzer(rule.Namespace+Name, Name, Doc, &Listener)

// checkFile judges every named type file declares at its top level, each a member of the package in the original's
// listing.
func checkFile(context *rule.Context, file *ast.File) {
	for _, declaration := range file.Decls {
		general, isGeneral := declaration.(*ast.GenDecl)
		if !isGeneral || general.Tok != token.TYPE {
			continue
		}
		for _, specification := range general.Specs {
			typeSpecification, isTypeSpecification := specification.(*ast.TypeSpec)
			// The intermediate representation makes no member of a type named "_", which no method can be declared on.
			if !isTypeSpecification || typeSpecification.Name.Name == "_" {
				continue
			}
			typeName, isTypeName := context.TypesInfo.Defs[typeSpecification.Name].(*types.TypeName)
			// An alias declares no type of its own, and the original keeps no member that is one.
			if !isTypeName || typeName.IsAlias() {
				continue
			}
			checkType(context, typeName)
		}
	}
}

// checkType reports typeName when the methods declared on it by hand name their receivers more than one way, each
// name counted, a receiver left unnamed or named "_" counting for none.
func checkType(context *rule.Context, typeName *types.TypeName) {
	counts := map[string]int{}
	// firstMethod is where the finding is reported: the first method of the method set, in its order, that the
	// original counts, whether or not its receiver is named.
	var firstMethod *types.Func
	// The intuitive method set holds the methods of both the type and a pointer to it, so a method declared on either
	// receiver is counted, as the original counts it.
	for _, selection := range typeutil.IntuitiveMethodSet(typeName.Type(), nil) {
		method, isMethod := selection.Obj().(*types.Func)
		// A method set holds only methods, so this is never met, and the original's unchecked assertion holds.
		if !isMethod {
			continue
		}
		receiver := method.Signature().Recv()
		// The original asks staticcheck's generated-file judgment by the name the receiver's file was parsed from,
		// code.IsGenerated, as IsGenerated is keyed.
		if context.IsGenerated(context.Fset.PositionFor(receiver.Pos(), false).Filename) {
			continue
		}
		// A method promoted from an embedded field is declared on the field's type and counted there. The comparison
		// is by identity, as the original's is, so it skips what the original skips: a generic type's methods, whose
		// receivers are instances of the type, and a method whose receiver is written through an alias, whose
		// receiver's type is the alias.
		if typeutil.Dereference(receiver.Type()) != typeName.Type() {
			continue
		}
		if firstMethod == nil {
			firstMethod = method
		}
		if receiver.Name() != "" && receiver.Name() != "_" {
			counts[receiver.Name()]++
		}
	}
	if len(counts) <= 1 {
		return
	}
	seen := []string{}
	for name, count := range counts {
		seen = append(seen, fmt.Sprintf("%dx %q", count, name))
	}
	// Sorted as the original sorts them, as strings, so a count of 10 sorts before one of 2.
	slices.Sort(seen)
	staticcheck_report.Report(context, firstMethod, fmt.Sprintf(
		"methods on the same type should have the same receiver name (seen %s)", strings.Join(seen, ", ")))
}
