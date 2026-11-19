package go_engine

import (
	"cmp"
	"go/token"
	"go/types"
	"slices"
	"strconv"
	"strings"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
)

// The host's side of the facts' fixed point, stage 4 of #9paaen9 (#5vbs1np): for one package the host checked from
// source, the bodies that use each object, from stage 1's record of what each declaration uses
// (build_reads.FunctionUses), and the answers that rest on each body's function, from what the host's fact resolver
// computed (check.ExportedFact). fact_reach.Settle reads the two as its Source. Nothing but a test reads this yet: the
// unit that asks a build each question, and names the objects known to differ before any is asked, comes after.

// factReachSeparator joins the parts of a key. No part can hold it: a NUL byte is in no package's path, no file's
// name, no objectpath, no identifier, no kind's name and no number written out. A file's digest is whatever its
// renderer's caller reads it as, which no run does yet, and must hold none either. So two keys are equal exactly when
// each part of one is the same part of the other.
const factReachSeparator = "\x00"

// factReachObject is the key of the object identity names, and false for an unknown identity, which names no object
// and so has no key. The key holds every field build_reads.Identity.Equal compares, in one order, each ended by the
// separator none of them can hold, so two keys are equal exactly when their identities are. Never the identity's
// String, which leaves fields out and writes an unknown identity as a word another unknown one would match.
func factReachObject(identity build_reads.Identity) (fact_reach.Object, bool) {
	if identity.Kind == build_reads.IdentityUnknown {
		return "", false
	}
	return fact_reach.Object(strings.Join([]string{string(identity.Kind), identity.PackagePath, string(identity.Path),
		identity.Method, identity.File, strconv.Itoa(identity.Ordinal), identity.Digest}, factReachSeparator)), true
}

// factReachQuestion is what a build is asked of one question the host recorded (fact_reach.Question), and what its
// answer is read beside: the answer's kind, the function it is of, or that declares the parameter it is of, by the
// identity that names the function in any build, the parameter's index among the function's, -1 for an answer of the
// function itself, and the host's answer as its resolver encoded it.
type factReachQuestion struct {
	kind      string
	function  build_reads.Identity
	parameter int
	encoded   []byte
}

