package go_engine

import (
	"cmp"
	"go/token"
	"go/types"
	"maps"
	"slices"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/printf"
)

// factReachInputs are what one package's side is made from (newFactReachPackage), each the host's own:
//   - fileSet places the record's declarations in their files.
//   - record is stage 1's record of what each of the package's declarations uses.
//   - renderer renders the identities, on the caller's goroutine.
//   - answers are the host's answers about the package's objects, each with Answer set.
//   - imported are the host's answers about other packages' objects, every one it holds, each with Answer set.
//   - noReturn reports whether the host holds the no-return fact of a function.
//   - pushed are the host's other pushed facts of each object, by the analyzer that learns them, in their words
//     (check.FactReachPackage.Pushed).
//   - orderBound are the objects whose answers hang on the order the host walked the package in.
//   - presence is what each body holds that the presence rows read, by its place, read off the package's syntax while
//     the host's run held it (check.FactReachPackage.Presence), presenceRead whether it was read at all, and markers
//     the method names of the marker interfaces the package declares (check.FactReachPackage.Markers).
type factReachInputs struct {
	fileSet      *token.FileSet
	record       *build_reads.FunctionUses
	renderer     *build_reads.IdentityRenderer
	answers      []check.ExportedFact
	imported     []check.ExportedFact
	noReturn     func(function *types.Func) bool
	pushed       map[string]map[types.Object]string
	orderBound   []types.Object
	presence     map[fact_reach.Place]fact_reach.BodyRecord
	presenceRead bool
	markers      []string
}

// factReachRendered is an object as a package's side names it: its identity, and its key, known false for an object
// no identity names.
type factReachRendered struct {
	identity build_reads.Identity
	key      fact_reach.Object
	known    bool
}

// factReachFunction is a function a declaration of the record declares, as an answer about it is placed: where its
// body is, placed false for one declared without a body or placed in no file.
type factReachFunction struct {
	place  fact_reach.Place
	placed bool
}

// factReachParameter is a parameter of a function the package declares or uses: the function, and the parameter's
// index among its parameters.
type factReachParameter struct {
	function *types.Func
	index    int
}

// factReachBuilder makes one package's side from inputs, and is let go once it has. rendered keeps each object's
// identity and key, rendered once however many bodies use it. functions are the functions the record's declarations
// declare, which the package's own answers are placed by, used the functions its bodies and its variables'
// initializers use, which another package's answers are kept by, and parameters each parameter of either. met are the
// functions a no-return fact is compared on already, by key, metPushed the objects only a wrapper fact is compared on
// already (meetPushed), and usedKeys the objects of any kind the bodies and the initializers use, by key.
type factReachBuilder struct {
	inputs     factReachInputs
	hosted     *factReachPackage
	rendered   map[types.Object]factReachRendered
	functions  map[*types.Func]factReachFunction
	used       map[*types.Func]bool
	parameters map[*types.Var]factReachParameter
	met        map[fact_reach.Object]bool
	metPushed  map[fact_reach.Object]bool
	usedKeys   map[fact_reach.Object]bool
}

