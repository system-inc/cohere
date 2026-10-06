package flow

import (
	"slices"
	"strconv"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/system-inc/cohere/internal/lint/rule"
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
//
// A judge rules only where the target has an object part: a mutable slot exists only inside an array,
// tuple, container or object, and a class instance and an optional property are object types. A union with
// such a member has one, an intersection with an array-like or container member does, and so does a type
// parameter whose constraint has one (#53w68gt).
// So a site whose target has none (`const x: string = e`, a `number` argument) is never offered, which spares the
// checker its source on the most common sites (#m6tyg79). A judge that would rule on such a target needs
// that condition lifted in offer first.
type Judge func(pair Pair) (wrong bool, descend bool)

// maximumDepth bounds the walk. A recursive type meets its own pair first and stops on the visited set;
// this is for a type that keeps producing new ones, a generic instantiating itself deeper each level.
const maximumDepth = 8

// Walk relates a site's source to its target part by part, the way tsc related them when it accepted the
// site, and returns the first pair judge finds wrong. The pair it returns owns its Path; the Path a judge
// sees is the walker's, valid while the judge looks at that pair.
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
//	intersections           a target intersection pairs its array, tuple and container members with
//	                        the source; a source one meeting such a target pairs its members of that
//	                        kind assignable to the target alone. Under ObjectIntersections, a target's
//	                        object members too, and an intersection source's properties, unless it
//	                        has a primitive member
//	type parameters         a target parameter whose constraint has a writable slot is one mutable
//	                        slot, judged whole: its parts are no type the shim can build
//
// Index signatures outside arrays are not paired yet: whether one is readonly is an IndexInfo field the
// shim does not reach, and `Record<string, T>` is a map Adamic refuses in favor of `Map`. A lib generic
// whose writes are methods and is not in the list above relates the way tsc does.
//
// A fresh site is judged at its top only. Its parts are sites of their own, so descending would report
// one hole twice.
func (w *Walker) Walk(site Site, judge Judge) (Pair, bool) {
	return w.WalkWith(site, judge, WalkOptions{})
}

// WalkOptions widens one walk past what every rule's walk relates.
type WalkOptions struct {
	// ObjectIntersections pairs an intersection's object members and reads an intersection source's
	// properties, as the walk does an object's (#b9a0wgy). invariant-mutable asks for it: `{ pet: Animal } &
	// Named` written from `{ pet: Dog } & Named` is its hole, proven in Node. The other rules don't: an
	// intersection's members read one at a time are no slot of theirs, and every member paired was about
	// 2,900 false no-optional-widening findings per frontend (#53w68gt). An intersection with a primitive
	// member (a brand) is never paired this way: its object member is a phantom.
	ObjectIntersections bool
}

// WalkWith is Walk with options.
func (w *Walker) WalkWith(site Site, judge Judge, options WalkOptions) (Pair, bool) {
	top := Pair{Source: site.Source, Target: site.Target}
	if site.Fresh {
		wrong, _ := judge(top)
		return top, wrong
	}
	w.judge, w.newContainer, w.objectIntersections = judge, site.NewContainer, options.ObjectIntersections
	w.visited, w.path = w.visited[:0], w.path[:0]
	if len(w.visitedSet) > 0 {
		clear(w.visitedSet)
	}
	found, wrong := w.relate(top, 0)
	w.judge = nil
	return found, wrong
}

// Walker relates the sites of one file, keeping what each walk would otherwise allocate for itself: the
// pairs a walk has met, the path to the pair it is on, and which pairs are assignable. One serves every rule
// that walks the file (WalkerFor), so the three rules also share what they ask the checker. Measured on a
// cold ahra run, the per-walk maps, paths and pair lists, the per-site judges and each rule finding every
// site again were most of the three rules' 3.85M objects (#m6tyg79).
//
// Not safe for concurrent use, and it does not need to be: a file's rules run one at a time on the goroutine
// that owns the file, and a judge never starts a walk of its own.
type Walker struct {
	typeChecker *checker.Checker

	// judge and newContainer are the walk in progress's. newContainer is a site whose source container was
	// just built: its own slots are read-only to the walk, since no other name can write into them. See
	// Site.NewContainer.
	judge               Judge
	newContainer        bool
	objectIntersections bool

	// visited is the pairs this walk has met, in a slice while it is short and in visitedSet as well past
	// visitedListLimit: most walks meet a handful, and a map made for each walk was a quarter of the rules'
	// objects.
	visited    [][2]*checker.Type
	visitedSet map[[2]*checker.Type]bool

	// path is the steps from the site to the pair the walk is on. A pair's Path is a view of it.
	path []Step

	// assignable is the checker's answer for each pair asked, since the three rules ask the same pairs and
	// the checker builds a relation key for every ask, answered before or not.
	assignable map[[2]*checker.Type]bool

	// sitesNode is the node whose sites are in sites: the first rule to reach a node finds them, and the
	// others listening there read them. See Listeners.
	sitesNode *ast.Node
	sites     []Site
}

// visitedListLimit is how many pairs a walk keeps in its slice before it looks them up in a map.
const visitedListLimit = 16

// walkerKey is the walker's place in a file's cache.
const walkerKey = "flow.Walker"

// WalkerFor is the walker of ctx's file, made by the first rule that asks and shared by the rest. A
// context with no file cache gets a walker of its own each time, which shares nothing and judges the same.
// The walker is made empty and given the checker after, so the ask allocates no closure.
func WalkerFor(ctx rule.Context) *Walker {
	walker := rule.Cached(ctx.FileCache, walkerKey, func() *Walker { return &Walker{} })
	if walker.typeChecker == nil {
		walker.typeChecker = ctx.TypeChecker
	}
	return walker
}

// NewWalker is a walker over typeChecker that shares nothing yet.
func NewWalker(typeChecker *checker.Checker) *Walker {
	return &Walker{typeChecker: typeChecker}
}

// IsAssignable is the checker's isTypeAssignableTo, asked once per pair in the file.
func (w *Walker) IsAssignable(source *checker.Type, target *checker.Type) bool {
	key := [2]*checker.Type{source, target}
	if answer, asked := w.assignable[key]; asked {
		return answer
	}
	answer := checker.Checker_isTypeAssignableTo(w.typeChecker, source, target)
	if w.assignable == nil {
		w.assignable = map[[2]*checker.Type]bool{}
	}
	w.assignable[key] = answer
	return answer
}

// meet records that the walk has met key, and says whether it had met it before.
func (w *Walker) meet(key [2]*checker.Type) bool {
	if len(w.visited) < visitedListLimit {
		for _, met := range w.visited {
			if met == key {
				return true
			}
		}
		w.visited = append(w.visited, key)
		return false
	}
	if w.visitedSet == nil {
		w.visitedSet = map[[2]*checker.Type]bool{}
	}
	if len(w.visitedSet) == 0 {
		for _, met := range w.visited {
			w.visitedSet[met] = true
		}
	}
	if w.visitedSet[key] {
		return true
	}
	w.visitedSet[key] = true
	return false
}

func (w *Walker) relate(pair Pair, depth int) (Pair, bool) {
	if pair.Source == nil || pair.Target == nil || pair.Source == pair.Target || depth > maximumDepth {
		return Pair{}, false
	}
	if w.meet([2]*checker.Type{pair.Source, pair.Target}) {
		return Pair{}, false
	}

	wrong, descend := w.judge(pair)
	if wrong {
		pair.Path = slices.Clone(pair.Path)
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
			if !w.IsAssignable(pair.Source, member) {
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
	/*
	 * An intersection's array, tuple or container member is paired like a union's, its slot judged whole
	 * above (#53w68gt). A target intersection's value is every member at once, so a write through its array
	 * member reaches the source: that member is related to the whole source. A source intersection meeting
	 * an array-like or container target is related through its members of that kind assignable to the
	 * target alone, since their slots are the ones the target's names reach.
	 *
	 * Only those members. Pairing every member reached two shapes no ruling covers: a branded primitive
	 * (`string & { readonly [brand]?: ... }`), whose phantom member no-optional-widening then read as an
	 * optional property a string literal lacks, about 2,900 findings on each frontend; and an intersection
	 * of object types (React props, typescript-eslint's RuleModuleWithName), a few hundred more for
	 * invariant-mutable. Measured on the four consumers, and left for their own ruling.
	 */
	if pair.Target.Flags()&checker.TypeFlagsIntersection != 0 {
		objectMembers := w.objectIntersections && !hasPrimitiveMember(pair.Target)
		for _, member := range pair.Target.Types() {
			if !w.isSlotContainer(member) && (!objectMembers || member.Flags()&checker.TypeFlagsObject == 0) {
				continue
			}
			if found, isWrong := w.relate(Pair{Source: pair.Source, Target: member, Path: pair.Path}, depth+1); isWrong {
				return found, true
			}
		}
		return Pair{}, false
	}
	if pair.Source.Flags()&checker.TypeFlagsIntersection != 0 && w.isSlotContainer(pair.Target) {
		for _, member := range pair.Source.Types() {
			if !w.isSlotContainer(member) || !w.IsAssignable(member, pair.Target) {
				continue
			}
			if found, isWrong := w.relate(Pair{Source: member, Target: pair.Target, Path: pair.Path}, depth+1); isWrong {
				return found, true
			}
		}
		return Pair{}, false
	}
	if pair.Target.Flags()&checker.TypeFlagsTypeParameter != 0 {
		return w.typeParameterSlot(pair)
	}
	return w.parts(pair, depth)
}

// hasPrimitiveMember is an intersection with a member that is no object: `string & { readonly [brand]?: X }`,
// whose object member is a phantom the value never holds.
func hasPrimitiveMember(t *checker.Type) bool {
	for _, member := range t.Types() {
		if member.Flags()&(checker.TypeFlagsObject|checker.TypeFlagsTypeParameter|checker.TypeFlagsIntersection) == 0 {
			return true
		}
	}
	return false
}

// isSlotContainer is an array, a tuple or a library container: the slots the walk pairs by position or by
// type argument, as opposed to by property name.
func (w *Walker) isSlotContainer(t *checker.Type) bool {
	if IsArrayLike(w.typeChecker, t) {
		return true
	}
	_, _, isContainer := containerOf(t)
	return isContainer
}

/*
 * typeParameterSlot judges a type parameter target as one mutable slot when its constraint has a writable
 * slot anywhere in it (#53w68gt). The walk cannot go into the parameter's parts: `Pack[number]` is no type
 * the shim can build, and relating the constraints alone compares `Animal[]` with `Animal[]` and misses the
 * hole the parameter hides, `const pack: Pack = narrow` with `Narrow extends Pack`, where `pack.push` stores
 * a `Pack[number]` into a value that holds only `Narrow[number]`. Judged mutable, invariant-mutable asks
 * whether the parameter is assignable back to the source, which a narrower source is not.
 *
 * Only a source that flows into the parameter: one assignable to it, and not merely the parameter narrowed.
 * Measured on the four consumers, the walk otherwise met three shapes that are no flow at all:
 *   - `clone: <T extends this>() => T` seen under two receivers pairs one signature's `T` with the
 *     other's, two unrelated parameters (21 findings, api);
 *   - a source union pairs every member, so `readonly Entity[]` met the parameter `Entity` it is not
 *     assignable to (4);
 *   - `{} & T` into `T` is `T` with its nullishness ruled out, whose parts are `T`'s own (3, SharedState).
 * An `any` or `never` source is no value the slot could be shared with either.
 */
func (w *Walker) typeParameterSlot(pair Pair) (Pair, bool) {
	if pair.Source.Flags()&(checker.TypeFlagsAny|checker.TypeFlagsNever) != 0 || !w.IsAssignable(pair.Source, pair.Target) {
		return Pair{}, false
	}
	if pair.Source.Flags()&checker.TypeFlagsIntersection != 0 && slices.Contains(pair.Source.Types(), pair.Target) {
		return Pair{}, false
	}
	constraint := checker.Checker_getBaseConstraintOfType(w.typeChecker, pair.Target)
	if constraint == nil || constraint == pair.Target || !w.hasWritableSlot(constraint, 0) {
		return Pair{}, false
	}
	pair.Mutable = true
	if wrong, _ := w.judge(pair); wrong {
		pair.Path = slices.Clone(pair.Path)
		return pair, true
	}
	return Pair{}, false
}

/*
 * hasWritableSlot is a type with a slot a write reaches: an element of a mutable array or tuple, the
 * contents of a mutable container, or a property that is not readonly (methods aside, as in parts), at any
 * depth below a readonly one, since `readonly items: Animal[]` still holds a mutable array.
 */
func (w *Walker) hasWritableSlot(t *checker.Type, depth int) bool {
	if t == nil || depth > maximumDepth {
		return false
	}
	if t.Flags()&(checker.TypeFlagsUnion|checker.TypeFlagsIntersection) != 0 {
		for _, member := range t.Types() {
			if w.hasWritableSlot(member, depth+1) {
				return true
			}
		}
		return false
	}
	if checker.Checker_isArrayType(w.typeChecker, t) {
		return !isNamed(t, "ReadonlyArray") || w.hasWritableSlot(w.typeArgument(t, 0), depth+1)
	}
	if checker.IsTupleType(t) {
		if !t.TargetTupleType().IsReadonly() {
			return true
		}
		return slices.ContainsFunc(checker.Checker_getTypeArguments(w.typeChecker, t), func(element *checker.Type) bool {
			return w.hasWritableSlot(element, depth+1)
		})
	}
	if _, readonly, isContainer := containerOf(t); isContainer {
		return !readonly || slices.ContainsFunc(checker.Checker_getTypeArguments(w.typeChecker, t), func(argument *checker.Type) bool {
			return w.hasWritableSlot(argument, depth+1)
		})
	}
	if t.Flags()&checker.TypeFlagsObject == 0 {
		return false
	}
	for _, property := range checker.Checker_getPropertiesOfType(w.typeChecker, t) {
		if property.Flags&ast.SymbolFlagsMethod != 0 {
			continue
		}
		if !checker.Checker_isReadonlySymbol(w.typeChecker, property) ||
			w.hasWritableSlot(checker.Checker_getTypeOfSymbol(w.typeChecker, property), depth+1) {
			return true
		}
	}
	return false
}

// into relates part, one step below pair, with the step on the path while the part is judged.
func (w *Walker) into(pair Pair, part Pair, step Step, depth int) (Pair, bool) {
	w.path = append(w.path[:len(pair.Path)], step)
	part.Path = w.path
	found, wrong := w.relate(part, depth+1)
	w.path = w.path[:len(pair.Path)]
	return found, wrong
}

// parts relates the parts of two types that are not unions, one at a time in the order they are listed,
// and stops at the first that is wrong.
func (w *Walker) parts(pair Pair, depth int) (Pair, bool) {
	source, target := pair.Source, pair.Target
	// The slots of a container the site just built are no one else's, so writing through the wider type
	// reaches only this value. Its parts' own slots keep their mutability.
	ownSlotsShared := !(w.newContainer && len(pair.Path) == 0)

	if checker.Checker_isArrayType(w.typeChecker, target) {
		targetElement := w.typeArgument(target, 0)
		mutable := ownSlotsShared && !isNamed(target, "ReadonlyArray")
		switch {
		case checker.Checker_isArrayType(w.typeChecker, source):
			return w.into(pair, Pair{Source: w.typeArgument(source, 0), Target: targetElement, Mutable: mutable},
				Step{Kind: StepElement, Index: -1}, depth)
		case checker.IsTupleType(source):
			for _, element := range checker.Checker_getTypeArguments(w.typeChecker, source) {
				if found, wrong := w.into(pair, Pair{Source: element, Target: targetElement, Mutable: mutable},
					Step{Kind: StepElement, Index: -1}, depth); wrong {
					return found, true
				}
			}
		}
		return Pair{}, false
	}
	if checker.IsTupleType(target) {
		if !checker.IsTupleType(source) {
			return Pair{}, false
		}
		mutable := ownSlotsShared && !target.TargetTupleType().IsReadonly()
		sourceElements := checker.Checker_getTypeArguments(w.typeChecker, source)
		targetElements := checker.Checker_getTypeArguments(w.typeChecker, target)
		for index := 0; index < len(sourceElements) && index < len(targetElements); index++ {
			if found, wrong := w.into(pair, Pair{Source: sourceElements[index], Target: targetElements[index], Mutable: mutable},
				Step{Kind: StepElement, Index: index}, depth); wrong {
				return found, true
			}
		}
		return Pair{}, false
	}
	if container, readonly, isContainer := containerOf(target); isContainer {
		sourceContainer, _, sourceIsContainer := containerOf(source)
		if !sourceIsContainer || strings.TrimPrefix(sourceContainer, "Readonly") != strings.TrimPrefix(container, "Readonly") {
			return Pair{}, false
		}
		sourceArguments := checker.Checker_getTypeArguments(w.typeChecker, source)
		targetArguments := checker.Checker_getTypeArguments(w.typeChecker, target)
		for index := 0; index < len(sourceArguments) && index < len(targetArguments); index++ {
			if found, wrong := w.into(pair, Pair{Source: sourceArguments[index], Target: targetArguments[index], Mutable: ownSlotsShared && !readonly},
				Step{Kind: StepTypeArgument, Name: container, Index: index}, depth); wrong {
				return found, true
			}
		}
		return Pair{}, false
	}
	// An intersection source is read property by property under ObjectIntersections, since a property
	// lookup on an intersection finds it in whichever member holds it.
	sourceIsObject := source.Flags()&checker.TypeFlagsObject != 0 ||
		(w.objectIntersections && source.Flags()&checker.TypeFlagsIntersection != 0 && !hasPrimitiveMember(source))
	if target.Flags()&checker.TypeFlagsObject == 0 || !sourceIsObject {
		return Pair{}, false
	}
	targetSignatures := checker.Checker_getSignaturesOfType(w.typeChecker, target, checker.SignatureKindCall)
	sourceSignatures := checker.Checker_getSignaturesOfType(w.typeChecker, source, checker.SignatureKindCall)
	if len(targetSignatures) == 1 && len(sourceSignatures) == 1 {
		targetSignature, sourceSignature := targetSignatures[0], sourceSignatures[0]
		if found, wrong := w.into(pair, Pair{
			Source: checker.Checker_getReturnTypeOfSignature(w.typeChecker, sourceSignature),
			Target: checker.Checker_getReturnTypeOfSignature(w.typeChecker, targetSignature),
		}, Step{Kind: StepReturn, Index: -1}, depth); wrong {
			return found, true
		}
		sourceParameters := checker.Signature_parameters(sourceSignature)
		targetParameters := checker.Signature_parameters(targetSignature)
		// Positional parameters pair by position, and the pairing stops at a rest parameter on either side,
		// whose type is a tuple or array of everything after it rather than one parameter's type.
		for index := 0; index < len(sourceParameters) && index < len(targetParameters); index++ {
			if isRestParameter(sourceParameters[index]) || isRestParameter(targetParameters[index]) {
				break
			}
			if found, wrong := w.into(pair, Pair{
				Source: checker.Checker_getTypeOfSymbol(w.typeChecker, targetParameters[index]),
				Target: checker.Checker_getTypeOfSymbol(w.typeChecker, sourceParameters[index]),
			}, Step{Kind: StepParameter, Name: sourceParameters[index].Name, Index: index}, depth); wrong {
				return found, true
			}
		}
	}
	// A class's `prototype` is not writable: assigning it throws in strict code before anything is stored,
	// so a constructor's `prototype` is a read-only slot (Node probe on #b9a0wgy). Read-only, not skipped:
	// nominal-class still judges what it holds, and skipping it dropped 24 of its api findings. The value decides it, the source:
	// through an intersection target the walk meets `{ prototype: object }` alone, with no construct
	// signature of its own. Every `() => typeof Entity` resolver was about 84 findings this way.
	isConstructor := len(checker.Checker_getSignaturesOfType(w.typeChecker, source, checker.SignatureKindConstruct)) > 0 ||
		len(checker.Checker_getSignaturesOfType(w.typeChecker, target, checker.SignatureKindConstruct)) > 0
	for _, property := range checker.Checker_getPropertiesOfType(w.typeChecker, target) {
		if property.Flags&ast.SymbolFlagsMethod != 0 {
			continue
		}
		readonly := checker.Checker_isReadonlySymbol(w.typeChecker, property) || (isConstructor && property.Name == "prototype")
		sourceProperty := checker.Checker_getPropertyOfType(w.typeChecker, source, property.Name)
		if sourceProperty == nil {
			continue
		}
		if found, wrong := w.into(pair, Pair{
			Source:  checker.Checker_getTypeOfSymbol(w.typeChecker, sourceProperty),
			Target:  checker.Checker_getTypeOfSymbol(w.typeChecker, property),
			Mutable: ownSlotsShared && !readonly,
		}, Step{Kind: StepProperty, Name: property.Name, Index: -1}, depth); wrong {
			return found, true
		}
	}
	return Pair{}, false
}

func (w *Walker) typeArgument(t *checker.Type, index int) *checker.Type {
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
