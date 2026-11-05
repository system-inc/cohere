// Package correctness_no_write_only_collection is the Go engine's rule that a map or slice something fills is also
// read.
//
// A collection every use of which puts something in or takes something out, and nothing reads, throws away the work
// that fills it: either the reader that was meant to exist is missing, or the collection is dead. Go's compiler counts
// collection[key] = value as a use, so it never says "declared and not used" here, and staticcheck's SA4010 judges
// only append. This rule is the rest: maps assigned into, and slices written by index.
package correctness_no_write_only_collection

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/goengine/rule"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/correctness-no-write-only-collection"

// Analyzer runs the rule over one package.
var Analyzer = &analysis.Analyzer{
	Name: "correctness_no_write_only_collection",
	Doc: "Flags a map or slice declared in a function, fresh from a composite literal, make or its zero value, whose " +
		"every use writes it (collection[key] = value, delete, clear, collection = append(collection, ...)) and at " +
		"least one write is not an append. A slice only ever appended to is staticcheck's SA4010, which holds the " +
		"append half, so this rule leaves it alone. Generated files are not judged.",
	Run: run,
}

// Rule is this rule's entry in the Go engine's registry.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer}

var writeOnlyCollectionMessage = policy.MessageOf(Name, "writeOnlyCollection")

// use is what one reference does to a collection.
type use int

const (
	// read is any reference that is not one of the writes below, so the collection stays silent.
	read use = iota
	// written is collection[key] = value, delete(collection, key) or clear(collection).
	written
	// appended is either side of collection = append(collection, ...), which SA4010 judges on its own.
	appended
)

// collection is one fresh local map or slice and what its references do to it.
type collection struct {
	// name is the identifier that declares it, where a finding is reported.
	name *ast.Ident
	// writes counts the references that write it other than by append.
	writes int
	// isRead is whether any reference reads it.
	isRead bool
}

func run(pass *analysis.Pass) (any, error) {
	for _, file := range pass.Files {
		// A generated file is its generator's to change, and the generator's next run puts any edit by hand back.
		// This is the one place generated files are skipped.
		if ast.IsGenerated(file) {
			continue
		}
		collections := freshCollections(pass, file)
		if len(collections) == 0 {
			continue
		}
		byVariable := map[*types.Var]*collection{}
		for _, found := range collections {
			if variable, isVar := pass.TypesInfo.Defs[found.name].(*types.Var); isVar {
				byVariable[variable] = found
			}
		}
		judgeReferences(pass, file, byVariable)
		for _, found := range collections {
			// A write by append alone is SA4010's to report, and a collection never referenced is the compiler's.
			if found.isRead || found.writes == 0 {
				continue
			}
			pass.Report(analysis.Diagnostic{
				Pos:      found.name.Pos(),
				End:      found.name.End(),
				Category: writeOnlyCollectionMessage.Id,
				Message:  writeOnlyCollectionMessage.Render(map[string]string{}),
			})
		}
	}
	return nil, nil
}

// freshCollections is every map or slice variable declared in a function body in file, in source order, whose value
// starts out its own. A DeclStmt and a := only occur inside a function, so a package-level variable, which any file
// of the package can read, is never one.
func freshCollections(pass *analysis.Pass, file *ast.File) []*collection {
	var collections []*collection
	consider := func(name *ast.Ident, initializer ast.Expr) {
		// Defs holds only a new variable: a := that reuses a name records it in Uses, and the blank name has none.
		variable, isVar := pass.TypesInfo.Defs[name].(*types.Var)
		if !isVar || !isCollection(variable.Type()) || !isFresh(pass, initializer) {
			return
		}
		collections = append(collections, &collection{name: name})
	}
	ast.Inspect(file, func(node ast.Node) bool {
		switch declaration := node.(type) {
		case *ast.AssignStmt:
			// A := from one call of several results gives no initializer to read per name, so it is not judged.
			if declaration.Tok != token.DEFINE || len(declaration.Lhs) != len(declaration.Rhs) {
				return true
			}
			for index, left := range declaration.Lhs {
				if name, isIdentifier := left.(*ast.Ident); isIdentifier {
					consider(name, declaration.Rhs[index])
				}
			}
		case *ast.DeclStmt:
			general, isGeneral := declaration.Decl.(*ast.GenDecl)
			if !isGeneral || general.Tok != token.VAR {
				return true
			}
			for _, specification := range general.Specs {
				value, isValue := specification.(*ast.ValueSpec)
				if !isValue {
					continue
				}
				for index, name := range value.Names {
					switch len(value.Values) {
					case 0:
						consider(name, nil)
					case len(value.Names):
						consider(name, value.Values[index])
					}
				}
			}
		}
		return true
	})
	return collections
}

// isCollection is whether a variable of this type is a map or a slice, a named one included. An array is a value
// whose unread element writes x/tools' unusedwrite reports, and a type parameter's underlying type is its
// constraint, so neither is one here.
func isCollection(candidate types.Type) bool {
	switch candidate.Underlying().(type) {
	case *types.Map, *types.Slice:
		return true
	}
	return false
}

