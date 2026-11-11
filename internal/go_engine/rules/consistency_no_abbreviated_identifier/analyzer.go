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
//
// A whole word whose expansion Go already uses, as `err`'s `error` and `ctx`'s `context` are, asks for a descriptive
// name. Where the value came from gives one (internal/naming/go_origin_names, which the errornames and contextnames
// codemods name by too). A local error written once, by a single call, takes the call's name, so `data, err :=
// os.ReadFile(path)` is told to use readError, and offered it. A context.Context parameter is the caller's,
// callerContext, and a local context written once takes the name of the call that made it, as context.WithCancel
// gives cancelContext.
package consistency_no_abbreviated_identifier

import (
	"go/ast"
	"go/token"
	"go/types"
	"iter"
	"sync"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/naming/abbreviation"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/consistency-no-abbreviated-identifier"

// listener reads what the package reaches as the syntax dispatcher's walk enters its first file, and hears every
// identifier and type switch of the walk.
var listener = rule.Listener{
	FileEnter: enterFile,
	Nodes:     []ast.Node{(*ast.TypeSwitchStmt)(nil), (*ast.Ident)(nil)},
	Node:      judgeNode,
}

// analyzerName and doc are the rule's analyzer's name and documentation, the same on every analyzer built for it.
const (
	analyzerName = "consistency_no_abbreviated_identifier"
	doc          = "Flags a name this package declares that abbreviates a word in policy/Abbreviations.json, the " +
		"house vocabulary every engine shares, and suggests the full word. Uses, imported package names, embedded " +
		"fields, and a method whose name and signature another package's interface or type fixes are not judged, " +
		"since those names are someone else's. Where the vocabulary names exactly one full spelling for the whole " +
		"name, the finding offers it as a rename, which the engine applies at every reference. A local error " +
		"written once by a single call is named for that call instead, and a context.Context for its caller when " +
		"it is a parameter or for the call that made it, the name the finding gives and offers. The " +
		"renameExports option lets a rename reach an exported name of an importable package, and never changes a " +
		"finding."
)

// NewAnalyzer builds the rule's analyzer on a syntax dispatcher of its own, with a flag set of its own, so a test can
// run it with renameExports set without setting the flag another test's analyzer reads. The shared dispatcher holds
// each rule's name once, so a second analyzer for the rule is registered on a dispatcher of its own.
func NewAnalyzer() *analysis.Analyzer {
	return newAnalyzer(dispatch.NewSyntax())
}

// newAnalyzer registers the rule's listener on dispatcher and gives the analyzer it returns its renameExports flag,
// which the engine sets from the rule's settings and the context's Option reads off this analyzer.
func newAnalyzer(dispatcher *dispatch.Syntax) *analysis.Analyzer {
	analyzer := dispatcher.RuleAnalyzer(Name, analyzerName, doc, &listener)
	rule.AddRenameExportsOption(analyzer)
	return analyzer
}

// Analyzer runs the rule over one package, reporting what the shared syntax dispatcher's walk found for it.
var Analyzer = newAnalyzer(dispatch.SharedSyntax())

// Rule is this rule's entry in the Go engine's registry.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer, Listener: &listener}

// messages are the four shapes a finding takes, by the id the vocabulary names, from the message file this rule
// shares with its TypeScript and Swift twins, so each renders the same id with the same values.
var messages = map[string]policy.MessageHandle{
	abbreviation.AbbreviatedIdentifier:  policy.MessageOf(Name, abbreviation.AbbreviatedIdentifier),
	abbreviation.AbbreviatedSuffix:      policy.MessageOf(Name, abbreviation.AbbreviatedSuffix),
	abbreviation.MillisecondSuffix:      policy.MessageOf(Name, abbreviation.MillisecondSuffix),
	abbreviation.AbbreviatedWordSegment: policy.MessageOf(Name, abbreviation.AbbreviatedWordSegment),
}

// fullWordRenameMessage is the rename a finding offers when the vocabulary names one spelling for the whole name.
var fullWordRenameMessage = policy.MessageOf(Name, "fullWordRename")

// namedForItsCall is the id of a Go finding on a local error named for the call that produced it: the whole word's
// advice toward a descriptive name, with the one descriptive name the call gives in its place.
const namedForItsCall = "namedForItsCall"

// contextNamedForItsOrigin is the id of a Go finding on a context named for where it came from, its caller or the
// call that made it, in place of the advice toward a descriptive name.
const contextNamedForItsOrigin = "contextNamedForItsOrigin"

// originMessages are the messages of a finding named for where its value came from, by id, each naming the exact
// spelling its rename writes.
var originMessages = map[string]policy.MessageHandle{
	namedForItsCall:          policy.MessageOf(Name, namedForItsCall),
	contextNamedForItsOrigin: policy.MessageOf(Name, contextNamedForItsOrigin),
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
	// declared are the methods of every interface and named type those packages declare, by name, one map to each
	// package that declares any. Each map is its package's alone, so it is shared with every other package of the
	// load that reaches the same one (see loadDeclarations).
	declared []map[string][]foreignMethod
	// packageNames are those packages' names, which a whole-word suggestion must not take.
	packageNames map[string]bool
	// reads is the reads of the package, which each question isTakenInGo asks of packageNames is recorded in, nil
	// when the run records none (#b2t9xmf).
	reads *build_reads.Reads
}

// methodsNamed are the methods named name that the reachable packages declare, in no order the rule depends on: a
// method is fixed by another package when any of them fixes it.
func methodsNamed(reachable reach, name string) iter.Seq[foreignMethod] {
	return func(yield func(foreignMethod) bool) {
		for _, methodsByName := range reachable.declared {
			for _, method := range methodsByName[name] {
				if !yield(method) {
					return
				}
			}
		}
	}
}

// packageState is what the rule reads once for its package, held in its context's state.
type packageState struct {
	// reachable is what the rule knows of the packages this one reaches, read as the walk enters its first file.
	reachable reach
	// writes counts the package's writes of every variable, only once a finding asks, so a package with no
	// abbreviated error pays no second walk.
	writes func() map[*types.Var]*variableWrites
}

// enterFile reads what the package reaches, the first time.
func enterFile(context *rule.Context, _ *ast.File) {
	state := rule.State[packageState](context)
	if state.writes != nil {
		return
	}
	state.reachable = reachOf(declarationsOf(context.FileSet), context.Package, context.Reads())
	state.writes = sync.OnceValue(func() map[*types.Var]*variableWrites { return writesOf(context) })
}

// judgeNode judges a name the package declares, or the name a type switch binds.
func judgeNode(context *rule.Context, node ast.Node, _ []ast.Node) {
	switch typed := node.(type) {
	case *ast.TypeSwitchStmt:
		// The symbolic variable of `switch value := x.(type)` is ours, though go/types records no object for it in
		// Defs, only one per clause in Implicits.
		assignment, isAssignment := typed.Assign.(*ast.AssignStmt)
		if !isAssignment || len(assignment.Lhs) != 1 {
			return
		}
		if identifier, isIdentifier := assignment.Lhs[0].(*ast.Ident); isIdentifier {
			state := rule.State[packageState](context)
			judge(context, state.reachable, state.writes, identifier)
		}
	case *ast.Ident:
		object, isDefinition := context.TypesInfo.Defs[typed]
		// Only a declaration: every other identifier is a use, the declaring code's spelling. A definition with no
		// object is the package clause's name or the type switch variable handled above.
		if !isDefinition || object == nil {
			return
		}
		state := rule.State[packageState](context)
		if isOurs(context, state.reachable, object) {
			judge(context, state.reachable, state.writes, typed)
		}
	}
}

// isOurs is whether a declared object's name is this package's to choose.
func isOurs(context *rule.Context, reachable reach, object types.Object) bool {
	switch declared := object.(type) {
	case *types.PkgName:
		// An import's name, renamed or not, is the imported package's.
		return false
	case *types.Var:
		// An embedded field is named for its type, which is judged where it is declared, if it is ours at all.
		return !declared.Embedded()
	case *types.Func:
		return !fixedByAnotherPackage(context, reachable, declared)
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
func fixedByAnotherPackage(context *rule.Context, reachable reach, method *types.Func) bool {
	if method.Signature().Recv() == nil {
		return false
	}
	// The answer depends on every method of the import closure, which no type read of the package holds, so the
	// question and the method that answers it, or none, are recorded in the context's reads (#b2t9xmf).
	fixing := fixingMethod(reachable, method)
	context.Reads().AddLookup(build_reads.Lookup{
		Query:   build_reads.QueryMethod,
		Name:    method.Name(),
		Subject: method,
		Found:   fixing != nil,
		Answer:  fixing,
	})
	return fixing != nil
}

// fixingMethod is the method of another package, as it declares it, that fixes method's name (see
// fixedByAnotherPackage), nil when none does. method has a receiver.
func fixingMethod(reachable reach, method *types.Func) *types.Func {
	receiver := method.Signature().Recv()
	isInterfaceMethod := types.IsInterface(receiver.Type())
	named := receiverNamed(receiver.Type())
	for candidate := range methodsNamed(reachable, method.Name()) {
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
					return candidate.method
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
			return candidate.method
		}
		if foreign.owner == nil {
			continue
		}
		if named == nil || named.TypeParams().Len() > 0 {
			return candidate.method
		}
		if types.Implements(named, foreign.owner) || types.Implements(types.NewPointer(named), foreign.owner) {
			return candidate.method
		}
	}
	return nil
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

// reachOf walks every package this one reaches through its imports, once per package, and gathers what the rule
// asks of them, each package's methods from what the load has already read of it.
func reachOf(declarations *loadDeclarations, current *types.Package, reads *build_reads.Reads) reach {
	reachable := reach{packageNames: map[string]bool{}, reads: reads}
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
		if methodsByName := declarations.of(imported); len(methodsByName) > 0 {
			reachable.declared = append(reachable.declared, methodsByName)
		}
	}
	return reachable
}

// declaredMethodsOf are the methods of every interface and named type one package declares, by name. Nothing in them
// depends on the package that reaches it: they are read off the declaring package's own scope, and a generic type's
// are kept uninstantiated, to be instantiated against the method a finding asks about (typeArgumentsOf).
func declaredMethodsOf(declaring *types.Package) map[string][]foreignMethod {
	methodsByName := map[string][]foreignMethod{}
	for _, name := range declaring.Scope().Names() {
		typeName, isTypeName := declaring.Scope().Lookup(name).(*types.TypeName)
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
				methodsByName[method.Name()] = append(methodsByName[method.Name()],
					foreignMethod{method: method, owner: declaredInterface, generic: generic})
			}
			continue
		}
		for index := range named.NumMethods() {
			method := named.Method(index)
			methodsByName[method.Name()] = append(methodsByName[method.Name()],
				foreignMethod{method: method, generic: generic})
		}
	}
	return methodsByName
}

