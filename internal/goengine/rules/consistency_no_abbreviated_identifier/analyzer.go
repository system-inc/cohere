// Package consistency_no_abbreviated_identifier is the Go engine's rule that a name we declare is spelled in full
// words, judged by the same vocabulary as the TypeScript and Swift rules.
//
// A name is written once and read everywhere, so the letters saved at the declaration are paid back at every call
// site by a reader who has to expand the abbreviation and hope they expanded it the way the author meant. The words
// and the forms they are judged in are policy/Abbreviations.json, read through internal/naming/abbreviation, so the
// house keeps one vocabulary across every engine.
//
// Only names this package declares are judged. A name that is someone else's, a use, an imported package's name, an
// embedded field named for its type, a method whose name another package's interface fixes, is not ours to rename,
// and is decided by types rather than by spelling.
package consistency_no_abbreviated_identifier

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/goengine/rule"
	"github.com/system-inc/cohere/internal/naming/abbreviation"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/consistency-no-abbreviated-identifier"

// Analyzer runs the rule over one package.
var Analyzer = &analysis.Analyzer{
	Name: "consistency_no_abbreviated_identifier",
	Doc: "Flags a name this package declares that abbreviates a word in policy/Abbreviations.json, the house " +
		"vocabulary every engine shares, and suggests the full word. Uses, imported package names, embedded fields, " +
		"and a method whose name and signature another package's interface or type fixes are not judged, since " +
		"those names are someone else's.",
	Run: run,
}

// Rule is this rule's entry in the Go engine's registry.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer}

// messages are the four shapes a finding takes, by the id the vocabulary names, from the message file this rule
// shares with its TypeScript and Swift twins, so each renders the same id with the same values.
var messages = map[string]policy.MessageHandle{
	abbreviation.AbbreviatedIdentifier:  policy.MessageOf(Name, abbreviation.AbbreviatedIdentifier),
	abbreviation.AbbreviatedSuffix:      policy.MessageOf(Name, abbreviation.AbbreviatedSuffix),
	abbreviation.MillisecondSuffix:      policy.MessageOf(Name, abbreviation.MillisecondSuffix),
	abbreviation.AbbreviatedWordSegment: policy.MessageOf(Name, abbreviation.AbbreviatedWordSegment),
}

// foreignMethod is a method another package declares, on an interface or on a named type, whose name and signature
// a method of ours may have to match.
type foreignMethod struct {
	method *types.Func
	// owner is the interface that declares the method, or nil when a concrete named type does.
	owner *types.Interface
}

// reach is what the rule knows of the packages this one can reach through its imports.
type reach struct {
	// methodsByName are every method of every interface and named type those packages declare, by name.
	methodsByName map[string][]foreignMethod
	// packageNames are those packages' names, which a whole-word suggestion must not take.
	packageNames map[string]bool
}

func run(pass *analysis.Pass) (any, error) {
	reachable := reachOf(pass.Pkg)
	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.TypeSwitchStmt:
				// The symbolic variable of `switch value := x.(type)` is ours, though go/types records no object
				// for it in Defs, only one per clause in Implicits.
				assignment, isAssignment := typed.Assign.(*ast.AssignStmt)
				if !isAssignment || len(assignment.Lhs) != 1 {
					return true
				}
				if identifier, isIdentifier := assignment.Lhs[0].(*ast.Ident); isIdentifier {
					judge(pass, reachable, identifier)
				}
			case *ast.Ident:
				object, isDefinition := pass.TypesInfo.Defs[typed]
				// Only a declaration: every other identifier is a use, the declaring code's spelling. A definition
				// with no object is the package clause's name or the type switch variable handled above.
				if isDefinition && object != nil && isOurs(pass, reachable, object) {
					judge(pass, reachable, typed)
				}
			}
			return true
		})
	}
	return nil, nil
}

// isOurs is whether a declared object's name is this package's to choose.
func isOurs(pass *analysis.Pass, reachable reach, object types.Object) bool {
	switch declared := object.(type) {
	case *types.PkgName:
		// An import's name, renamed or not, is the imported package's.
		return false
	case *types.Var:
		// An embedded field is named for its type, which is judged where it is declared, if it is ours at all.
		return !declared.Embedded()
	case *types.Func:
		return !fixedByAnotherPackage(pass, reachable, declared)
	}
	return true
}

