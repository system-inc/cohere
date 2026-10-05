package flow

import (
	"strconv"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
)

// StepKind is how the walk moved from a type into one of its parts.
type StepKind string

const (
	StepElement      StepKind = "Element"
	StepProperty     StepKind = "Property"
	StepTypeArgument StepKind = "TypeArgument"
	StepReturn       StepKind = "Return"
	StepParameter    StepKind = "Parameter"
)

// Step is one move into a part: an array's element, a tuple's element at Index, a property by Name, a
// container's type argument at Index (Name is the container, `Map`), a function's return, or its
// parameter at Index.
type Step struct {
	Kind  StepKind
	Name  string
	Index int
}

// Pair is one part of a site's source related to the matching part of its target.
//
// Mutable is the slot's own mutability, read off the target: an element of `T[]` or a property without
// `readonly` can be written through the target's name, so whatever the source put there must be exactly
// what the target says. A `readonly` slot is read-only through the target, so a narrower source is fine
// there, and only the slots beneath it can still be holes.
type Pair struct {
	Source  *checker.Type
	Target  *checker.Type
	Path    []Step
	Mutable bool
}

// Judge rules on one pair: wrong when the pair is the hole the rule exists for, and descend when the walk
// should go on into its parts. A rule that owns a whole type (nominal-class owns a class instance) stops
// the walk there, so two rules do not report one hole twice.
type Judge func(pair Pair) (wrong bool, descend bool)

// maximumDepth bounds the walk. A recursive type meets its own pair first and stops on the visited set;
// this is for a type that keeps producing new ones, a generic instantiating itself deeper each level.
const maximumDepth = 8

// Walk relates a site's source to its target part by part, the way tsc related them when it accepted the
// site, and returns the first pair judge finds wrong.
//
// What it pairs:
//
//	T[] and readonly T[]    the element, mutable unless the target is readonly; a tuple source pairs
//	                        each of its elements with the target's element
//	tuples                  element by element, mutable unless the target tuple is readonly
//	Map, Set, WeakMap,      the type arguments, mutable: their writes are methods, which tsc compares
//	WeakSet                 both ways (probe h04), so no structural walk could see them
//	ReadonlyMap,            the type arguments, read-only
//	ReadonlySet
//	functions               a single call signature each: the return, and the parameters with source
//	                        and target swapped, since a parameter is written by the caller
//	objects                 each property of the target the source also has, by name, mutable unless
//	                        the target's property is readonly; methods are skipped, as their variance is
//	                        method-signature-style's and nominal-class's business
//	unions                  a source union pairs each member; a target union pairs the members the
//	                        source is assignable to, and passes when any one of them passes
//
// Index signatures outside arrays are not paired yet: whether one is readonly is an IndexInfo field the
// shim does not reach, and `Record<string, T>` is a map Adamic refuses in favor of `Map`. A lib generic
// whose writes are methods and is not in the list above relates the way tsc does.
//
// A fresh site is judged at its top only. Its parts are sites of their own, so descending would report
// one hole twice.
func Walk(typeChecker *checker.Checker, site Site, judge Judge) (Pair, bool) {
	top := Pair{Source: site.Source, Target: site.Target}
	if site.Fresh {
		wrong, _ := judge(top)
		return top, wrong
	}
	walker := walker{typeChecker: typeChecker, judge: judge, visited: map[[2]*checker.Type]bool{}, newContainer: site.NewContainer}
	return walker.relate(top, 0)
}

type walker struct {
	typeChecker *checker.Checker
	judge       Judge
	visited     map[[2]*checker.Type]bool

	// newContainer is a site whose source container was just built: its own slots are read-only to the
	// walk, since no other name can write into them. See Site.NewContainer.
	newContainer bool
}

func (w walker) relate(pair Pair, depth int) (Pair, bool) {
	if pair.Source == nil || pair.Target == nil || pair.Source == pair.Target || depth > maximumDepth {
		return Pair{}, false
	}
	key := [2]*checker.Type{pair.Source, pair.Target}
	if w.visited[key] {
		return Pair{}, false
	}
	w.visited[key] = true

	wrong, descend := w.judge(pair)
	if wrong {
		return pair, true
	}
	if !descend {
		return Pair{}, false
	}
	// A union's members are paired as parts of the same slot, so the slot's own mutability was judged above,
	// on the whole pair, and is not judged again member by member: a `Handler | null` slot is not a `null`
	// slot a handler could be written into. Before this, every element seen as an `Element` reported its
	// `onfullscreenchange` this way, on all four consumers (#drbrp8c). What lies inside each member keeps its
	// own mutability.
	if pair.Source.Flags()&checker.TypeFlagsUnion != 0 {
		for _, member := range pair.Source.Types() {
			if found, isWrong := w.relate(Pair{Source: member, Target: pair.Target, Path: pair.Path}, depth+1); isWrong {
				return found, true
			}
		}
		return Pair{}, false
	}
	if pair.Target.Flags()&checker.TypeFlagsUnion != 0 {
		var first Pair
		failed := false
		for _, member := range pair.Target.Types() {
			if !checker.Checker_isTypeAssignableTo(w.typeChecker, pair.Source, member) {
				continue
			}
			found, isWrong := w.relate(Pair{Source: pair.Source, Target: member, Path: pair.Path}, depth+1)
			if !isWrong {
				return Pair{}, false
			}
			if !failed {
				first, failed = found, true
			}
		}
		return first, failed
	}
	return w.parts(pair, depth)
}