// judge reports a declared name the vocabulary finds abbreviated.
func judge(
	context *rule.Context,
	reachable reach,
	writes func() map[*types.Var]*variableWrites,
	identifier *ast.Ident,
) {
	if identifier.Name == "_" {
		return
	}
	finding, found := abbreviation.Policy.Find(identifier.Name)
	if !found {
		return
	}
	values := finding.Values
	descriptive := false
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
		descriptive = true
	}
	diagnostic := analysis.Diagnostic{
		Pos:      identifier.Pos(),
		End:      identifier.End(),
		Category: finding.MessageId,
		Message:  messages[finding.MessageId].Render(values),
	}
	// A local error or context the advice asks a descriptive name of has one when where it came from names it, so
	// the finding names that spelling and offers it as the rename, the name the codemods give the same local.
	if descriptive {
		if newName, messageId, named := originName(context, reachable, writes, identifier); named {
			renameValues := map[string]string{"name": identifier.Name, "suggestedName": newName}
			diagnostic.Category = messageId
			diagnostic.Message = originMessages[messageId].Render(renameValues)
			diagnostic.SuggestedFixes = rule.RenameFixes(identifier, newName,
				fullWordRenameMessage.Render(renameValues))
		}
	} else if spelling, isOne := oneSpelling(reachable, finding, identifier.Name); isOne {
		diagnostic.SuggestedFixes = rule.RenameFixes(identifier, spelling, fullWordRenameMessage.Render(
			map[string]string{"name": identifier.Name, "suggestedName": spelling},
		))
	}
	context.Report(diagnostic)
}

// oneSpelling is the full spelling a finding's advice names for the whole name, when it names exactly one and that
// one is a name this package may declare. A word with several meanings names one per meaning and leaves the choice to
// the author, an advice-style form names none, and a whole word whose expansion Go already uses is advised toward a
// descriptive name above, as `err` and `ctx` are, so none of them is a rename by its spelling. A local error or
// context may be one by where it came from instead (originName).
func oneSpelling(reachable reach, finding abbreviation.Finding, name string) (string, bool) {
	if len(finding.Spellings) != 1 || finding.Spellings[0] == name || isTakenInGo(reachable, finding.Spellings[0]) {
		return "", false
	}
	return finding.Spellings[0], true
}

// isTakenInGo is whether a name is a keyword, a predeclared identifier, or the name of a package this one reaches, so
// declaring it would be illegal or would shadow what a reader expects it to mean. Whether a reached package is so
// named is recorded in the reachable packages' reads.
func isTakenInGo(reachable reach, name string) bool {
	if token.IsKeyword(name) || types.Universe.Lookup(name) != nil {
		return true
	}
	taken := reachable.packageNames[name]
	reachable.reads.AddLookup(build_reads.Lookup{Query: build_reads.QueryPackageName, Name: name, Found: taken})
	return taken
}