// newFactReachPackage is the host's side of the package inputs are of (factReachPackage).
func newFactReachPackage(inputs factReachInputs) *factReachPackage {
	builder := &factReachBuilder{
		inputs: inputs,
		hosted: &factReachPackage{users: map[fact_reach.Object][]fact_reach.Place{},
			recorded: map[fact_reach.Place][]fact_reach.Question{}, askings: map[string]fact_reach.Asking{},
			declared: map[fact_reach.Place][]fact_reach.Object{}, parameters: map[fact_reach.Object]int{},
			initializers: factReachInitializers{uses: map[fact_reach.Object]bool{}},
			presence:     map[fact_reach.Place]fact_reach.PresenceRecord{}},
		rendered: map[types.Object]factReachRendered{}, functions: map[*types.Func]factReachFunction{},
		used: map[*types.Func]bool{}, parameters: map[*types.Var]factReachParameter{},
		met: map[fact_reach.Object]bool{}, metPushed: map[fact_reach.Object]bool{},
		usedKeys: map[fact_reach.Object]bool{},
	}
	// The parsed files placing a body in each name a reader sees, so a name two of them share, by their //line
	// directives, is known (factReachPackage.shared).
	parsedFiles := map[string]map[string]bool{}
	for declared := range inputs.record.Declarations() {
		// A variable's initializer is no function's body and so no place: the package's are one asker (initialize).
		if declared.HasBody && isVariableInitializer(declared.Declaration) {
			builder.initialize(declared)
			continue
		}
		// A type has no body, and a const's value runs in no function: neither is a place.
		if declared.Declaration.Kind != build_reads.DeclaredFunction {
			continue
		}
		place, isPlaced := fact_reach.Place{}, false
		if declared.HasBody {
			if tokenFile := inputs.fileSet.File(declared.Declaration.File); tokenFile != nil {
				// The file a reader sees, past any //line directive, at the offset in the file as parsed: stage 1's
				// rule (build_dependence's newUnit), so an answer is kept at the place it is looked up by.
				place = fact_reach.Place{
					File:   tokenFile.Position(tokenFile.Pos(declared.Declaration.Offset)).Filename,
					Offset: declared.Declaration.Offset,
				}
				isPlaced = true
				builder.hosted.places = append(builder.hosted.places, place)
				if parsedFiles[place.File] == nil {
					parsedFiles[place.File] = map[string]bool{}
				}
				parsedFiles[place.File][tokenFile.Name()] = true
			} else {
				// A record always places its declarations in the host's files. One that does not has a body the fixed
				// point cannot name, so its package is one to lint whole.
				builder.hosted.unplaced++
			}
		}
		builder.declare(declared, place, isPlaced)
		if isPlaced {
			builder.use(place, declared)
		}
	}
	builder.readPresence()
	builder.hosted.markers = inputs.markers
	for name, parsed := range parsedFiles {
		if len(parsed) > 1 {
			if builder.hosted.shared == nil {
				builder.hosted.shared = map[string]bool{}
			}
			builder.hosted.shared[name] = true
		}
	}
	// Each kind the host answered about the package, answering about parameters where an answer of it is about a
	// variable: a function's answers are never about one.
	answeredKinds := map[string]bool{}
	for _, answer := range inputs.answers {
		builder.answer(answer)
		_, isVariable := answer.Object.(*types.Var)
		answeredKinds[answer.Analyzer] = answeredKinds[answer.Analyzer] || isVariable
	}
	for _, name := range slices.Sorted(maps.Keys(answeredKinds)) {
		builder.hosted.kinds = append(builder.hosted.kinds,
			fact_reach.Kind{Name: name, Parameters: answeredKinds[name]})
	}
	for _, answer := range inputs.imported {
		builder.importedAnswer(answer)
	}
	// The answers come in a map's order, and the fixed point asks, and takes bodies out, in the order it reads them.
	byKey := func(first fact_reach.Question, second fact_reach.Question) int {
		return cmp.Compare(first.Key, second.Key)
	}
	for _, questions := range builder.hosted.recorded {
		slices.SortFunc(questions, byKey)
	}
	slices.SortFunc(builder.hosted.imported, byKey)
	slices.SortFunc(builder.hosted.usedObjects, func(first fact_reach.UsedObject, second fact_reach.UsedObject) int {
		return cmp.Compare(first.Object, second.Object)
	})
	for _, object := range inputs.orderBound {
		rendered := builder.render(object)
		if !rendered.known {
			builder.hosted.unplaced++
			continue
		}
		builder.hosted.orderBound = append(builder.hosted.orderBound,
			fact_reach.Named{Object: rendered.key, Reason: factReachOrderBound})
	}
	return builder.hosted
}

// readPresence keeps, by its place, what each body holds that the presence rows read, read off the host's own syntax
// (fact_reach.Host.Presence), each function a body starts by its key. A function no identity names is counted
// unplaced, as a body's use of one is, since no build could find which body it is. A package whose bodies were never
// read is counted unplaced too, and so linted whole: no record would read as no body holding anything, and a standing
// body that starts a goroutine or writes its own any would stand unproven.
func (builder *factReachBuilder) readPresence() {
	if !builder.inputs.presenceRead {
		builder.hosted.unplaced++
		return
	}
	for place, body := range builder.inputs.presence {
		record := fact_reach.PresenceRecord{OwnAny: body.OwnAny, LocalMarkers: body.LocalMarkers}
		record.GoStarts = builder.startedKeys(body.GoStarts)
		record.RunStarts = builder.startedKeys(body.RunStarts)
		builder.hosted.presence[place] = record
	}
}

// startedKeys are the keys of functions, in their order, counting each no identity names unplaced.
func (builder *factReachBuilder) startedKeys(functions []*types.Func) []fact_reach.Object {
	var keys []fact_reach.Object
	for _, function := range functions {
		rendered := builder.render(function)
		if !rendered.known {
			builder.hosted.unplaced++
			continue
		}
		keys = append(keys, rendered.key)
	}
	return keys
}

