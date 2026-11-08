// Package receiver_name is staticcheck's `ST1006`, a method's receiver named "self" or "this", which says nothing of
// what the receiver is, or "_", where an unused receiver leaves its name out, ported onto the engine's syntax
// dispatcher.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 stylecheck/st1006/st1006.go, whose messages it
// reports word for word, at the receiver, with no range, as the original does, and withholds in a generated file as
// the original's FilterGenerated does. The original requires buildir only to list the package's members, of which it
// keeps the named types. The port lists those types from the top-level type declarations of each file instead, the
// very declarations the intermediate representation makes a member of each (honnef.co/go/tools go/ir/create.go,
// membersFromDecl, which leaves out a type named "_"), so the run builds no intermediate representation for this
// check. From each type on, the two read the same go/types answers through the same staticcheck helpers.
//
// The original's MergeIf, MergeIfAny, tells staticcheck's runner to keep a finding any one build configuration makes,
// and the engine runs one configuration, so there is nothing for the port to merge.
package receiver_name

import (
	"go/ast"
	"go/token"
	"go/types"

	"honnef.co/go/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Name is the original's code, which the port takes, so the rule, its settings and every `//cohere:ignore` naming it
// read the same as before the port took the original's place.
const Name = "ST1006"

// Doc is the original's title.
const Doc = "Poorly chosen receiver name"

// genericNameMessage is the original's message for a receiver named "self" or "this", word for word.
const genericNameMessage = `receiver name should be a reflection of its identity; ` +
	`don't use generic names such as "this" or "self"`

// underscoreMessage is the original's message for a receiver named "_", word for word.
const underscoreMessage = "receiver name should not be an underscore, omit the name if it is unused"

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

// checkType reports the receiver of each method declared on typeName that is named "self", "this" or "_".
func checkType(context *rule.Context, typeName *types.TypeName) {
	// The intuitive method set holds the methods of both the type and a pointer to it, so a method declared on either
	// receiver is judged, as the original judges it.
	for _, selection := range typeutil.IntuitiveMethodSet(typeName.Type(), nil) {
		method, isMethod := selection.Obj().(*types.Func)
		// A method set holds only methods, so this is never met, and the original's unchecked assertion holds.
		if !isMethod {
			continue
		}
		receiver := method.Signature().Recv()
		// A method promoted from an embedded field is declared on the field's type and judged there. The comparison is
		// by identity, as the original's is, so it skips what the original skips: a generic type's methods, whose
		// receivers are instances of the type, and a method whose receiver is written through an alias, whose
		// receiver's type is the alias.
		if typeutil.Dereference(receiver.Type()) != typeName.Type() {
			continue
		}
		switch receiver.Name() {
		case "self", "this":
			staticcheck_report.Report(context, receiver, genericNameMessage, staticcheck_report.FilterGenerated())
		case "_":
			staticcheck_report.Report(context, receiver, underscoreMessage, staticcheck_report.FilterGenerated())
		}
	}
}