// factReachPackage is the host's side of one package for the facts' fixed point (fact_reach.Source), made once, on
// the goroutine that renders its identities, and read only after, so every build's fixed point can share it. What it
// hands out are places, keys and identities, never an object of the host's, which no other build could read.
type factReachPackage struct {
	// users are, by an object's key, the bodies whose own uses hold the object, in the order the record declares them,
	// and everything the bodies that use every object, each holding a use the record could not follow. A body of
	// everything is in no key's users, so the two never hold one body twice.
	users      map[fact_reach.Object][]fact_reach.Place
	everything []fact_reach.Place

	// recorded are, by a body's place, the questions the host's answers about its function and its parameters make,
	// in the order of their keys, since the answers come in a map's order and the fixed point asks, and takes bodies
	// out, in the order it reads them. questions are what a build is asked of each, by the question's key.
	recorded  map[fact_reach.Place][]fact_reach.Question
	questions map[string]factReachQuestion

	// places are each declared function's body, by the function's key: the place a function whose own fact differs
	// stands at, where one declared without a body has none.
	places map[fact_reach.Object]fact_reach.Place

	// unplaced counts the answers no body could take: one about the package, a result, a receiver, a function no
	// declaration of the record declares or one no identity names. It counts too each body the file set places in no
	// file, whose answers and uses have nowhere to rest. A caller must lint whole, in every build, a package that has
	// any, since an answer it cannot place is one it cannot compare.
	unplaced int
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

// factReachBuilder makes one package's side, and is let go once it has: record is stage 1's record of the package and
// renderer renders its objects' identities, each object once, kept in rendered, however many bodies use it. functions
// and parameters are what the record's function declarations declare, which the answers are placed by.
type factReachBuilder struct {
	hosted     *factReachPackage
	record     *build_reads.FunctionUses
	renderer   *build_reads.IdentityRenderer
	rendered   map[types.Object]factReachRendered
	functions  map[*types.Func]factReachFunction
	parameters map[*types.Var]factReachParameter
}

// newFactReachPackage is the host's side of the package record is of (factReachPackage): fileSet places the record's
// declarations in their files, answers are the host's answers about the package's objects, each with Answer set, and
// renderer renders the identities, on the caller's goroutine.
func newFactReachPackage(
	fileSet *token.FileSet,
	record *build_reads.FunctionUses,
	answers []check.ExportedFact,
	renderer *build_reads.IdentityRenderer,
) *factReachPackage {
	builder := &factReachBuilder{
		hosted: &factReachPackage{users: map[fact_reach.Object][]fact_reach.Place{},
			recorded:  map[fact_reach.Place][]fact_reach.Question{},
			questions: map[string]factReachQuestion{}, places: map[fact_reach.Object]fact_reach.Place{}},
		record: record, renderer: renderer, rendered: map[types.Object]factReachRendered{},
		functions: map[*types.Func]factReachFunction{}, parameters: map[*types.Var]factReachParameter{},
	}
	for declared := range record.Declarations() {
		// A type has no body, and a value's initializer is no function's: neither is a place.
		if declared.Declaration.Kind != build_reads.DeclaredFunction {
			continue
		}
		place, isPlaced := fact_reach.Place{}, false
		if declared.HasBody {
			if tokenFile := fileSet.File(declared.Declaration.File); tokenFile != nil {
				place = fact_reach.Place{File: tokenFile.Name(), Offset: declared.Declaration.Offset}
				isPlaced = true
				builder.use(place, declared)
			} else {
				// A record always places its declarations in the host's files. One that does not has a body the fixed
				// point cannot name, so its package is one to lint whole.
				builder.hosted.unplaced++
			}
		}
		builder.declare(declared.Declaration, place, isPlaced)
	}
	for _, answer := range answers {
		builder.answer(answer)
	}
	for _, questions := range builder.hosted.recorded {
		slices.SortFunc(questions, func(first fact_reach.Question, second fact_reach.Question) int {
			return cmp.Compare(first.Key, second.Key)
		})
	}
	return builder.hosted
}

// render is object's identity and key, rendered once.
func (builder *factReachBuilder) render(object types.Object) factReachRendered {
	rendered, isRendered := builder.rendered[object]
	if !isRendered {
		rendered.identity = builder.renderer.Object(object)
		rendered.key, rendered.known = factReachObject(rendered.identity)
		builder.rendered[object] = rendered
	}
	return rendered
}

// use makes the body at place, declared's, a user of each object its body uses, the object selected for a selection.
// Its signature's uses are not its body's: a rule reads a fact where a body names or selects its object. A body with a
// use no identity names is a user of every object instead, as one the record marks unknown is. The recorder marks a
// body unknown wherever a use of it renders unknown, by the renderer's own rule (build_reads.unplacedIdentity), so the
// second check finds nothing today, and stands for a record made any other way.
func (builder *factReachBuilder) use(place fact_reach.Place, declared build_reads.DeclarationUses) {
	if declared.BodyUnknown {
		builder.hosted.everything = append(builder.hosted.everything, place)
		return
	}
	keys := make([]fact_reach.Object, 0, len(declared.Body))
	for _, use := range declared.Body {
		rendered := builder.render(builder.record.Used(use).Object)
		if !rendered.known {
			builder.hosted.everything = append(builder.hosted.everything, place)
			return
		}
		keys = append(keys, rendered.key)
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

// declare keeps each function declaration declares, by its object, with the place of its body, and each of its
// parameters by the parameter's object. An init function and one named `_` declare no object, and no answer is about
// either.
func (builder *factReachBuilder) declare(declaration build_reads.Declaration, place fact_reach.Place, isPlaced bool) {
	for _, object := range declaration.Objects {
		function, isFunction := object.(*types.Func)
		if !isFunction {
			continue
		}
		builder.functions[function] = factReachFunction{place: place, placed: isPlaced}
		parameters := function.Signature().Params()
		for index := range parameters.Len() {
			builder.parameters[parameters.At(index)] = factReachParameter{function: function, index: index}
		}
		if rendered := builder.render(function); isPlaced && rendered.known {
			builder.hosted.places[rendered.key] = place
		}
	}
}

// answer records answer as a question under the place of the function it is of, or of the function declaring the
// parameter it is of. The question is about that function either way, since its users are the bodies a differing
// answer reaches. An answer about a function declared without a body is left out: nothing of a body made it, so it
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
	rendered := builder.render(function)
	if !rendered.known {
		builder.hosted.unplaced++
		return
	}
	key := answer.Analyzer + factReachSeparator + string(rendered.key)
	if parameter >= 0 {
		key += factReachSeparator + strconv.Itoa(parameter)
	}
	// The resolver holds one answer of a kind about an object, so a key met again is the same answer handed twice.
	if _, isRecorded := builder.hosted.questions[key]; isRecorded {
		return
	}
	builder.hosted.questions[key] = factReachQuestion{kind: answer.Analyzer, function: rendered.identity,
		parameter: parameter, encoded: answer.Encoded}
	builder.hosted.recorded[declared.place] = append(builder.hosted.recorded[declared.place],
		fact_reach.Question{Key: key, Kind: answer.Analyzer, About: rendered.key})
}

// Users are the bodies whose own uses hold object, in the order the package's files declare them, then the bodies
// that use every object.
func (hosted *factReachPackage) Users(object fact_reach.Object) []fact_reach.Place {
	if len(hosted.everything) == 0 {
		return hosted.users[object]
	}
	return slices.Concat(hosted.users[object], hosted.everything)
}

// Recorded are the questions the host's answers about the function at place, and about its parameters, make.
func (hosted *factReachPackage) Recorded(place fact_reach.Place) []fact_reach.Question {
	return hosted.recorded[place]
}

// question is what a build is asked of the question key names, and whether the host recorded one by that key.
func (hosted *factReachPackage) question(key string) (factReachQuestion, bool) {
	asked, isRecorded := hosted.questions[key]
	return asked, isRecorded
}

// placeOf is the body of the function object names, and false for an object that is no function of the package's
// with a body.
func (hosted *factReachPackage) placeOf(object fact_reach.Object) (fact_reach.Place, bool) {
	place, isPlaced := hosted.places[object]
	return place, isPlaced
}