// render is object's identity and key, rendered once.
func (builder *factReachBuilder) render(object types.Object) factReachRendered {
	rendered, isRendered := builder.rendered[object]
	if !isRendered {
		rendered.identity = builder.inputs.renderer.Object(object)
		rendered.key, rendered.known = factReachObject(rendered.identity)
		builder.rendered[object] = rendered
	}
	return rendered
}

// meet adds function to the functions facts are compared on, with the host's no-return fact and pushed facts of it,
// the first time its key is met. A method of an interface is left out: its receiver's type is the interface, it has
// no body in any build, and no fact is ever about it. So is a function no identity names, which no build could find
// its own of.
func (builder *factReachBuilder) meet(function *types.Func) {
	if receiver := function.Signature().Recv(); receiver != nil && types.IsInterface(receiver.Type()) {
		return
	}
	rendered := builder.render(function)
	if !rendered.known || builder.met[rendered.key] {
		return
	}
	builder.met[rendered.key] = true
	var pushed map[string]string
	if len(builder.inputs.pushed) > 0 {
		pushed = make(map[string]string, len(builder.inputs.pushed))
		for analyzer, facts := range builder.inputs.pushed {
			// A fact is about the generic declaration.
			pushed[analyzer] = facts[function.Origin()]
		}
	}
	builder.hosted.functions = append(builder.hosted.functions, fact_reach.Function{Object: rendered.key,
		Identity: rendered.identity, NoReturn: builder.inputs.noReturn(function), Pushed: pushed})
}

// meetPushed adds object to the objects only printf's wrapper fact is compared on (fact_reach.PushedObject), with the
// host's fact of it, the first time its key is met, where the host ran printf on the package (#zqx7gke): a method of an
// interface, which meet leaves out, or a package-level variable or a field, whose type, or an instance's, has a print
// or printf function's shape. The record names an instance's field or method by its generic declaration, whose type
// may have that shape only once its type parameters are given, as a field of type func(string, ...T) does as
// GenericLogger[any]'s, so the shape is any instance's (printf.PrintShaped). printf admits a candidate by that shape
// alone, so no other object holds a wrapper fact a call reads in any build, and comparing only these keeps every other
// kind's comparison as it was. A local variable is left out: a function literal assigned to one is called only in the
// body that declares the variable, whose own uses of the literal's callees are compared, so that body leaves wherever
// the variable's fact could differ. So is an object no identity names, which no build could find its own of.
func (builder *factReachBuilder) meetPushed(object types.Object) {
	facts, isRun := builder.inputs.pushed[printf.Analyzer.Name]
	if !isRun {
		return
	}
	var typed types.Type
	switch declared := object.(type) {
	case *types.Func:
		if receiver := declared.Signature().Recv(); receiver == nil || !types.IsInterface(receiver.Type()) {
			return
		}
		typed = declared.Signature()
	case *types.Var:
		packageLevel := declared.Pkg() != nil && declared.Pkg().Scope().Lookup(declared.Name()) == declared
		if !packageLevel && !declared.IsField() {
			return
		}
		typed = declared.Type()
	default:
		return
	}
	if !printf.PrintShaped(typed) {
		return
	}
	rendered := builder.render(object)
	if !rendered.known || builder.metPushed[rendered.key] {
		return
	}
	builder.metPushed[rendered.key] = true
	// A fact is about the generic declaration.
	declared := object
	switch generic := object.(type) {
	case *types.Func:
		declared = generic.Origin()
	case *types.Var:
		declared = generic.Origin()
	}
	builder.hosted.pushedObjects = append(builder.hosted.pushedObjects, fact_reach.PushedObject{Object: rendered.key,
		Identity: rendered.identity, Pushed: map[string]string{printf.Analyzer.Name: facts[declared]}})
}

// declare keeps each function declared declares, by its object, with the place of its body, and its parameters,
// meets each declared with a body, and keeps the key of each placed one under its place. An init function and one
// named `_` declare no object, and no answer or fact is about either.
func (builder *factReachBuilder) declare(
	declared build_reads.DeclarationUses,
	place fact_reach.Place,
	isPlaced bool,
) {
	for _, object := range declared.Declaration.Objects {
		function, isFunction := object.(*types.Func)
		if !isFunction {
			continue
		}
		builder.functions[function] = factReachFunction{place: place, placed: isPlaced}
		builder.keepParameters(function)
		if declared.HasBody {
			builder.meet(function)
		}
		if rendered := builder.render(function); isPlaced && rendered.known {
			builder.hosted.declared[place] = append(builder.hosted.declared[place], rendered.key)
			builder.hosted.parameters[rendered.key] = function.Signature().Params().Len()
		}
	}
}

