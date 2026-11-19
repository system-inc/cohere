package go_engine

import (
	"cmp"
	"go/token"
	"go/types"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/tools/go/types/objectpath"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
)

// factReachInputs are what one package's side is made from (newFactReachPackage), each the host's own:
//   - fileSet places the record's declarations in their files.
//   - record is stage 1's record of what each of the package's declarations uses.
//   - renderer renders the identities, on the caller's goroutine.
//   - answers are the host's answers about the package's objects, each with Answer set.
//   - reads are the answers the host's analysis of the package took from other packages.
//   - noReturn reports whether the host holds the no-return fact of a function.
//   - orderBound are the objects whose answers hang on the order the host walked the package in.
type factReachInputs struct {
	fileSet    *token.FileSet
	record     *build_reads.FunctionUses
	renderer   *build_reads.IdentityRenderer
	answers    []check.ExportedFact
	reads      []fact_resolver.Read
	noReturn   func(function *types.Func) bool
	orderBound []types.Object
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

// factReachParameter is a parameter of a declared function: the function, and the parameter's index among its
// parameters.
type factReachParameter struct {
	function *types.Func
	index    int
}

// factReachBuilder makes one package's side from inputs, and is let go once it has. rendered keeps each object's
// identity and key, rendered once however many bodies use it. functions and parameters are what the record's function
// declarations declare, which the answers are placed by, and met the functions a no-return fact is compared on
// already, by key.
type factReachBuilder struct {
	inputs     factReachInputs
	hosted     *factReachPackage
	rendered   map[types.Object]factReachRendered
	functions  map[*types.Func]factReachFunction
	parameters map[*types.Var]factReachParameter
	met        map[fact_reach.Object]bool
}

// newFactReachPackage is the host's side of the package inputs are of (factReachPackage).
func newFactReachPackage(inputs factReachInputs) *factReachPackage {
	builder := &factReachBuilder{
		inputs: inputs,
		hosted: &factReachPackage{users: map[fact_reach.Object][]fact_reach.Place{},
			recorded: map[fact_reach.Place][]fact_reach.Question{}, askings: map[string]fact_reach.Asking{}},
		rendered: map[types.Object]factReachRendered{}, functions: map[*types.Func]factReachFunction{},
		parameters: map[*types.Var]factReachParameter{}, met: map[fact_reach.Object]bool{},
	}
	for declared := range inputs.record.Declarations() {
		// A type has no body, and a value's initializer is no function's: neither is a place.
		if declared.Declaration.Kind != build_reads.DeclaredFunction {
			continue
		}
		place, isPlaced := fact_reach.Place{}, false
		if declared.HasBody {
			if tokenFile := inputs.fileSet.File(declared.Declaration.File); tokenFile != nil {
				place = fact_reach.Place{File: tokenFile.Name(), Offset: declared.Declaration.Offset}
				isPlaced = true
				builder.hosted.places = append(builder.hosted.places, place)
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
	for _, answer := range inputs.answers {
		builder.answer(answer)
	}
	for _, questions := range builder.hosted.recorded {
		slices.SortFunc(questions, func(first fact_reach.Question, second fact_reach.Question) int {
			return cmp.Compare(first.Key, second.Key)
		})
	}
	for _, read := range inputs.reads {
		builder.read(read)
	}
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

// meet adds function to the functions a no-return fact is compared on, with the host's fact of it, the first time
// its key is met. A method of an interface is left out: its receiver's type is the interface, it has no body in any
// build, and no fact is ever about it. So is a function no identity names, which no build could find its own of.
func (builder *factReachBuilder) meet(function *types.Func) {
	if receiver := function.Signature().Recv(); receiver != nil && types.IsInterface(receiver.Type()) {
		return
	}
	rendered := builder.render(function)
	if !rendered.known || builder.met[rendered.key] {
		return
	}
	builder.met[rendered.key] = true
	builder.hosted.functions = append(builder.hosted.functions, fact_reach.Function{Object: rendered.key,
		Identity: rendered.identity, NoReturn: builder.inputs.noReturn(function)})
}

// declare keeps each function declared declares, by its object, with the place of its body, and each of its
// parameters by the parameter's object, and meets each declared with a body. An init function and one named `_`
// declare no object, and no answer or fact is about either.
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
		parameters := function.Signature().Params()
		for index := range parameters.Len() {
			builder.parameters[parameters.At(index)] = factReachParameter{function: function, index: index}
		}
		if declared.HasBody {
			builder.meet(function)
		}
	}
}

// use makes the body at place, declared's, a user of each object its body uses, the object selected for a selection,
// and meets each function among them. Its signature's uses are not its body's: a rule reads a fact where a body names
// or selects its object. A body with a use no identity names is a user of every object instead, as one the record
// marks unknown is. The recorder marks a body unknown wherever a use of it renders unknown, by the renderer's own rule
// (build_reads.unplacedIdentity), so the second check finds nothing today, and stands for a record made any other way.
func (builder *factReachBuilder) use(place fact_reach.Place, declared build_reads.DeclarationUses) {
	isFollowed := !declared.BodyUnknown
	keys := make([]fact_reach.Object, 0, len(declared.Body))
	for _, use := range declared.Body {
		object := builder.inputs.record.Used(use).Object
		if function, isFunction := object.(*types.Func); isFunction {
			builder.meet(function)
		}
		rendered := builder.render(object)
		isFollowed = isFollowed && rendered.known
		keys = append(keys, rendered.key)
	}
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
// reads the same wherever the function's types do. Any other answer that finds no place is counted unplaced.
func (builder *factReachBuilder) answer(answer check.ExportedFact) {
	var function *types.Func
	parameter := -1
	switch object := answer.Object.(type) {
	case *types.Func:
		function = object
	case *types.Var:
		declared, isParameter := builder.parameters[object]
		if !isParameter {
			builder.hosted.unplaced++
			return
		}
		function, parameter = declared.function, declared.index
	default:
		builder.hosted.unplaced++
		return
	}
	declared, isDeclared := builder.functions[function]
	if !isDeclared {
		builder.hosted.unplaced++
		return
	}
	if !declared.placed {
		return
	}
	if question, isNew := builder.ask(answer.Analyzer, function, parameter, answer.Encoded); isNew {
		builder.hosted.recorded[declared.place] = append(builder.hosted.recorded[declared.place], question)
	}
}

// read records read, an answer the host's analysis took from another package, as a question asked before any body
// leaves: about the function its path names, or the function declaring the parameter its path names. A read with no
// owner, or whose path names nothing in its owner, or anything else, is counted unplaced.
func (builder *factReachBuilder) read(read fact_resolver.Read) {
	if read.Owner == nil {
		builder.hosted.unplaced++
		return
	}
	object, objectError := objectpath.Object(read.Owner, read.Path)
	if objectError != nil {
		builder.hosted.unplaced++
		return
	}
	var function *types.Func
	parameter := -1
	switch object := object.(type) {
	case *types.Func:
		function = object
	case *types.Var:
		declaring, index, isParameter := factReachParameterOf(read.Owner, read.Path, object)
		if !isParameter {
			builder.hosted.unplaced++
			return
		}
		function, parameter = declaring, index
	default:
		builder.hosted.unplaced++
		return
	}
	if question, isNew := builder.ask(read.Kind, function, parameter, read.Value); isNew {
		builder.hosted.imported = append(builder.hosted.imported, question)
	}
}

// factReachParameterSuffix is how an objectpath goes from a function to one of its parameters, before the parameter's
// index: `.` for the function's type, `P` for the signature's parameters and `A` for the one at an index.
const factReachParameterSuffix = ".PA"

// factReachParameterOf is the function of owner that declares variable, the object path names there, as a parameter,
// and the parameter's index, false when variable is no function's parameter. An objectpath writes a parameter as its
// function's path, then factReachParameterSuffix and the index, so the function is looked up by what comes before.
// The path is only where to look: what it leads to must be a function, and hold variable itself at that index.
func factReachParameterOf(owner *types.Package, path objectpath.Path, variable *types.Var) (*types.Func, int, bool) {
	cut := strings.LastIndex(string(path), factReachParameterSuffix)
	if cut < 0 {
		return nil, 0, false
	}
	index, indexError := strconv.Atoi(string(path)[cut+len(factReachParameterSuffix):])
	if indexError != nil || index < 0 {
		return nil, 0, false
	}
	declaring, declaringError := objectpath.Object(owner, path[:cut])
	if declaringError != nil {
		return nil, 0, false
	}
	function, isFunction := declaring.(*types.Func)
	if !isFunction {
		return nil, 0, false
	}
	parameters := function.Signature().Params()
	if index >= parameters.Len() || parameters.At(index) != variable {
		return nil, 0, false
	}
	return function, index, true
}