// isFresh is whether an initializer makes a collection no other name shares: none (the zero value), a composite
// literal, or the builtin make. Anything else may alias one held elsewhere (alias := source, window := buffer[:2],
// cache := newCache()), and a write through the alias is a write someone else can read.
func isFresh(pass *analysis.Pass, initializer ast.Expr) bool {
	if initializer == nil {
		return true
	}
	switch value := ast.Unparen(initializer).(type) {
	case *ast.CompositeLit:
		return true
	case *ast.CallExpr:
		return builtinName(pass, value.Fun) == "make"
	}
	return false
}

// judgeReferences records what every reference in file to one of the collections does to it. A local is referenced
// only in the file that declares it, so one file is the whole of its uses.
func judgeReferences(pass *analysis.Pass, file *ast.File, byVariable map[*types.Var]*collection) {
	// The path from the file down to the node being visited, so a reference is judged by the nodes around it.
	var stack []ast.Node
	ast.Inspect(file, func(node ast.Node) bool {
		if node == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		stack = append(stack, node)
		identifier, isIdentifier := node.(*ast.Ident)
		if !isIdentifier || len(stack) < 3 {
			return true
		}
		// Uses, by object, so a shadowing variable of the same name in an inner scope is never this one.
		variable, isVar := pass.TypesInfo.Uses[identifier].(*types.Var)
		if !isVar {
			return true
		}
		found, isCandidate := byVariable[variable]
		if !isCandidate {
			return true
		}
		switch useOf(pass, identifier, variable, stack[len(stack)-2], stack[len(stack)-3]) {
		case written:
			found.writes++
		case appended:
			// Neither a read nor a write this rule counts, since a slice only appended to is SA4010's.
		case read:
			found.isRead = true
		}
		return true
	})
}

// useOf judges one reference by its parent and grandparent. Each write is matched exactly, with no parentheses looked
// through, and anything else is a read: a missed finding at worst, never a false one.
func useOf(pass *analysis.Pass, identifier *ast.Ident, variable *types.Var, parent ast.Node, grandparent ast.Node) use {
	switch around := parent.(type) {
	case *ast.IndexExpr:
		// collection[key] = value, alone or among several targets of one plain =. A compound op= and ++ read the
		// element first, so they are reads, as the TypeScript and Swift twins read them.
		assignment, isAssignment := grandparent.(*ast.AssignStmt)
		if around.X != identifier || !isAssignment || assignment.Tok != token.ASSIGN {
			return read
		}
		for _, left := range assignment.Lhs {
			if left == around {
				return written
			}
		}
	case *ast.CallExpr:
		if len(around.Args) == 0 || around.Args[0] != identifier {
			return read
		}
		switch builtinName(pass, around.Fun) {
		case "delete", "clear":
			// Neither has a result, so wherever it stands, a statement, defer or go, it only writes.
			return written
		case "append":
			assignment, isAssignment := grandparent.(*ast.AssignStmt)
			if !isAssignment {
				return read
			}
			if index := rightIndex(assignment, around); index >= 0 && refersTo(pass, assignment.Lhs[index], variable) {
				return appended
			}
		}
	case *ast.AssignStmt:
		// The target of collection = append(collection, ...). Any other assignment to the name gives it a value
		// that may be shared, so it is a read.
		if around.Tok != token.ASSIGN || len(around.Lhs) != len(around.Rhs) {
			return read
		}
		for index, left := range around.Lhs {
			if left != identifier {
				continue
			}
			call, isCall := around.Rhs[index].(*ast.CallExpr)
			if isCall && builtinName(pass, call.Fun) == "append" && len(call.Args) > 0 &&
				refersTo(pass, call.Args[0], variable) {
				return appended
			}
		}
	}
	return read
}

// rightIndex is where call stands among a plain ='s values, matched to a target by position, or -1.
func rightIndex(assignment *ast.AssignStmt, call *ast.CallExpr) int {
	if assignment.Tok != token.ASSIGN || len(assignment.Lhs) != len(assignment.Rhs) {
		return -1
	}
	for index, right := range assignment.Rhs {
		if right == call {
			return index
		}
	}
	return -1
}

// refersTo is whether expression is the bare name of variable.
func refersTo(pass *analysis.Pass, expression ast.Expr, variable *types.Var) bool {
	identifier, isIdentifier := expression.(*ast.Ident)
	return isIdentifier && pass.TypesInfo.Uses[identifier] == variable
}

// builtinName is the name of the builtin function expression names, or "" when it names anything else, so a local
// function called delete or append, which may read what it is given, is never taken for the builtin.
func builtinName(pass *analysis.Pass, expression ast.Expr) string {
	identifier, isIdentifier := expression.(*ast.Ident)
	if !isIdentifier {
		return ""
	}
	builtin, isBuiltin := pass.TypesInfo.Uses[identifier].(*types.Builtin)
	if !isBuiltin {
		return ""
	}
	return builtin.Name()
}