// keepParameters keeps each of function's parameters by the parameter's object, so an answer about one finds the
// function and the parameter's index among its parameters.
func (builder *factReachBuilder) keepParameters(function *types.Func) {
	parameters := function.Signature().Params()
	for index := range parameters.Len() {
		builder.parameters[parameters.At(index)] = factReachParameter{function: function, index: index}
	}
}

// followUses are the keys of the objects declared's body uses, a function's or a variable's initializer, the object
// selected for a selection, and whether every one of them could be followed. It meets each function among them, kept
// as used with its parameters, and each object only a wrapper fact is compared on (meetPushed), and keeps each object
// an identity names among the package's used objects (fact_reach.Host.Used), an initializer's too, so a build can judge
// an object only an initializer uses and the initializers leave by it. The signature's uses are not the body's: a rule
// reads a fact where a body names or selects its object. A body with a use no identity names is not followed, as one
// the record marks unknown is not. The recorder marks a body unknown wherever a use of it renders unknown, by the
// renderer's own rule (build_reads.unplacedIdentity), so the second check finds nothing today, and stands for a record
// made any other way.
func (builder *factReachBuilder) followUses(declared build_reads.DeclarationUses) ([]fact_reach.Object, bool) {
	isFollowed := !declared.BodyUnknown
	keys := make([]fact_reach.Object, 0, len(declared.Body))
	for _, use := range declared.Body {
		object := builder.inputs.record.Used(use).Object
		if function, isFunction := object.(*types.Func); isFunction && !builder.used[function] {
			builder.used[function] = true
			builder.keepParameters(function)
			builder.meet(function)
		}
		builder.meetPushed(object)
		rendered := builder.render(object)
		isFollowed = isFollowed && rendered.known
		keys = append(keys, rendered.key)
		// Kept for a body of everything too, which is a user of each object anyway, so an object only it uses is
		// still one a build can judge.
		if rendered.known && !builder.usedKeys[rendered.key] {
			builder.usedKeys[rendered.key] = true
			builder.hosted.usedObjects = append(builder.hosted.usedObjects,
				fact_reach.UsedObject{Object: rendered.key, Identity: rendered.identity})
		}
	}
	return keys, isFollowed
}

// initialize joins declared, a variable's initializer, to the package's one asker of them
// (factReachPackage.initializers): its place, as the census places it, and the objects its values use, so a function
// only an initializer uses is met, its facts compared, and the host's answers about it asked (importedAnswer). One
// that could not be followed makes the asker a user of every object. An initializer the file set places in no file
// has no place to leave by, and is counted unplaced, as a body is.
func (builder *factReachBuilder) initialize(declared build_reads.DeclarationUses) {
	tokenFile := builder.inputs.fileSet.File(declared.Declaration.File)
	if tokenFile == nil {
		builder.hosted.unplaced++
		return
	}
	// The same rule as a body's place: the file a reader sees, at the offset in the file as parsed.
	builder.hosted.initializers.places = append(builder.hosted.initializers.places, fact_reach.Place{
		File:   tokenFile.Position(tokenFile.Pos(declared.Declaration.Offset)).Filename,
		Offset: declared.Declaration.Offset,
	})
	keys, isFollowed := builder.followUses(declared)
	if !isFollowed {
		builder.hosted.initializers.everything = true
		return
	}
	for _, key := range keys {
		builder.hosted.initializers.uses[key] = true
	}
}

// use makes the body at place, declared's, a user of each object its body uses (followUses). A body that could not be
// followed is a user of every object instead.
func (builder *factReachBuilder) use(place fact_reach.Place, declared build_reads.DeclarationUses) {
	keys, isFollowed := builder.followUses(declared)
	if !isFollowed {
		builder.hosted.everything = append(builder.hosted.everything, place)
		return
	}
	for _, key := range keys {
		// A body holds an object once by its name and once by each selection of it, and is its user once: the bodies
		// come in order, so a list this body is in already ends with it.
		users := builder.hosted.users[key]
		if len(users) > 0 && users[len(users)-1] == place {
			continue
		}
		builder.hosted.users[key] = append(users, place)
	}
}

