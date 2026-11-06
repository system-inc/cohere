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

	"github.com/system-inc/cohere/internal/go_engine/rule"
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
	// generic is the type that declares the method when that type has type parameters, nil when it has none. Its
	// method speaks of those parameters, so it is compared only through an instance.
	generic *types.Named
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
//
// A method of a generic foreign type is compared through the instance our method would have to match, its type
// arguments read off our signature, so `func (reader) Src() int` is checked against `Source[int]` as any other.
func fixedByAnotherPackage(pass *analysis.Pass, reachable reach, method *types.Func) bool {
	receiver := method.Signature().Recv()
	if receiver == nil {
		return false
	}
	isInterfaceMethod := types.IsInterface(receiver.Type())
	named := receiverNamed(receiver.Type())
	for _, candidate := range reachable.methodsByName[method.Name()] {
		foreign := candidate
		if candidate.generic != nil {
			arguments, determined := typeArgumentsOf(candidate, method.Signature())
			if !determined {
				// The foreign signature names too few of its type parameters bare to fix an instance, as
				// `Seq() int` names none of `Sequence[Item]`'s, so no instance can be compared. Like a generic
				// receiver, which cannot be asked what it implements, this is decided by the name and the shape of
				// the signature: as many parameters and results, variadic the same. It errs toward silence only when
				// that shape matches, and only for a method a foreign interface could fix.
				if (isInterfaceMethod || candidate.owner != nil) &&
					haveSameShape(candidate.method.Signature(), method.Signature()) {
					return true
				}
				continue
			}
			instance, instantiated := instantiate(candidate, arguments)
			if !instantiated {
				continue
			}
			foreign = instance
		}
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
	dereferenced := receiverType
	if pointer, isPointer := types.Unalias(receiverType).(*types.Pointer); isPointer {
		dereferenced = pointer.Elem()
	}
	named, isNamed := types.Unalias(dereferenced).(*types.Named)
	if !isNamed {
		return nil
	}
	return named
}

// typeArgumentsOf reads the type arguments of the instance of a generic foreign type whose method our signature would
// match: wherever the foreign signature has one of its type parameters bare, as a parameter or a result, our type at
// the same position is that parameter's argument. It is not determined when the shapes differ or a type parameter
// appears nowhere bare. Where two positions disagree the first is kept, and the instance then fails to match.
func typeArgumentsOf(candidate foreignMethod, ours *types.Signature) ([]types.Type, bool) {
	foreignSignature := candidate.method.Signature()
	if !haveSameShape(foreignSignature, ours) {
		return nil, false
	}
	// A method declared on a generic concrete type speaks of its receiver's own type parameters, one per parameter of
	// the type and in the same order; an interface's method speaks of the interface's.
	typeParameters := candidate.generic.TypeParams()
	if foreignSignature.RecvTypeParams().Len() > 0 {
		typeParameters = foreignSignature.RecvTypeParams()
	}
	arguments := make([]types.Type, typeParameters.Len())
	for _, tuples := range [][2]*types.Tuple{
		{foreignSignature.Params(), ours.Params()},
		{foreignSignature.Results(), ours.Results()},
	} {
		for index := range tuples[0].Len() {
			typeParameter, isTypeParameter := tuples[0].At(index).Type().(*types.TypeParam)
			if !isTypeParameter || typeParameter.Index() >= typeParameters.Len() ||
				typeParameters.At(typeParameter.Index()) != typeParameter ||
				arguments[typeParameter.Index()] != nil {
				continue
			}
			arguments[typeParameter.Index()] = tuples[1].At(index).Type()
		}
	}
	for _, argument := range arguments {
		if argument == nil {
			return nil, false
		}
	}
	return arguments, true
}

// instantiate is a generic foreign type's method as the instance with these type arguments has it, or false when
// the arguments do not satisfy the type's constraints, so no instance our method could match exists.
func instantiate(candidate foreignMethod, arguments []types.Type) (foreignMethod, bool) {
	instance, instantiateError := types.Instantiate(nil, candidate.generic, arguments, true)
	if instantiateError != nil {
		return foreignMethod{}, false
	}
	instanceNamed, isNamed := instance.(*types.Named)
	if !isNamed {
		return foreignMethod{}, false
	}
	if instanceInterface, isInterface := instanceNamed.Underlying().(*types.Interface); isInterface {
		for index := range instanceInterface.NumMethods() {
			if instanceInterface.Method(index).Name() == candidate.method.Name() {
				return foreignMethod{method: instanceInterface.Method(index), owner: instanceInterface}, true
			}
		}
		return foreignMethod{}, false
	}
	for index := range instanceNamed.NumMethods() {
		if instanceNamed.Method(index).Name() == candidate.method.Name() {
			return foreignMethod{method: instanceNamed.Method(index)}, true
		}
	}
	return foreignMethod{}, false
}

// haveSameShape is whether two signatures take as many parameters and give as many results, variadic the same.
func haveSameShape(left *types.Signature, right *types.Signature) bool {
	return left.Params().Len() == right.Params().Len() && left.Results().Len() == right.Results().Len() &&
		left.Variadic() == right.Variadic()
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
			// An alias is indexed where the type it names is declared.
			named, isNamed := typeName.Type().(*types.Named)
			if !isNamed {
				continue
			}
			// A generic type's methods are compared through an instance, so the type is kept to instantiate.
			var generic *types.Named
			if named.TypeParams().Len() > 0 {
				generic = named
			}
			if declaredInterface, isInterface := named.Underlying().(*types.Interface); isInterface {
				for index := range declaredInterface.NumMethods() {
					method := declaredInterface.Method(index)
					reachable.methodsByName[method.Name()] = append(reachable.methodsByName[method.Name()],
						foreignMethod{method: method, owner: declaredInterface, generic: generic})
				}
				continue
			}
			for index := range named.NumMethods() {
				method := named.Method(index)
				reachable.methodsByName[method.Name()] = append(reachable.methodsByName[method.Name()],
					foreignMethod{method: method, generic: generic})
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
	// already gives its own. A word with more than one expansion keeps the vocabulary's advice: it names every
	// meaning and asks for whichever one the name has, and an ending that named one meaning would be a guess.
	if finding.Form == abbreviation.FormWhole && finding.Entry.Whole.Style != "advice" &&
		len(finding.Entry.Expansions) == 1 && isTakenInGo(reachable, finding.Entry.Expansions[0]) {
		values = map[string]string{
			"name": finding.Values["name"],
			"advice": `Use a descriptive name ending in "` + abbreviation.Capitalize(finding.Entry.Expansions[0]) +
				`", since "` + finding.Entry.Expansions[0] + `" is already Go's name for something else.`,
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