// parts pairs the parts of two types that are not unions.
func (w walker) parts(pair Pair, depth int) (Pair, bool) {
	source, target := pair.Source, pair.Target
	// The slots of a container the site just built are no one else's, so writing through the wider type
	// reaches only this value. Its parts' own slots keep their mutability.
	ownSlotsShared := !(w.newContainer && len(pair.Path) == 0)
	into := func(kind StepKind, name string, index int) []Step {
		path := make([]Step, len(pair.Path), len(pair.Path)+1)
		copy(path, pair.Path)
		return append(path, Step{Kind: kind, Name: name, Index: index})
	}
	each := func(pairs []Pair) (Pair, bool) {
		for _, part := range pairs {
			if found, wrong := w.relate(part, depth+1); wrong {
				return found, true
			}
		}
		return Pair{}, false
	}

	if checker.Checker_isArrayType(w.typeChecker, target) {
		targetElement := w.typeArgument(target, 0)
		mutable := ownSlotsShared && !isNamed(target, "ReadonlyArray")
		var pairs []Pair
		switch {
		case checker.Checker_isArrayType(w.typeChecker, source):
			pairs = append(pairs, Pair{w.typeArgument(source, 0), targetElement, into(StepElement, "", -1), mutable})
		case checker.IsTupleType(source):
			for _, element := range checker.Checker_getTypeArguments(w.typeChecker, source) {
				pairs = append(pairs, Pair{element, targetElement, into(StepElement, "", -1), mutable})
			}
		}
		return each(pairs)
	}
	if checker.IsTupleType(target) {
		if !checker.IsTupleType(source) {
			return Pair{}, false
		}
		mutable := ownSlotsShared && !target.TargetTupleType().IsReadonly()
		sourceElements := checker.Checker_getTypeArguments(w.typeChecker, source)
		targetElements := checker.Checker_getTypeArguments(w.typeChecker, target)
		var pairs []Pair
		for index := 0; index < len(sourceElements) && index < len(targetElements); index++ {
			pairs = append(pairs, Pair{sourceElements[index], targetElements[index], into(StepElement, "", index), mutable})
		}
		return each(pairs)
	}
	if container, readonly, isContainer := containerOf(target); isContainer {
		sourceContainer, _, sourceIsContainer := containerOf(source)
		if !sourceIsContainer || strings.TrimPrefix(sourceContainer, "Readonly") != strings.TrimPrefix(container, "Readonly") {
			return Pair{}, false
		}
		sourceArguments := checker.Checker_getTypeArguments(w.typeChecker, source)
		targetArguments := checker.Checker_getTypeArguments(w.typeChecker, target)
		var pairs []Pair
		for index := 0; index < len(sourceArguments) && index < len(targetArguments); index++ {
			pairs = append(pairs, Pair{sourceArguments[index], targetArguments[index], into(StepTypeArgument, container, index), ownSlotsShared && !readonly})
		}
		return each(pairs)
	}
	if target.Flags()&checker.TypeFlagsObject == 0 || source.Flags()&checker.TypeFlagsObject == 0 {
		return Pair{}, false
	}
	targetSignatures := checker.Checker_getSignaturesOfType(w.typeChecker, target, checker.SignatureKindCall)
	sourceSignatures := checker.Checker_getSignaturesOfType(w.typeChecker, source, checker.SignatureKindCall)
	var pairs []Pair
	if len(targetSignatures) == 1 && len(sourceSignatures) == 1 {
		targetSignature, sourceSignature := targetSignatures[0], sourceSignatures[0]
		pairs = append(pairs, Pair{
			checker.Checker_getReturnTypeOfSignature(w.typeChecker, sourceSignature),
			checker.Checker_getReturnTypeOfSignature(w.typeChecker, targetSignature),
			into(StepReturn, "", -1), false,
		})
		sourceParameters := checker.Signature_parameters(sourceSignature)
		targetParameters := checker.Signature_parameters(targetSignature)
		// Positional parameters pair by position, and the pairing stops at a rest parameter on either side,
		// whose type is a tuple or array of everything after it rather than one parameter's type.
		for index := 0; index < len(sourceParameters) && index < len(targetParameters); index++ {
			if isRestParameter(sourceParameters[index]) || isRestParameter(targetParameters[index]) {
				break
			}
			pairs = append(pairs, Pair{
				checker.Checker_getTypeOfSymbol(w.typeChecker, targetParameters[index]),
				checker.Checker_getTypeOfSymbol(w.typeChecker, sourceParameters[index]),
				into(StepParameter, sourceParameters[index].Name, index), false,
			})
		}
	}
	for _, property := range checker.Checker_getPropertiesOfType(w.typeChecker, target) {
		if property.Flags&ast.SymbolFlagsMethod != 0 {
			continue
		}
		sourceProperty := checker.Checker_getPropertyOfType(w.typeChecker, source, property.Name)
		if sourceProperty == nil {
			continue
		}
		pairs = append(pairs, Pair{
			checker.Checker_getTypeOfSymbol(w.typeChecker, sourceProperty),
			checker.Checker_getTypeOfSymbol(w.typeChecker, property),
			into(StepProperty, property.Name, -1),
			ownSlotsShared && !checker.Checker_isReadonlySymbol(w.typeChecker, property),
		})
	}
	return each(pairs)
}