// ask keeps what a build is asked of the answer of kind, encoded, about function, or about its parameter at index
// parameter, and is its question, about the function either way, since the function's users are the bodies a
// differing answer reaches. It is false for a function no identity names, counted unplaced, and for an answer kept
// already: a resolver holds one answer of a kind about an object, so a key met again is the same answer handed twice.
func (builder *factReachBuilder) ask(
	kind string,
	function *types.Func,
	parameter int,
	encoded []byte,
) (fact_reach.Question, bool) {
	rendered := builder.render(function)
	if !rendered.known {
		builder.hosted.unplaced++
		return fact_reach.Question{}, false
	}
	question := factReachQuestion(kind, rendered.key, parameter)
	if _, isAsked := builder.hosted.askings[question.Key]; isAsked {
		return fact_reach.Question{}, false
	}
	builder.hosted.askings[question.Key] = fact_reach.Asking{Kind: kind, Function: rendered.identity,
		Parameter: parameter, Encoded: encoded}
	return question, true
}

// answer records answer as a question under the place of the function it is of, or of the function declaring the
// parameter it is of. An answer about a function declared without a body is left out: nothing of a body made it, so it
// reads the same wherever the function's types do.
//
// An answer about a variable that is no declared function's parameter is left out too, and counted nowhere. The
// resolver's memo holds a mark for a function literal's parameter, and it needs no question of its own: a literal is
// linted exactly when the declaration around it is; the only calls that read its parameter's mark statically are in
// that same declaration; and the mark is made of the literal's own body, which stands or leaves with the declaration,
// and of its callees' marks, whose difference takes the declaration out as their user. A variable that is a parameter
// of nothing the package declares at all, an instantiation's, is answered from its types alone, the same in every
// build. Counting either would have one closure make its whole package lint whole.
//
// An answer about a function no declaration of the record declares is left out, uncounted, when its kind marks it as
// its types' alone (check.ExportedFact.TypesAlone), as purity's about an `init` function is, which the package's
// initializer calls and no identity names. It reads the same in every build that declares the function, whatever the
// build makes of its body, so no build's answer differs from the host's, nothing compares it, and no finding turns on
// it. Counting it would have one `init` make its whole package lint whole. A marked answer about a function a
// declaration does declare is recorded and asked as any other, so its body, where a build lints it, holds its record
// of the kind.
//
// Any other answer that finds no place is counted unplaced: one about neither a function nor a variable, about a
// function no declaration of the record declares, or about one no identity names.
func (builder *factReachBuilder) answer(answer check.ExportedFact) {
	var function *types.Func
	parameter := -1
	switch object := answer.Object.(type) {
	case *types.Func:
		function = object
	case *types.Var:
		declaring, isParameter := builder.parameters[object]
		if _, isDeclared := builder.functions[declaring.function]; !isParameter || !isDeclared {
			return
		}
		function, parameter = declaring.function, declaring.index
	default:
		builder.hosted.unplaced++
		return
	}
	declared, isDeclared := builder.functions[function]
	if !isDeclared {
		if !answer.TypesAlone {
			builder.hosted.unplaced++
		}
		return
	}
	if !declared.placed {
		return
	}
	if question, isNew := builder.ask(answer.Analyzer, function, parameter, answer.Encoded); isNew {
		builder.hosted.recorded[declared.place] = append(builder.hosted.recorded[declared.place], question)
	}
}

// importedAnswer records answer, one the host holds about another package's object, as a question asked before any
// body leaves, when it is about a function a body or a variable's initializer of the package uses or about a
// parameter of one. The host hands every answer it holds, most of them about functions the package never touches, so
// any other is left out and counted nowhere: no body's findings turn on it. So is one about a function the record
// declares, which is the package's own and is asked as its body leaves.
func (builder *factReachBuilder) importedAnswer(answer check.ExportedFact) {
	var function *types.Func
	parameter := -1
	switch object := answer.Object.(type) {
	case *types.Func:
		function = object
	case *types.Var:
		declaring, isParameter := builder.parameters[object]
		if !isParameter {
			return
		}
		function, parameter = declaring.function, declaring.index
	default:
		return
	}
	if _, isDeclared := builder.functions[function]; isDeclared || !builder.used[function] {
		return
	}
	if question, isNew := builder.ask(answer.Analyzer, function, parameter, answer.Encoded); isNew {
		builder.hosted.imported = append(builder.hosted.imported, question)
	}
}