// fixedByAnotherPackage is whether a method's name is set by another package: a concrete method that implements a
// method of an interface another package declares, or an interface method of ours that a method another package
// declares must be able to satisfy. A plain function is never fixed.
//
// The concrete check is by types: the method's name and signature match the foreign interface's method, and the
// receiver type, or a pointer to it, implements that whole interface. A generic receiver cannot be asked whether it
// implements anything, so for one the name and signature matching a foreign interface method decides. An interface
// method of ours matches by name and signature against any foreign method, since a foreign type that has it is what
// the interface is written to accept, as `interface{ Len() int }` accepts *bytes.Buffer.
func fixedByAnotherPackage(pass *analysis.Pass, reachable reach, method *types.Func) bool {
	receiver := method.Signature().Recv()
	if receiver == nil {
		return false
	}
	isInterfaceMethod := types.IsInterface(receiver.Type())
	named := receiverNamed(receiver.Type())
	for _, foreign := range reachable.methodsByName[method.Name()] {
		if !types.Identical(foreign.method.Signature(), method.Signature()) {
			continue
		}
		if isInterfaceMethod {
			return true
		}
		if foreign.owner == nil {
			continue
		}
		if named == nil || named.TypeParams().Len() > 0 {
			return true
		}
		if types.Implements(named, foreign.owner) || types.Implements(types.NewPointer(named), foreign.owner) {
			return true
		}
	}
	return false
}

// receiverNamed is the named type a method's receiver is, through its pointer, or nil when there is none.
func receiverNamed(receiverType types.Type) *types.Named {
	if pointer, isPointer := types.Unalias(receiverType).(*types.Pointer); isPointer {
		receiverType = pointer.Elem()
	}
	named, isNamed := types.Unalias(receiverType).(*types.Named)
	if !isNamed {
		return nil
	}
	return named
}

// reachOf walks every package this one reaches through its imports, once per package, and indexes what the rule
// asks of them.
func reachOf(current *types.Package) reach {
	reachable := reach{methodsByName: map[string][]foreignMethod{}, packageNames: map[string]bool{}}
	visited := map[*types.Package]bool{current: true}
	pending := append([]*types.Package{}, current.Imports()...)
	for len(pending) > 0 {
		imported := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if visited[imported] {
			continue
		}
		visited[imported] = true
		pending = append(pending, imported.Imports()...)
		reachable.packageNames[imported.Name()] = true
		for _, name := range imported.Scope().Names() {
			typeName, isTypeName := imported.Scope().Lookup(name).(*types.TypeName)
			if !isTypeName || typeName.IsAlias() {
				continue
			}
			named, isNamed := typeName.Type().(*types.Named)
			// A generic type's methods cannot be compared without instantiating it, and an alias is indexed where
			// the type it names is declared.
			if !isNamed || named.TypeParams().Len() > 0 {
				continue
			}
			if declaredInterface, isInterface := named.Underlying().(*types.Interface); isInterface {
				for index := range declaredInterface.NumMethods() {
					method := declaredInterface.Method(index)
					reachable.methodsByName[method.Name()] = append(reachable.methodsByName[method.Name()],
						foreignMethod{method: method, owner: declaredInterface})
				}
				continue
			}
			for index := range named.NumMethods() {
				method := named.Method(index)
				reachable.methodsByName[method.Name()] = append(reachable.methodsByName[method.Name()],
					foreignMethod{method: method})
			}
		}
	}
	return reachable
}

// judge reports a declared name the vocabulary finds abbreviated.
func judge(pass *analysis.Pass, reachable reach, identifier *ast.Ident) {
	if identifier.Name == "_" {
		return
	}
	finding, found := abbreviation.Policy.Find(identifier.Name)
	if !found {
		return
	}
	values := finding.Values
	// The house expands to a descriptive name, never to one Go already gives something else: `ctx` becomes
	// `requestContext`, not the package name `context`, and `err` becomes `readError`, not the predeclared `error`.
	// Only a whole-word finding suggests the bare expansion, so only its advice changes, and an advice-style entry
	// already gives its own.
	if finding.Form == abbreviation.FormWhole && finding.Entry.Whole.Style != "advice" &&
		isTakenInGo(reachable, finding.Entry.Expansion) {
		values = map[string]string{
			"name": finding.Values["name"],
			"advice": `Use a descriptive name ending in "` + abbreviation.Capitalize(finding.Entry.Expansion) +
				`", since "` + finding.Entry.Expansion + `" is already Go's name for something else.`,
		}
	}
	pass.Report(analysis.Diagnostic{
		Pos:      identifier.Pos(),
		End:      identifier.End(),
		Category: finding.MessageId,
		Message:  messages[finding.MessageId].Render(values),
	})
}

// isTakenInGo is whether a name is a keyword, a predeclared identifier, or the name of a package this one reaches, so
// declaring it would be illegal or would shadow what a reader expects it to mean.
func isTakenInGo(reachable reach, name string) bool {
	return token.IsKeyword(name) || types.Universe.Lookup(name) != nil || reachable.packageNames[name]
}