func (w walker) typeArgument(t *checker.Type, index int) *checker.Type {
	arguments := checker.Checker_getTypeArguments(w.typeChecker, t)
	if index >= len(arguments) {
		return nil
	}
	return arguments[index]
}

// containers are the library generics whose writes are methods, and whether each is read-only.
var containers = map[string]bool{
	"Map": false, "Set": false, "WeakMap": false, "WeakSet": false,
	"ReadonlyMap": true, "ReadonlySet": true,
}

// containerOf names t's container and whether it is read-only, when t is a reference to one declared in a
// declaration file. The file test stands in for "the library": Adamic ships its own `.d.ts` under noLib,
// so asking whether the symbol came from TypeScript's default library would miss Adamic's own Map.
func containerOf(t *checker.Type) (string, bool, bool) {
	if t.ObjectFlags()&checker.ObjectFlagsReference == 0 || t.Target() == nil {
		return "", false, false
	}
	symbol := t.Target().Symbol()
	if symbol == nil {
		return "", false, false
	}
	readonly, isContainer := containers[symbol.Name]
	if !isContainer || !declaredInDeclarationFile(symbol) {
		return "", false, false
	}
	return symbol.Name, readonly, true
}

// isNamed is a type reference whose generic target is the library type of that name.
func isNamed(t *checker.Type, name string) bool {
	if t.ObjectFlags()&checker.ObjectFlagsReference == 0 || t.Target() == nil {
		return false
	}
	symbol := t.Target().Symbol()
	return symbol != nil && symbol.Name == name && declaredInDeclarationFile(symbol)
}

func declaredInDeclarationFile(symbol *ast.Symbol) bool {
	for _, declaration := range symbol.Declarations {
		if sourceFile := ast.GetSourceFileOfNode(declaration); sourceFile != nil && sourceFile.IsDeclarationFile {
			return true
		}
	}
	return false
}

// isRestParameter is a parameter declared `...name`.
func isRestParameter(parameter *ast.Symbol) bool {
	declaration := parameter.ValueDeclaration
	return declaration != nil && declaration.Kind == ast.KindParameter &&
		declaration.AsParameterDeclaration().DotDotDotToken != nil
}

// IsArrayLike is an array or a tuple, read-only or not.
func IsArrayLike(typeChecker *checker.Checker, t *checker.Type) bool {
	return checker.Checker_isArrayType(typeChecker, t) || checker.IsTupleType(t)
}

// PathText is a path as a reader follows it from the whole value: `.pets[]`, `<Map value>`, `()`.
func PathText(path []Step) string {
	var text strings.Builder
	for _, step := range path {
		switch step.Kind {
		case StepElement:
			if step.Index >= 0 {
				text.WriteString("[" + strconv.Itoa(step.Index) + "]")
			} else {
				text.WriteString("[]")
			}
		case StepProperty:
			text.WriteString("." + step.Name)
		case StepTypeArgument:
			text.WriteString("<" + step.Name + " " + containerArgumentName(step.Name, step.Index) + ">")
		case StepReturn:
			text.WriteString("()")
		case StepParameter:
			text.WriteString("(" + step.Name + ")")
		}
	}
	return text.String()
}

func containerArgumentName(container string, index int) string {
	switch strings.TrimPrefix(container, "Readonly") {
	case "Map", "WeakMap":
		if index == 0 {
			return "key"
		}
		return "value"
	}
	return "member"
}
